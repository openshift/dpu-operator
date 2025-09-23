package testutils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"github.com/openshift/dpu-operator/pkgs/vars"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Cluster interface {
	EnsureExists() *rest.Config
	EnsureDeleted()
}

type NetworkStatus struct {
	Name      string   `json:"name"`
	Interface string   `json:"interface"`
	IPs       []string `json:"ips"`
	Mac       string   `json:"mac"`
	DNS       struct{} `json:"dns"`
}

func PodGetDpuResourceRequests(pod *corev1.Pod) int {
	total := resource.MustParse("0")

	for _, c := range pod.Spec.Containers {
		if qty, ok := c.Resources.Requests["openshift.io/dpu"]; ok {
			total.Add(qty)
		}
	}

	return int(total.Value())
}

func GetPod(c client.Client, name string, namespace string) *corev1.Pod {
	obj := client.ObjectKey{Namespace: namespace, Name: name}
	pod := &corev1.Pod{}
	err := c.Get(context.TODO(), obj, pod)
	if err != nil {
		return nil
	}
	return pod
}

func ExecInPod(clientset kubernetes.Interface, config *rest.Config, pod *corev1.Pod, command string) (string, error) {
	if pod == nil {
		return "", fmt.Errorf("pod cannot be nil")
	}

	podExecOptions := corev1.PodExecOptions{
		Command: []string{"sh", "-c", command},
		Stdout:  true,
		Stderr:  true,
		TTY:     false,
	}

	req := clientset.CoreV1().RESTClient().Post().Resource("pods").Name(pod.Name).Namespace(pod.Namespace).SubResource("exec").VersionedParams(&podExecOptions, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create SPDY executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: io.Writer(&stdout),
		Stderr: io.Writer(&stderr),
	})
	if err != nil {
		return stderr.String(), fmt.Errorf("command execution failed: %w", err)
	}

	return stdout.String(), nil
}

func GetDPUNodes(c client.Client) ([]corev1.Node, error) {
	nodeList := &corev1.NodeList{}
	labelSelector := client.MatchingLabels{"dpu": "true"}

	err := c.List(context.TODO(), nodeList, labelSelector)
	if err != nil {
		return nil, err
	}

	return nodeList.Items, nil
}

// TrafficFlowTestsImage returns the appropriate image reference based on USE_LOCAL_REGISTRY
func TrafficFlowTestsImage() string {
	localContainer := ContainerImage{
		Registry: os.Getenv("REGISTRY"),
		Name:     "ovn-kubernetes/kubernetes-traffic-flow-tests",
		Tag:      "latest",
	}

	remoteContainer := ContainerImage{
		Registry: "ghcr.io",
		Name:     "ovn-kubernetes/kubernetes-traffic-flow-tests",
		Tag:      "latest",
	}

	if val, found := os.LookupEnv("USE_LOCAL_REGISTRY"); !found || val == "true" {
		err := EnsurePullAndPush(context.TODO(), remoteContainer, localContainer)
		Expect(err).To(BeNil())
		return localContainer.FullRef()
	}
	return remoteContainer.FullRef()
}

func NewTestPod(podName string, nodeHostname string) *corev1.Pod {
	privileged := true

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: "default",
			Annotations: map[string]string{
				"k8s.v1.cni.cncf.io/networks": vars.DefaultHostNADName,
			},
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": nodeHostname,
			},
			Containers: []corev1.Container{
				{
					Name:            "appcntr1",
					Image:           TrafficFlowTestsImage(),
					ImagePullPolicy: corev1.PullAlways,
					SecurityContext: &corev1.SecurityContext{
						Privileged: &privileged,
					},
				},
			},
		},
	}
}

func NewTestSfc(sfcName string, nfName string) *configv1.ServiceFunctionChain {
	return &configv1.ServiceFunctionChain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sfcName,
			Namespace: vars.Namespace,
		},
		Spec: configv1.ServiceFunctionChainSpec{
			NodeSelector: map[string]string{
				"dpu.config.openshift.io/dpuside": "dpu",
			},
			NetworkFunctions: []configv1.NetworkFunction{
				{
					Name:  nfName,
					Image: TrafficFlowTestsImage(),
				},
			},
		},
	}
}

func PodIsRunning(c client.Client, podName string, podNamespace string) bool {
	pod := GetPod(c, podName, podNamespace)
	if pod != nil {
		return pod.Status.Phase == corev1.PodRunning
	}
	return false
}

