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
	"github.com/openshift/dpu-operator/internal/scheme"
	"github.com/openshift/dpu-operator/internal/utils"
	"github.com/openshift/dpu-operator/pkgs/vars"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	kubescheme "k8s.io/client-go/kubernetes/scheme"
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

	req := clientset.CoreV1().RESTClient().Post().Resource("pods").Name(pod.Name).Namespace(pod.Namespace).SubResource("exec").VersionedParams(&podExecOptions, kubescheme.ParameterCodec)

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

func GetDPUHostNodes(c client.Client) ([]corev1.Node, error) {
	nodeList := &corev1.NodeList{}
	labelSelector := client.MatchingLabels{"dpu.config.openshift.io/dpuside": "dpu-host"}

	err := c.List(context.TODO(), nodeList, labelSelector)
	if err != nil {
		return nil, err
	}

	return nodeList.Items, nil
}

func GetDPUNodes(c client.Client) ([]corev1.Node, error) {
	nodeList := &corev1.NodeList{}
	labelSelector := client.MatchingLabels{"dpu.config.openshift.io/dpuside": "dpu"}

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

func EventuallyNoDpuOperatorConfig(c client.Client, timeout time.Duration, interval time.Duration) {
	formatCRDetails := func(cr configv1.DpuOperatorConfig) string {
		detail := fmt.Sprintf("%s/%s", cr.Namespace, cr.Name)
		if len(cr.Finalizers) > 0 {
			detail += fmt.Sprintf(" (finalizers: %v)", cr.Finalizers)
		}
		if cr.DeletionTimestamp != nil {
			detail += fmt.Sprintf(" (deletion started: %v)", cr.DeletionTimestamp)
		}
		return detail
	}

	onFailure := func() {
		crList := &configv1.DpuOperatorConfigList{}
		err := c.List(context.Background(), crList)
		if err != nil {
			fmt.Printf("Failed to list DpuOperatorConfigs for diagnostics: %v\n", err)
			return
		}
		if len(crList.Items) > 0 {
			fmt.Printf("Found %d DpuOperatorConfig CRs:\n", len(crList.Items))
			for _, cr := range crList.Items {
				fmt.Printf("  - %s\n", formatCRDetails(cr))
				fmt.Println(LogDpuOperatorConfigDiagnostics(c, cr.Name))
			}
		}
	}

	startTime := time.Now()

	AssertEventually(
		func() error {
			crList := &configv1.DpuOperatorConfigList{}
			err := c.List(context.Background(), crList)
			if err != nil {
				return fmt.Errorf("failed to list DpuOperatorConfigs: %v", err)
			}

			if len(crList.Items) > 0 {
				var details []string
				for _, cr := range crList.Items {
					details = append(details, formatCRDetails(cr))
				}
				return fmt.Errorf("found %d DpuOperatorConfig CRs still present: %v", len(crList.Items), details)
			}
			return nil
		},
		timeout,
		interval,
		timeout,
		"have no DpuOperatorConfig CRs present",
		onFailure,
		onFailure)

	cleanupTime := time.Now()
	fmt.Printf("All DpuOperatorConfigs cleaned up after %v\n", cleanupTime.Sub(startTime))
}

func LabelNodesForDpu(c client.Client, dpuSide string) error {
	nodeList := &corev1.NodeList{}
	err := c.List(context.TODO(), nodeList)
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	const dpuSideLabelKey = "dpu.config.openshift.io/dpuside"
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		if node.Labels == nil {
			node.Labels = make(map[string]string)
		}
		node.Labels[dpuSideLabelKey] = dpuSide

		err := c.Update(context.TODO(), node)
		if err != nil {
			return fmt.Errorf("failed to label node %s: %w", node.Name, err)
		}
	}
	return nil
}

func IsMasterNode(node corev1.Node) bool {
	if labels := node.Labels; labels != nil {
		if _, exists := labels["node-role.kubernetes.io/master"]; exists {
			return true
		}
		if _, exists := labels["node-role.kubernetes.io/control-plane"]; exists {
			return true
		}
	}
	return false
}

