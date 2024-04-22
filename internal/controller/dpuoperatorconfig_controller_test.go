package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	netattdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/dpu-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var (
	testNamespace             = "openshift-dpu-operator"
	testDpuOperatorConfigName = "default"
	testDpuOperatorConfigKind = "DpuOperatorConfig"
	testDpuDaemonName         = "dpu-daemon"
	testSriovDevicePlugin     = "sriov-device-plugin"
	testNetworkFunctionNAD    = "dpunfcni-conf"
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

func stopDPUControllerManager(mode string, cancel context.CancelFunc, wg *sync.WaitGroup) {
	By("shut down controller manager")
	cancel()
	wg.Wait()
	config := createDpuOperatorCR(mode)
	err := k8sClient.Delete(context.Background(), config)
	Expect(err).ToNot(HaveOccurred())
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

	BeforeEach(func() {
		// IMPORTANT Note: The Envtest has many limitations described here:
		// https://book.kubebuilder.io/reference/envtest.html#testing-considerations
		// Thus we need to create and destroy the Envtest environment. Please note
		// that Envtest does not garbage collect thus owner references do not get
		// cleaned up!!
		By("bootstrapping test environment")
		testEnv = &envtest.Environment{
			CRDDirectoryPaths:     []string{filepath.Join("config", "crd", "bases"), filepath.Join("test", "crd")},
			ErrorIfCRDPathMissing: true,
		}
		var err error
		By("starting the test env")
		cfg, err = testEnv.Start()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).NotTo(BeNil())

		By("registering schemes")
		err = configv1.AddToScheme(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred())
		err = netattdefv1.AddToScheme(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred())

		By("creating k8s client")
		k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient).NotTo(BeNil())

		// NOTE: Please refer to this limitation of namespace deletion:
		// https://book.kubebuilder.io/reference/envtest.html#namespace-usage-limitation
		// Namespaces cannot be cleaned up!
		ensureDpuOperatorNamespace()
	})

	Context("When Host controller manager has started without DpuOperatorConfig CR", func() {
		mode := "host"
		BeforeEach(func() {
			ctx, cancel = context.WithCancel(context.Background())
			wg = sync.WaitGroup{}
			startDPUControllerManager(ctx, &wg)
			ensureDpuOperatorCR(mode)
		})
		It("should have DPU daemon daemonsets created by controller manager", func() {
			daemonSet := &appsv1.DaemonSet{}
			WaitForDaemonSetReady(daemonSet, k8sClient, testNamespace, testDpuDaemonName)
			Expect(daemonSet.OwnerReferences).To(HaveLen(1))
			Expect(daemonSet.OwnerReferences[0].Kind).To(Equal(testDpuOperatorConfigKind))
			Expect(daemonSet.OwnerReferences[0].Name).To(Equal(testDpuOperatorConfigName))
			Expect(daemonSet.Spec.Template.Spec.Containers[0].Args[1]).To(Equal(mode))
		})
		It("should have SR-IOV device plugin daemonsets created by controller manager", func() {
			daemonSet := &appsv1.DaemonSet{}
			WaitForDaemonSetReady(daemonSet, k8sClient, testNamespace, testSriovDevicePlugin)
			Expect(daemonSet.OwnerReferences).To(HaveLen(1))
			Expect(daemonSet.OwnerReferences[0].Kind).To(Equal(testDpuOperatorConfigKind))
			Expect(daemonSet.OwnerReferences[0].Name).To(Equal(testDpuOperatorConfigName))
		})
		It("should not have the network function NAD created by controller manager", func() {
			nad := &netattdefv1.NetworkAttachmentDefinition{}
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testNetworkFunctionNAD}, nad)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
		AfterEach(func() {
			stopDPUControllerManager(mode, cancel, &wg)
		})
	})

	Context("When DPU controller manager has started without DpuOperatorConfig CR", func() {
		mode := "dpu"
		BeforeEach(func() {
			ctx, cancel = context.WithCancel(context.Background())
			wg = sync.WaitGroup{}
			startDPUControllerManager(ctx, &wg)
			ensureDpuOperatorCR(mode)
		})
		It("should have DPU daemon daemonsets created by controller manager", func() {
			daemonSet := &appsv1.DaemonSet{}
			WaitForDaemonSetReady(daemonSet, k8sClient, testNamespace, testDpuDaemonName)
			Expect(daemonSet.OwnerReferences).To(HaveLen(1))
			Expect(daemonSet.OwnerReferences[0].Kind).To(Equal(testDpuOperatorConfigKind))
			Expect(daemonSet.OwnerReferences[0].Name).To(Equal(testDpuOperatorConfigName))
			Expect(daemonSet.Spec.Template.Spec.Containers[0].Args[1]).To(Equal(mode))
		})
		It("should not have SR-IOV device plugin daemonsets created by controller manager", func() {
			daemonSet := &appsv1.DaemonSet{}
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testSriovDevicePlugin}, daemonSet)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
		It("should have SR-IOV device plugin daemonsets created by controller manager", func() {
			nad := &netattdefv1.NetworkAttachmentDefinition{}
			Eventually(func() error {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testNetworkFunctionNAD}, nad)
				if err != nil {
					return err
				}
				return nil
			}, testAPITimeout, testRetryInterval).ShouldNot(HaveOccurred())
			Expect(nad.OwnerReferences).To(HaveLen(1))
			Expect(nad.OwnerReferences[0].Kind).To(Equal(testDpuOperatorConfigKind))
			Expect(nad.OwnerReferences[0].Name).To(Equal(testDpuOperatorConfigName))
		})
		AfterEach(func() {
			stopDPUControllerManager(mode, cancel, &wg)
		})
	})

	AfterEach(func() {
		By("tearing down the test environment")
		err := testEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	})
})