func EventuallyPodIsRunning(c client.Client, podName string, podNamespace string, timeout time.Duration, interval time.Duration) *corev1.Pod {
	var pod *corev1.Pod

	onFailure := func() {
		fmt.Println(LogPodDiagnostics(c, podName, podNamespace))
	}

	startTime := time.Now()

	AssertEventually(
		func() error {
			pod = GetPod(c, podName, podNamespace)
			if pod == nil {
				return fmt.Errorf("Pod %s in %s does not exist", podName, podNamespace)
			}
			return nil
		},
		timeout,
		interval,
		5*timeout,
		fmt.Sprintf("have pod %v in %v", podName, podNamespace),
		onFailure,
		onFailure)

	createdTime := time.Now()

	fmt.Printf("Pod '%s' created after %v\n", podName, createdTime.Sub(startTime))

	// Wait for pod to be running
	AssertEventually(
		func() error {
			pod = GetPod(c, podName, podNamespace)
			if pod == nil {
				return fmt.Errorf("Pod %s in %s does not exist", podName, podNamespace)
			}
			if pod.Status.Phase != corev1.PodRunning {
				return fmt.Errorf("Pod %s in %s is not running but in state %s", podName, podNamespace, pod.Status.Phase)
			}
			return nil
		}, timeout,
		interval,
		5*timeout,
		fmt.Sprintf("have pod %v in %v running", podName, podNamespace),
		onFailure,
		onFailure,
	)

	runningTime := time.Now()
	fmt.Printf("Pod '%s' running after %v (startup took %v)\n", podName, runningTime.Sub(startTime), runningTime.Sub(createdTime))

	return pod
}

func EventuallyPodDoesNotExist(c client.Client, podName string, podNamespace string, timeout time.Duration, interval time.Duration) {
	onFailure := func() {
		fmt.Println(LogPodDiagnostics(c, podName, podNamespace))
	}

	startTime := time.Now()

	AssertEventually(
		func() error {
			pod := GetPod(c, podName, podNamespace)
			if pod != nil {
				return fmt.Errorf("Pod %s in %s still exists", podName, podNamespace)
			}
			return nil
		},
		timeout,
		interval,
		5*timeout,
		fmt.Sprintf("have pod %v in %v deleted", podName, podNamespace),
		onFailure,
		onFailure)

	deletedTime := time.Now()
	fmt.Printf("Pod '%s' deleted after %v\n", podName, deletedTime.Sub(startTime))
}

func DeleteAndEventuallyPodDoesNotExist(c client.Client, podName string, podNamespace string, timeout time.Duration, interval time.Duration) {
	// Delete the pod if it exists (ignore NotFound errors)
	err := c.Delete(context.TODO(), &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: podNamespace,
		},
	})
	if err != nil && !errors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred(), "Failed to delete pod %s", podName)
	}

	// Wait for pod to be fully gone
	EventuallyPodDoesNotExist(c, podName, podNamespace, timeout, interval)
}

func GetSecondaryNetworkIP(pod *corev1.Pod, netdevName string) (string, error) {
	annotation, exists := pod.Annotations["k8s.v1.cni.cncf.io/network-status"]
	if !exists {
		return "", fmt.Errorf("network-status annotation not found")
	}

	var networks []NetworkStatus
	err := json.Unmarshal([]byte(annotation), &networks)
	if err != nil {
		return "", err
	}

	// Find secondary network IP
	for _, net := range networks {
		if net.Interface == netdevName {
			if len(net.IPs) > 0 {
				return net.IPs[0], nil
			}
		}
	}

	return "", fmt.Errorf("secondary network IP not found")
}

func GetSubnet(ip string) string {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		panic(fmt.Sprintf("Invalid IP address: %s", ip))
	}

	// Assume a standard /24 subnet for simplicity
	return parsedIP.Mask(net.CIDRMask(24, 32)).String() + "/24"
}

func GenerateAvailableIP(subnet string, usedIPs map[string]bool) string {
	ip, _, err := net.ParseCIDR(subnet)
	if err != nil {
		panic(fmt.Sprintf("Invalid subnet: %s", subnet))
	}

	// Avoid IPs in low range to reduce likelihood of a conflict
	for i := 10; i < 250; i++ {
		newIP := fmt.Sprintf("%s.%d", strings.Join(strings.Split(ip.String(), ".")[:3], "."), i)
		if !usedIPs[newIP] {
			return newIP
		}
	}

	panic("No available IPs found in subnet")
}