func LabelAllNodesWithDpu(c client.Client) error {
	nodes := &corev1.NodeList{}
	err := c.List(context.TODO(), nodes)
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	for i := range nodes.Items {
		node := &nodes.Items[i]
		if err := LabelSingleNodeWithDpu(c, node); err != nil {
			return err
		}
	}
	return nil
}

func LabelWorkerNodesWithDpu(c client.Client) error {
	nodes := &corev1.NodeList{}
	err := c.List(context.TODO(), nodes)
	if err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	var workerNodes []corev1.Node
	for _, node := range nodes.Items {
		if !IsMasterNode(node) {
			workerNodes = append(workerNodes, node)
		}
	}

	if len(workerNodes) == 0 {
		return nil
	}

	for _, node := range workerNodes {
		if err := LabelSingleNodeWithDpu(c, &node); err != nil {
			return err
		}
	}
	return nil
}

func LabelSingleNodeWithDpu(c client.Client, node *corev1.Node) error {
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	node.Labels["dpu"] = "true"

	err := c.Update(context.TODO(), node)
	if err != nil {
		return fmt.Errorf("failed to label node %s: %w", node.Name, err)
	}
	return nil
}

func LabelNodesWithDpu(c client.Client) error {
	clusterEnv := utils.NewClusterEnvironment(c)
	flavour, err := clusterEnv.Flavour(context.TODO())
	if err != nil {
		return fmt.Errorf("failed to detect cluster flavor: %w", err)
	}

	if flavour == utils.UnknownFlavour {
		return fmt.Errorf("unknown cluster flavor - cannot determine node labeling strategy")
	}

	switch flavour {
	case utils.MicroShiftFlavour, utils.KindFlavour:
		return LabelAllNodesWithDpu(c)
	case utils.OpenShiftFlavour:
		return LabelWorkerNodesWithDpu(c)
	default:
		return fmt.Errorf("unsupported cluster flavor %s", flavour)
	}
}

func WaitForDPUReady(c client.Client) error {
	// Get initial DPU list and fail fast if empty
	// Since daemon readiness guarantees detection completed, we should have DPUs
	initialDpuList := &configv1.DataProcessingUnitList{}
	err := c.List(context.TODO(), initialDpuList, client.InNamespace(vars.Namespace))
	if err != nil {
		return fmt.Errorf("failed to list DPU CRs: %w", err)
	}

	if len(initialDpuList.Items) == 0 {
		return fmt.Errorf("no DPU CRs found after daemon completed detection cycle")
	}

	expectedDpuNames := make(map[string]bool)
	for _, dpu := range initialDpuList.Items {
		expectedDpuNames[dpu.Name] = true
	}
	expectedCount := len(initialDpuList.Items)

	cdaLog.Info("Found DPU CRs to wait for", "count", expectedCount, "names", expectedDpuNames)

	// Wait for all detected DPUs to be Ready
	err = wait.PollUntilContextTimeout(context.TODO(), time.Second, TestInitialSetupTimeout*5, true, func(ctx context.Context) (bool, error) {
		dpuList := &configv1.DataProcessingUnitList{}
		err := c.List(ctx, dpuList, client.InNamespace(vars.Namespace))
		if err != nil {
			return false, nil
		}

		allReady := true
		for i, dpu := range dpuList.Items {
			var status string
			var reason string
			ready := false
			for _, condition := range dpu.Status.Conditions {
				if condition.Type == "Ready" {
					status = string(condition.Status)
					reason = condition.Reason
					if condition.Status == metav1.ConditionTrue {
						ready = true
					}
					break
				}
			}
			if status == "" {
				status = "Unknown"
			}

			cdaLog.Info("DPU CR details", "index", i, "name", dpu.Name, "status", status, "reason", reason, "ready", ready)

			if !ready {
				allReady = false
			}
		}

		return allReady, nil
	})

	if err != nil {
		return fmt.Errorf("timeout waiting for all DPU CRs to be Ready: %w", err)
	}

	// Verify no new DPUs appeared during waiting
	// Detection should be stable after daemon readiness
	finalDpuList := &configv1.DataProcessingUnitList{}
	err = c.List(context.TODO(), finalDpuList, client.InNamespace(vars.Namespace))
	if err != nil {
		return fmt.Errorf("failed to verify final DPU list: %w", err)
	}

	if len(finalDpuList.Items) != expectedCount {
		return fmt.Errorf("DPU count changed during waiting: expected %d, found %d (detection may still be ongoing)", expectedCount, len(finalDpuList.Items))
	}

	// Verify same DPUs are present
	for _, dpu := range finalDpuList.Items {
		if !expectedDpuNames[dpu.Name] {
			return fmt.Errorf("unexpected new DPU %s appeared during waiting (detection may still be ongoing)", dpu.Name)
		}
	}

	cdaLog.Info("All DPUs ready and detection stable", "count", expectedCount)
	return nil
}

