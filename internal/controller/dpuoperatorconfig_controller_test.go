package controller

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/dpu-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	testNamespace             = "openshift-dpu-operator"
	testDpuOperatorConfigName = "default"
	testDpuOperatorConfigKind = "DpuOperatorConfig"
	testDpuDaemonName         = "dpu-operator-daemon"
	testAPITimeout            = time.Second * 20
	testRetryInterval         = time.Second * 1
)

func WaitForDaemonSetReady(daemonSet *appsv1.DaemonSet, k8sClient client.Client, namespace, name string) {
	Eventually(func() error {
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, daemonSet)
		if err != nil {
			return err
		}
		if daemonSet.Status.DesiredNumberScheduled != daemonSet.Status.NumberReady {
			return fmt.Errorf("Desired Number Scheduled(%v) != NumberReady(%v)", daemonSet.Status.DesiredNumberScheduled, daemonSet.Status.NumberReady)
		} else {
			return nil
		}
	}, testAPITimeout, testRetryInterval).ShouldNot(HaveOccurred())
}

func createDpuOperatorNameSpace() *corev1.Namespace {
	namespace := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
		},
		Spec:   corev1.NamespaceSpec{},
		Status: corev1.NamespaceStatus{},
	}
	return namespace
}

func ensureDpuOperatorNamespace() {
	By("create test DPU operator namespace")
	namespace := createDpuOperatorNameSpace()
	Expect(k8sClient.Create(context.Background(), namespace)).Should(Succeed())

	By("verify test DPU operator namespace is created")
	retrieved_namespace := &corev1.Namespace{}
	err := k8sClient.Get(context.Background(), client.ObjectKey{Name: testNamespace}, retrieved_namespace)
	Expect(err).NotTo(HaveOccurred())
	Expect(retrieved_namespace.ObjectMeta.Name).To(Equal(testNamespace))
}

func startDPUControllerManager(ctx context.Context, wg *sync.WaitGroup) {
	By("setting up env variables for tests")
	err := os.Setenv("DPU_DAEMON_IMAGE", "mock-image")
	Expect(err).NotTo(HaveOccurred())

	By("setup controller manager")
	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	By("setup controller manager reconciler")
	err = (&DpuOperatorConfigReconciler{
		Client: k8sManager.GetClient(),
		Scheme: k8sManager.GetScheme(),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer GinkgoRecover()
		By("start controller manager")
		err := k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred())
	}()
}

func createDpuOperatorCR(mode string) *configv1.DpuOperatorConfig {
	config := &configv1.DpuOperatorConfig{}
	config.SetNamespace(testNamespace)
	config.SetName(testDpuOperatorConfigName)
	config.Spec = configv1.DpuOperatorConfigSpec{
		Mode:     mode,
		LogLevel: 2,
	}
	return config
}

func ensureDpuOperatorCR(mode string) {
	By("create DpuOperatorConfig CR")
	config := createDpuOperatorCR(mode)
	Expect(k8sClient.Create(context.Background(), config)).Should(Succeed())

	By("verify DpuOperatorConfig CR is created")
	retrieved_config := &configv1.DpuOperatorConfig{}
	err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: testDpuOperatorConfigName}, retrieved_config)
	Expect(err).NotTo(HaveOccurred())
	Expect(retrieved_config.ObjectMeta.Namespace).To(Equal(testNamespace))
	Expect(retrieved_config.ObjectMeta.Name).To(Equal(testDpuOperatorConfigName))
}

var _ = Describe("Main Controller", Ordered, func() {
	var cancel context.CancelFunc
	var ctx context.Context
	var wg sync.WaitGroup

	BeforeAll(func() {
		// NOTE: We know that it is more complete to delete and re-create the test
		// namespace however there seems to be a problem deleting the namespace
		// after creation.
		ensureDpuOperatorNamespace()
	})

	Context("When controller manager has started without DpuOperatorConfig CRD", func() {
		BeforeEach(func() {
			ctx, cancel = context.WithCancel(context.Background())
			wg = sync.WaitGroup{}
			startDPUControllerManager(ctx, &wg)
			ensureDpuOperatorCR("host")
		})
		It("should have DPU daemon daemonsets created by controller manager", func() {
			daemonSet := &appsv1.DaemonSet{}
			WaitForDaemonSetReady(daemonSet, k8sClient, testNamespace, testDpuDaemonName)
			Expect(daemonSet.OwnerReferences).To(HaveLen(1))
			Expect(daemonSet.OwnerReferences[0].Kind).To(Equal(testDpuOperatorConfigKind))
			Expect(daemonSet.OwnerReferences[0].Name).To(Equal(testDpuOperatorConfigName))
		})
		AfterEach(func() {
			By("shut down controller manager")
			cancel()
			wg.Wait()
			config := createDpuOperatorCR("host")
			err := k8sClient.Delete(context.Background(), config)
			Expect(err).ToNot(HaveOccurred())
		})
	})

})