// Assume a standard /24 subnet for simplicity
func GetGatewayFromSubnet(subnet string) string {
	ip, _, err := net.ParseCIDR(subnet)
	if err != nil {
		panic(fmt.Sprintf("Invalid subnet: %s", subnet))
	}

	gatewayIP := fmt.Sprintf("%s.1", strings.Join(strings.Split(ip.String(), ".")[:3], "."))
	return gatewayIP
}

func GetPodEvents(c client.Client, podName string, podNamespace string) string {
	pod := GetPod(c, podName, podNamespace)
	if pod == nil {
		return "Pod not found, cannot retrieve events"
	}

	eventList := &corev1.EventList{}
	err := c.List(context.TODO(), eventList,
		client.InNamespace(podNamespace),
		client.MatchingFields{"involvedObject.name": podName})

	eventMsg := fmt.Sprintf("Recent events for pod %s (UID: %s):\n", podName, pod.UID)

	if err != nil {
		eventMsg += fmt.Sprintf("  - Error fetching events: %v\n", err)
	} else if len(eventList.Items) == 0 {
		eventMsg += "  - No events found\n"
	} else {
		events := eventList.Items
		start := len(events) - 5
		if start < 0 {
			start = 0
		}

		for i := start; i < len(events); i++ {
			event := events[i]
			eventMsg += fmt.Sprintf("  - %s: %s (%s)\n",
				event.LastTimestamp.Format("15:04:05"),
				event.Message, event.Reason)
		}
	}

	if pod.Spec.NodeName != "" {
		eventMsg += fmt.Sprintf("  - Scheduled to node: %s\n", pod.Spec.NodeName)
	} else {
		eventMsg += "  - Pod not yet scheduled to any node\n"
	}

	return eventMsg
}

func LogPodDiagnostics(c client.Client, podName string, podNamespace string) string {
	pod := GetPod(c, podName, podNamespace)
	if pod == nil {
		return fmt.Sprintf("Pod '%s' not found, cannot retrieve diagnostics", podName)
	}

	msg := fmt.Sprintf("Pod '%s' diagnostics (Phase: %s):\n", podName, pod.Status.Phase)

	if len(pod.Status.Conditions) > 0 {
		msg += "Pod conditions:\n"
		for _, condition := range pod.Status.Conditions {
			msg += fmt.Sprintf("  - %s: %s (reason: %s, message: %s)\n",
				condition.Type, condition.Status, condition.Reason, condition.Message)
		}
	}

	if len(pod.Status.ContainerStatuses) > 0 {
		msg += "Container statuses:\n"
		for _, containerStatus := range pod.Status.ContainerStatuses {
			msg += fmt.Sprintf("  - Container '%s': Ready=%t, RestartCount=%d\n",
				containerStatus.Name, containerStatus.Ready, containerStatus.RestartCount)

			if containerStatus.State.Waiting != nil {
				msg += fmt.Sprintf("    Waiting: %s - %s\n",
					containerStatus.State.Waiting.Reason, containerStatus.State.Waiting.Message)
			}
			if containerStatus.State.Terminated != nil {
				msg += fmt.Sprintf("    Terminated: %s - %s (exit code: %d)\n",
					containerStatus.State.Terminated.Reason, containerStatus.State.Terminated.Message,
					containerStatus.State.Terminated.ExitCode)
			}
		}
	}

	msg += GetPodEvents(c, podName, podNamespace)

	return msg
}

func AreIPsInSameSubnet(ip1, ip2, subnet string) bool {
	_, ipNet, err := net.ParseCIDR(subnet)
	if err != nil {
		panic(fmt.Sprintf("Invalid subnet: %s", subnet))
	}

	return ipNet.Contains(net.ParseIP(ip1)) && ipNet.Contains(net.ParseIP(ip2))
}

func GetFirstNode(c client.Client) (corev1.Node, error) {
	nodes := &corev1.NodeList{}
	err := c.List(context.Background(), nodes)
	if err != nil {
		return corev1.Node{}, fmt.Errorf("Failed to get nodes: %v", err)
	}
	if len(nodes.Items) == 0 {
		return corev1.Node{}, fmt.Errorf("No nodes found in cluster")
	}
	return nodes.Items[0], nil
}

func DpuOperatorNamespace() *corev1.Namespace {
	namespace := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: vars.Namespace,
		},
		Spec:   corev1.NamespaceSpec{},
		Status: corev1.NamespaceStatus{},
	}
	return namespace
}

func DpuOperatorCR(name string, ns *corev1.Namespace) *configv1.DpuOperatorConfig {
	config := &configv1.DpuOperatorConfig{}
	config.SetNamespace(ns.Name)
	config.SetName(name)
	config.Spec = configv1.DpuOperatorConfigSpec{
		LogLevel: 2,
	}
	return config
}