func WaitForDPU(c client.Client) error {

	var dpuName string
	err := wait.PollUntilContextTimeout(context.TODO(), time.Second, TestInitialSetupTimeout*3, true, func(ctx context.Context) (bool, error) {
		dpuList := &configv1.DataProcessingUnitList{}
		err := c.List(ctx, dpuList, client.InNamespace(vars.Namespace))
		if err != nil {
			return false, nil
		}

		if len(dpuList.Items) > 0 {
			dpuName = dpuList.Items[0].Name
			return true, nil
		}

		return false, nil
	})

	if err != nil {
		return fmt.Errorf("timeout waiting for DPU resource: %w", err)
	}

	err = wait.PollUntilContextTimeout(context.TODO(), time.Second, TestInitialSetupTimeout*3, true, func(ctx context.Context) (bool, error) {
		dpu := &configv1.DataProcessingUnit{}
		key := client.ObjectKey{Name: dpuName, Namespace: vars.Namespace}

		err := c.Get(ctx, key, dpu)
		if err != nil {
			return false, nil
		}

		if meta.IsStatusConditionTrue(dpu.Status.Conditions, "Ready") {
			return true, nil
		}

		return false, nil
	})

	if err != nil {
		return fmt.Errorf("timeout waiting for DPU %s to be Ready: %w", dpuName, err)
	}

	return nil
}

func WaitForAllPodsReady(c client.Client, namespace string) error {

	err := wait.PollImmediate(time.Second, TestInitialSetupTimeout*3, func() (bool, error) {
		podList := &corev1.PodList{}
		err := c.List(context.TODO(), podList, client.InNamespace(namespace))
		if err != nil {
			return false, err
		}

		if len(podList.Items) == 0 {
			return false, nil
		}

		for _, pod := range podList.Items {
			ready := false
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					ready = true
					break
				}
			}
			if !ready {
				return false, nil
			}
		}

		return true, nil
	})

	if err != nil {
		return fmt.Errorf("pods not ready after timeout: %w", err)
	}

	return nil
}

func CreateClientsFromConfig(restConfig *rest.Config) (client.Client, kubernetes.Interface, error) {
	crClient, err := client.New(restConfig, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create controller-runtime client: %w", err)
	}

	k8sClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return crClient, k8sClient, nil
}

func SetupDpuOperator(c client.Client, configFile string) error {
	if err := LabelNodesWithDpu(c); err != nil {
		return fmt.Errorf("failed to label nodes: %w", err)
	}

	if err := SetupDpuOperatorConfig(c, configFile); err != nil {
		return fmt.Errorf("failed to create config: %w", err)
	}

	if err := TryEventually(func() error {
		return IsDpuOperatorConfigReady(c, "dpu-operator-config")
	}, TestInitialSetupTimeout, time.Second); err != nil {
		return fmt.Errorf("failed waiting for DpuOperatorConfig: %w", err)
	}

	if err := WaitForDPU(c); err != nil {
		return fmt.Errorf("failed waiting for DPU: %w", err)
	}

	if err := WaitForAllPodsReady(c, vars.Namespace); err != nil {
		return fmt.Errorf("failed waiting for pods: %w", err)
	}

	return nil
}

