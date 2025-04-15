package drain

import (
	"context"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/dpu-operator/internal/testutils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func testPod(node corev1.Node) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "nginx:latest",
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
}

func ensureTestPodCreated(c client.Client, node corev1.Node) {
	pod := testPod(node)
	existingPod := &corev1.Pod{}
	err := c.Get(context.Background(), client.ObjectKey{
		Name:      pod.Name,
		Namespace: pod.Namespace,
	}, existingPod)

	if err != nil {
		if !errors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred())
		} else {
			err := c.Create(context.Background(), pod)
			Expect(err).NotTo(HaveOccurred())
		}
	}

	Eventually(func() bool {
		return testPodIsRunning(c, node)
	}, testutils.TestAPITimeout*8, testutils.TestRetryInterval).Should(BeTrue(), "Pod did not become running in expected time")
}

func ensureTestPodDeleted(c client.Client, node corev1.Node) {
	if exists, err := testPodExists(c, node); exists {
		Expect(err).NotTo(HaveOccurred())
		Expect(c.Delete(context.Background(), testPod(node))).NotTo(HaveOccurred())
	}

	Eventually(func() bool {
		exists, err := testPodExists(c, node)
		Expect(err).NotTo(HaveOccurred())
		return !exists
	}, testutils.TestAPITimeout*8, testutils.TestRetryInterval).Should(BeTrue(), "Pod still exists after deletion")
}

func testPodExists(c client.Client, node corev1.Node) (bool, error) {
	pod := testPod(node)
	existingPod := &corev1.Pod{}
	err := c.Get(context.Background(), client.ObjectKey{
		Name:      pod.Name,
		Namespace: pod.Namespace,
	}, existingPod)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func testPodIsRunning(c client.Client, node corev1.Node) bool {
	pod := testPod(node)

	existingPod := testutils.GetPod(c, pod.Name, pod.Namespace)
	if existingPod != nil {
		return existingPod.Status.Phase == corev1.PodRunning
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

func TestKindCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Drain helper suite")
}

var _ = Describe("Drain Interface", Ordered, func() {
	var (
		restConfig  *rest.Config
		k8sClient   client.Client
		testCluster testutils.KindCluster
		node        corev1.Node
		testDrainer *Drainer
	)

	BeforeAll(func() {
		var err error

		opts := zap.Options{
			Development: true,
		}
		ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

		testCluster = testutils.KindCluster{Name: "dpu-operator-test-cluster"}
		restConfig = testCluster.EnsureExists()
		k8sClient, err = client.New(restConfig, client.Options{})
		Expect(err).NotTo(HaveOccurred())
		testutils.WaitAllNodesReady(k8sClient)
		node, err = testutils.GetFirstNode(k8sClient)
		testDrainer, err = NewDrainer(restConfig)
		Expect(err).NotTo(HaveOccurred())

		// outer block prepares a clean cluster without a pod
		testDrainer.CompleteDrainNode(context.TODO(), &node)
		Expect(isNodeSchedulable(node)).To(BeTrue(), "Node is not schedulable: "+node.Name)
		ensureTestPodDeleted(k8sClient, node)
	})

	AfterAll(func() {
		// Make sure we leave the cluster in a clean state: no draining and no test pod
		ensureTestPodDeleted(k8sClient, node)
		testDrainer.CompleteDrainNode(context.TODO(), &node)
		if os.Getenv("FAST_TEST") == "false" {
			testCluster.EnsureDeleted()
		}
	})

	Context("When the drainer has not been requested", func() {
		It("should be able to run a workload on the target node", func() {
			By("Creating a test pod")
			ensureTestPodCreated(k8sClient, node)
			By("Cleaning up the test pod")
			ensureTestPodDeleted(k8sClient, node)
		})
	})

	Context("When there is a pod running and drain is requested", func() {
		BeforeAll(func() {
			ensureTestPodCreated(k8sClient, node)

			drainSuccess, err := testDrainer.DrainNode(context.TODO(), &node, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(drainSuccess).To(BeTrue())
		})

		AfterAll(func() {
			ensureTestPodDeleted(k8sClient, node)
			testDrainer.CompleteDrainNode(context.TODO(), &node)
			Eventually(func() bool { return isNodeSchedulable(node) }, testutils.TestAPITimeout*4, testutils.TestRetryInterval).Should(BeTrue(), "Node is not schedulable: "+node.Name)
		})

		It("should mark the node as unschedulable", func() {
			Eventually(func() bool { return isNodeSchedulable(node) }).Should(BeFalse(), "Node is still schedulable after drain: "+node.Name)
		})

		It("Should evict the running pod", func() {
			Eventually(func() bool { return testPodIsRunning(k8sClient, node) }, testutils.TestAPITimeout, testutils.TestRetryInterval).Should(BeFalse(), "Test pod still exists after drain")
		})
	})

	Context("When a node drain has completed", func() {
		BeforeAll(func() {
			drainSuccess, err := testDrainer.DrainNode(context.TODO(), &node, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(drainSuccess).To(BeTrue())
			testDrainer.CompleteDrainNode(context.TODO(), &node)
		})

		AfterAll(func() {
			ensureTestPodDeleted(k8sClient, node)
		})

		It("should mark the node as schedulable again", func() {
			Eventually(func() bool { return isNodeSchedulable(node) }, testutils.TestAPITimeout, testutils.TestRetryInterval).Should(BeTrue(), "Node should be schedulable but it isn't")
		})

		It("should allow a new pod to run on the node", func() {
			ensureTestPodCreated(k8sClient, node)
			Eventually(func() bool { return testPodIsRunning(k8sClient, node) }, testutils.TestAPITimeout, testutils.TestRetryInterval).Should(BeTrue(), "Test pod not running after drain complete")
			ensureTestPodDeleted(k8sClient, node)
		})
	})
})