func CreateNamespace(client client.Client, ns *corev1.Namespace) {
	// ignore error when creating the namespace since it can already exist
	client.Create(context.Background(), ns)
	found := corev1.Namespace{}
	Eventually(func() error {
		return client.Get(context.Background(), types.NamespacedName{Namespace: vars.Namespace, Name: ns.GetName()}, &found)
	}, TestAPITimeout, TestRetryInterval).Should(Succeed())
}

func DeleteNamespace(client client.Client, ns *corev1.Namespace) {
	client.Delete(context.Background(), ns)
	found := corev1.Namespace{}
	Eventually(func() error {
		err := client.Get(context.Background(), types.NamespacedName{Namespace: vars.Namespace, Name: ns.GetName()}, &found)
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}, TestAPITimeout, TestRetryInterval).Should(Succeed())
}

func CreateDpuOperatorCR(client client.Client, cr *configv1.DpuOperatorConfig) {
	err := client.Create(context.Background(), cr)
	Expect(err).NotTo(HaveOccurred())
	found := configv1.DpuOperatorConfig{}
	Eventually(func() error {
		return client.Get(context.Background(), types.NamespacedName{Name: cr.GetName()}, &found)
	}, TestAPITimeout, TestRetryInterval).Should(Succeed())
}

func DeleteDpuOperatorCR(client client.Client, cr *configv1.DpuOperatorConfig) {
	err := client.Delete(context.Background(), cr)
	if err != nil && !errors.IsNotFound(err) {
		// If resource already doesn't exist, that's fine
		Expect(err).NotTo(HaveOccurred())
	}

	// Wait for the resource to be fully deleted
	found := configv1.DpuOperatorConfig{}
	Eventually(func() error {
		err := client.Get(context.Background(), types.NamespacedName{Namespace: vars.Namespace, Name: cr.GetName()}, &found)
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}, TestAPITimeout, TestRetryInterval).Should(Succeed())
}

func IsDpuOperatorConfigReady(c client.Client, name string) error {
	config := GetDpuOperatorConfig(c, name)
	if config == nil {
		return fmt.Errorf("DpuOperatorConfig %s does not exist", name)
	}
	if !meta.IsStatusConditionTrue(config.Status.Conditions, plugin.ReadyConditionType) {
		return fmt.Errorf("DpuOperatorConfig %s not ready", name)
	}
	return nil
}

func SfcNew(namespace, sfcName, nfName, nfImage string) *configv1.ServiceFunctionChain {
	return &configv1.ServiceFunctionChain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sfcName,
			Namespace: namespace,
		},
		Spec: configv1.ServiceFunctionChainSpec{
			NodeSelector: map[string]string{
				"dpu.config.openshift.io/dpuside": "dpu",
			},
			NetworkFunctions: []configv1.NetworkFunction{
				{
					Name:  nfName,
					Image: nfImage,
				},
			},
		},
	}
}

func SfcGet(c client.Client, name string, namespace string) *configv1.ServiceFunctionChain {
	obj := client.ObjectKey{Namespace: namespace, Name: name}
	pod := &configv1.ServiceFunctionChain{}
	err := c.Get(context.TODO(), obj, pod)
	if err != nil {
		Expect(errors.IsNotFound(err)).To(BeTrue())
		return nil
	}
	return pod
}

func SfcWait(c client.Client, name, namespace string, timeout time.Duration) *configv1.ServiceFunctionChain {
	var sfc *configv1.ServiceFunctionChain

	Eventually(func() bool {
		sfc = SfcGet(c, name, namespace)
		return sfc != nil
	}, timeout, 100*time.Millisecond).Should(BeTrue())

	return sfc
}

func SfcCreate(c client.Client, sfc *configv1.ServiceFunctionChain) *configv1.ServiceFunctionChain {
	err := c.Create(context.TODO(), sfc)
	Expect(err).NotTo(HaveOccurred())

	sfc2 := SfcWait(c, sfc.ObjectMeta.Name, sfc.ObjectMeta.Namespace, 2*time.Second)
	Expect(sfc2).NotTo(BeNil())

	return sfc2
}

func SfcList(c client.Client, namespace string) *configv1.ServiceFunctionChainList {
	sfcLs := &configv1.ServiceFunctionChainList{}
	err := c.List(context.TODO(), sfcLs, client.InNamespace(namespace))
	Expect(err).NotTo(HaveOccurred())
	return sfcLs
}

