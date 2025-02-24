package drain

import (
	"context"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/dpu-operator/internal/testutils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	restConfig    *rest.Config
	k8sClient     client.Client
	testCluster   testutils.KindCluster
	node          corev1.Node
	testDrainer   *Drainer
	podName       = "test-pod"
	containerName = "test-container"
	podNamespace  = "default"
	imageName     = "nginx:latest"
)

func ensureTestPodCreated(c client.Client) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: podNamespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  containerName,
					Image: imageName,
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 80,
						},
					},
				},
			},
			NodeName: node.Name,
		},
	}
	err := c.Create(context.Background(), pod)
	Expect(err).NotTo(HaveOccurred())

	Eventually(func() bool {
		return testPodIsRunning()
	}, testutils.TestAPITimeout*4, testutils.TestRetryInterval).Should(BeTrue(), "Pod did not become running in expected time")
}

func ensureTestPodDeleted(c client.Client) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: podNamespace,
		},
	}

	err := c.Delete(context.Background(), pod)
	Expect(err).NotTo(HaveOccurred())

	Eventually(testPodIsRunning, testutils.TestAPITimeout*4, testutils.TestRetryInterval).
		Should(BeFalse(), "Test pod still exists after deletion")
}

func testPodIsRunning() bool {
	pod := testutils.GetPod(k8sClient, podName, podNamespace)
	if pod != nil {
		return pod.Status.Phase != corev1.PodRunning
	}
	return false
}

func isNodeReady(n corev1.Node) bool {
	for _, condition := range n.Status.Conditions {
		if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func isNodeSchedulable(n corev1.Node) bool {
	if n.Spec.Unschedulable {
		return false
	}
	for _, taint := range n.Spec.Taints {
		if taint.Effect == corev1.TaintEffectNoSchedule {
			return false
		}
	}
	return true
}

func getNode(c client.Client) corev1.Node {
	nodes := &corev1.NodeList{}
	err := c.List(context.Background(), nodes)
	Expect(err).NotTo(HaveOccurred())
	Expect(nodes.Items).NotTo(BeEmpty(), "No nodes found in the cluster")
	return nodes.Items[0]
}

func TestKindCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Drain helper suite")
}

var _ = Describe("Drain Interface", Ordered, func() {
	BeforeAll(func() {
		var err error

		opts := zap.Options{
			Development: true,
		}
		ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

		// Create a KindCluster
		testCluster = testutils.KindCluster{Name: "dpu-operator-test-cluster"}
		restConfig = testCluster.EnsureExists()

		k8sClient, err = client.New(restConfig, client.Options{})
		Expect(err).NotTo(HaveOccurred())
		node = getNode(k8sClient)
	})
	AfterAll(func() {
		if os.Getenv("FAST_TEST") == "false" {
			testCluster.EnsureDeleted()
		} else {
			ensureTestPodDeleted(k8sClient)
		}
	})
	Context("When node drain has not been requested", func() {
		It("should have all nodes available", func() {
			By("checking that all nodes are ready and schedulable")
			ctrl.Log.Info("Checking node is ready", "nodeName", node.Name)

			Expect(isNodeReady(node)).To(BeTrue(), "Node is not ready: "+node.Name)
			Expect(isNodeSchedulable(node)).To(BeTrue(), "Node is not schedulable: "+node.Name)
		})
		It("should be able to run a workload", func() {
			By("scheduling a test workload")
			ctrl.Log.Info("Creating test pod on node", "nodeName", node.Name)
			ensureTestPodCreated(k8sClient)
		})
	})
	Context("When node drain and cordon has been requested", func() {
		var drainSuccess bool
		var err error

		BeforeAll(func() {
			Eventually(func() bool {
				return testPodIsRunning()
			}, testutils.TestAPITimeout, testutils.TestRetryInterval).Should(BeTrue(), "Pod did not become running in expected time")

			ctrl.Log.Info("Draining node", "nodeName", node.Name)
			testDrainer, err = NewDrainer(restConfig)
			Expect(err).NotTo(HaveOccurred())

			drainSuccess, err = testDrainer.DrainNode(context.TODO(), &node, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(drainSuccess).To(BeTrue())
		})

		It("should drain the node and mark it unschedulable", func() {
			By("checking that the node is cordoned")
			Expect(node.Spec.Unschedulable).To(BeTrue(), "Node is still schedulable after drain: "+node.Name)
		})

		It("should have any workload pods drained", func() {
			By("verifying the test pod has been removed")
			ctrl.Log.Info("Ensuring workload has been drained", "nodeName", node.Name)
			Eventually(testPodIsRunning, testutils.TestAPITimeout, testutils.TestRetryInterval).Should(BeFalse(), "Test pod still exists after drain")
		})
	})
	Context("When node drain has completed", func() {
		var drainSuccess bool
		var err error

		BeforeAll(func() {
			ctrl.Log.Info("Completing drain on node", "nodeName", node.Name)
			drainSuccess, err = testDrainer.CompleteDrainNode(context.TODO(), &node)
			Expect(drainSuccess).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())

			ctrl.Log.Info("Checking node is ready", "nodeName", node.Name)
		})

		It("should have all nodes available", func() {
			By("checking that all nodes are ready and schedulable")
			Expect(isNodeReady(node)).To(BeTrue(), "Node is not ready: "+node.Name)
			Expect(isNodeSchedulable(node)).To(BeTrue(), "Node is not schedulable: "+node.Name)
		})
		It("should be able to run a workload", func() {
			By("scheduling a test workload")
			ctrl.Log.Info("Creating test pod on node", "nodeName", node.Name)
			ensureTestPodCreated(k8sClient)
		})
	})
})
