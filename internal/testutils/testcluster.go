package testutils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	configv1 "github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/pkgs/vars"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/gomega"
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
					Image:           "ghcr.io/ovn-kubernetes/kubernetes-traffic-flow-tests:latest",
					ImagePullPolicy: corev1.PullAlways,
					SecurityContext: &corev1.SecurityContext{
						Privileged: &privileged,
					},
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

func SfcNew(namespace, sfcName, nfName, nfImage string) *configv1.ServiceFunctionChain {
	return &configv1.ServiceFunctionChain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sfcName,
			Namespace: namespace,
		},
		Spec: configv1.ServiceFunctionChainSpec{
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