func GetDpuOperatorConfig(c client.Client, name string) *configv1.DpuOperatorConfig {
	obj := client.ObjectKey{Name: name}
	dpuOperatorConfig := &configv1.DpuOperatorConfig{}
	err := c.Get(context.TODO(), obj, dpuOperatorConfig)
	if err != nil {
		return nil
	}
	return dpuOperatorConfig
}

func LogDpuOperatorConfigDiagnostics(c client.Client, name string) string {
	dpuConfig := GetDpuOperatorConfig(c, name)
	if dpuConfig == nil {
		return fmt.Sprintf("DpuOperatorConfig '%s' not found, cannot retrieve diagnostics", name)
	}

	msg := fmt.Sprintf("DpuOperatorConfig '%s' diagnostics:\n", name)

	if !dpuConfig.DeletionTimestamp.IsZero() {
		msg += fmt.Sprintf("  - Deletion timestamp: %s\n", dpuConfig.DeletionTimestamp.Format("15:04:05"))
	} else {
		msg += "  - No deletion timestamp set\n"
	}

	if len(dpuConfig.Finalizers) > 0 {
		msg += "  - Finalizers present:\n"
		for _, finalizer := range dpuConfig.Finalizers {
			msg += fmt.Sprintf("    - %s\n", finalizer)
		}
	} else {
		msg += "  - No finalizers present\n"
	}

	msg += fmt.Sprintf("  - Generation: %d\n", dpuConfig.Generation)
	msg += fmt.Sprintf("  - ResourceVersion: %s\n", dpuConfig.ResourceVersion)

	if len(dpuConfig.Status.Conditions) > 0 {
		msg += "Status conditions:\n"
		for _, condition := range dpuConfig.Status.Conditions {
			msg += fmt.Sprintf("  - %s: %s (reason: %s, message: %s)\n",
				condition.Type, condition.Status, condition.Reason, condition.Message)
		}
	} else {
		msg += "No status conditions found\n"
	}

	return msg
}

func SetDpuOperatorConfigReady(c client.Client, name string) {
	dpuConfig := &configv1.DpuOperatorConfig{}
	err := c.Get(context.TODO(), client.ObjectKey{Name: name}, dpuConfig)
	Expect(err).NotTo(HaveOccurred())

	// Set Ready condition
	meta.SetStatusCondition(&dpuConfig.Status.Conditions, metav1.Condition{
		Type:    plugin.ReadyConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  "TestReady",
		Message: "Manually set to Ready for test",
	})

	err = c.Status().Update(context.TODO(), dpuConfig)
	Expect(err).NotTo(HaveOccurred())
}

func EventuallyDpuOperatorConfigReady(c client.Client, logger logr.Logger, cr *configv1.DpuOperatorConfig, timeout time.Duration, interval time.Duration) *configv1.DpuOperatorConfig {
	var dpuOperatorConfig *configv1.DpuOperatorConfig

	onFailure := func() {
		logger.Info(LogDpuOperatorConfigDiagnostics(c, cr.GetName()))
	}

	startTime := time.Now()

	// Wait for DpuOperatorConfig to exist and be Ready
	AssertEventually(
		func() error {
			return IsDpuOperatorConfigReady(c, cr.GetName())
		},
		timeout,
		interval,
		5*timeout,
		"have DpuOperatorConfig ready in namespace",
		onFailure,
		onFailure,
	)

	// Get the final ready config to return
	dpuOperatorConfig = GetDpuOperatorConfig(c, cr.GetName())

	readyTime := time.Now()
	logger.Info("DpuOperatorConfig ready", "name", cr.GetName(), "totalDuration", readyTime.Sub(startTime))

	return dpuOperatorConfig
}

func EventuallyDpuOperatorConfigDeleted(c client.Client, name string, namespace string, timeout time.Duration, interval time.Duration) {
	onFailure := func() {
		fmt.Println(LogDpuOperatorConfigDiagnostics(c, name))
	}

	startTime := time.Now()

	AssertEventually(
		func() error {
			dpuConfig := &configv1.DpuOperatorConfig{}
			err := c.Get(context.Background(), types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, dpuConfig)
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("error checking DpuOperatorConfig %s in %s: %v", name, namespace, err)
			}
			return fmt.Errorf("DpuOperatorConfig %s in %s still exists", name, namespace)
		},
		timeout,
		interval,
		5*timeout,
		fmt.Sprintf("have DpuOperatorConfig %v in %v deleted", name, namespace),
		onFailure,
		onFailure)

	deletedTime := time.Now()
	fmt.Printf("DpuOperatorConfig '%s' deleted after %v\n", name, deletedTime.Sub(startTime))
}