// Non-test versions of helper functions that return errors instead of using assertions

func CreateNamespaceWithRetry(client client.Client, ns *corev1.Namespace) error {
	// Try to create the namespace (ignore error if it already exists)
	err := client.Create(context.Background(), ns)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	// Wait for the namespace to be available
	return TryEventually(func() error {
		found := &corev1.Namespace{}
		return client.Get(context.Background(), types.NamespacedName{Name: ns.GetName()}, found)
	}, TestAPITimeout*3, TestRetryInterval)
}

func CreateDpuOperatorCRWithRetry(client client.Client, cr *configv1.DpuOperatorConfig) error {
	err := client.Create(context.Background(), cr)
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create DpuOperatorConfig: %w", err)
	}

	// Wait for it to exist
	return TryEventually(func() error {
		found := &configv1.DpuOperatorConfig{}
		return client.Get(context.Background(), types.NamespacedName{Name: cr.GetName()}, found)
	}, TestAPITimeout*3, TestRetryInterval)
}

func SetupDpuOperatorConfig(c client.Client, configFile string) error {
	ns := DpuOperatorNamespace()

	// Create namespace without test assertions
	if err := CreateNamespaceWithRetry(c, ns); err != nil {
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	var config *configv1.DpuOperatorConfig

	if configFile != "" {
		configData, err := os.ReadFile(configFile)
		if err != nil {
			return fmt.Errorf("failed to read config file %s: %w", configFile, err)
		}

		config = &configv1.DpuOperatorConfig{}
		if yamlErr := yaml.UnmarshalStrict(configData, config); yamlErr != nil {
			return fmt.Errorf("failed to parse config file %s: %w", configFile, yamlErr)
		}

		config.SetNamespace(ns.Name)
		config.SetName("dpu-operator-config")
	} else {
		config = DpuOperatorCR("dpu-operator-config", ns)
	}

	// Create DpuOperatorConfig without test assertions
	return CreateDpuOperatorCRWithRetry(c, config)
}

func ConfigureDpuOperator(c client.Client, configFile string) error {
	// Label nodes with DPU
	if err := LabelNodesWithDpu(c); err != nil {
		return fmt.Errorf("failed to label nodes: %w", err)
	}

	// Setup DpuOperatorConfig using non-test version
	if err := SetupDpuOperatorConfig(c, configFile); err != nil {
		return fmt.Errorf("failed to create config: %w", err)
	}

	// Wait for DpuOperatorConfig to be ready
	if err := TryEventually(func() error {
		return IsDpuOperatorConfigReady(c, "dpu-operator-config")
	}, TestInitialSetupTimeout, time.Second); err != nil {
		return fmt.Errorf("failed waiting for DpuOperatorConfig: %w", err)
	}

	// Wait for all pods
	if err := WaitForAllPodsReady(c, vars.Namespace); err != nil {
		return fmt.Errorf("failed waiting for pods: %w", err)
	}

	return nil
}

func SetupDpuOperatorWithRetry(c client.Client, configFile string) error {
	if err := LabelNodesWithDpu(c); err != nil {
		return fmt.Errorf("failed to label nodes: %w", err)
	}

	if err := SetupDpuOperatorConfig(c, configFile); err != nil {
		return fmt.Errorf("failed to create config: %w", err)
	}

	if err := TryEventually(func() error {
		return IsDpuOperatorConfigReady(c, "dpu-operator-config")
	}, TestInitialSetupTimeout, time.Second); err != nil {
		return fmt.Errorf("failed waiting for DpuOperatorConfig: %w", err)
	}

	if err := WaitForAllPodsReady(c, vars.Namespace); err != nil {
		return fmt.Errorf("failed waiting for pods: %w", err)
	}

	if err := WaitForDPUReady(c); err != nil {
		return fmt.Errorf("failed waiting for all DPU CRs to be Ready: %w", err)
	}

	return nil
}
