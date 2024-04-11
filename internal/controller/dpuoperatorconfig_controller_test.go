package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	netattdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"

	"k8s.io/apimachinery/pkg/api/errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/kind/pkg/cluster"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/rest"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"k8s.io/client-go/tools/clientcmd"

	configv1 "github.com/openshift/dpu-operator/api/v1"
)

var (
	testNamespace             = "openshift-dpu-operator"
	testDpuOperatorConfigName = "default"
	testDpuOperatorConfigKind = "DpuOperatorConfig"
	testDpuDaemonName         = "dpu-daemon"
	testSriovDevicePlugin     = "sriov-device-plugin"
	testNetworkFunctionNAD    = "dpunfcni-conf"
	testClusterName           = "dpu-operator-test-cluster"
	testAPITimeout            = time.Second * 2
	testRetryInterval         = time.Millisecond * 10
	testInitialSetupTimeout   = time.Minute
	setupLog                  = ctrl.Log.WithName("setup")
)

func dpuOperatorNameSpace() *corev1.Namespace {
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

func dpuOperatorCR(name string, mode string, ns *corev1.Namespace) *configv1.DpuOperatorConfig {
	config := &configv1.DpuOperatorConfig{}
	config.SetNamespace(ns.Name)
	config.SetName(name)
	config.Spec = configv1.DpuOperatorConfigSpec{
		Mode:     mode,
		LogLevel: 2,
	}
	return config
}

func createNameSpace(client client.Client, ns *v1.Namespace) {
	// ignore error when creating the namespace since it can already exist
	client.Create(context.Background(), ns)
	found := v1.Namespace{}
	Eventually(func() error {
		return client.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: ns.GetName()}, &found)
	}, testAPITimeout, testRetryInterval).Should(Succeed())
}

func deleteNameSpace(client client.Client, ns *v1.Namespace) {
	client.Delete(context.Background(), ns)
	found := v1.Namespace{}
	Eventually(func() error {
		err := client.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: ns.GetName()}, &found)
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("Still found")
	}, testAPITimeout, testRetryInterval).Should(Succeed())
}

func deleteKindTestCluster() {
	provider := cluster.NewProvider()
	provider.Delete(testClusterName, "")
}

func prepareKindCluster() *rest.Config {
	provider := cluster.NewProvider()
	// ignore error on create which will be non-nil when the cluster already exists
	provider.Create(testClusterName)
	cfg, err := provider.KubeConfig(testClusterName, false)
	Expect(err).NotTo(HaveOccurred())
	config, err := clientcmd.NewClientConfigFromBytes([]byte(cfg))
	Expect(err).NotTo(HaveOccurred())
	restCfg, err := config.ClientConfig()
	Expect(err).NotTo(HaveOccurred())
	return restCfg
}

func createDpuOperatorCR(client client.Client, cr *configv1.DpuOperatorConfig) {
	err := client.Create(context.Background(), cr)
	Expect(err).NotTo(HaveOccurred())
	found := configv1.DpuOperatorConfig{}
	Eventually(func() error {
		return client.Get(context.Background(), types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.GetName()}, &found)
	}, testAPITimeout, testRetryInterval).Should(Succeed())
}

func deleteDpuOperatorCR(client client.Client, cr *configv1.DpuOperatorConfig) {
	client.Delete(context.Background(), cr)
	found := configv1.DpuOperatorConfig{}
	Eventually(func() error {
		err := client.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: cr.GetName()}, &found)
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("Still found")
	}, testAPITimeout, testRetryInterval).Should(Succeed())
}

func startDPUControllerManager(ctx context.Context, client *rest.Config, wg *sync.WaitGroup) ctrl.Manager {
	var err error

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(configv1.AddToScheme(scheme))
	utilruntime.Must(netattdefv1.AddToScheme(scheme))

	mgr, err := ctrl.NewManager(client, ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: ":18001",
		},
		WebhookServer:    webhook.NewServer(webhook.Options{Port: 9443}),
		LeaderElectionID: "1e46962d.openshift.io",
	})
	Expect(err).NotTo(HaveOccurred())

	b := NewDpuOperatorConfigReconciler(mgr.GetClient(), mgr.GetScheme(), "mock-image")
	err = b.SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	wg.Add(1)
	go func() {
		setupLog.Info("starting manager")
		err := mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
		wg.Done()
	}()
	<-mgr.Elected()

	return mgr
}

func bootstrapTestEnv(restConfig *rest.Config) {
	var err error
	trueVal := true
	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("config", "crd", "bases"), filepath.Join("test", "crd")},
		ErrorIfCRDPathMissing: true,
		UseExistingCluster:    &trueVal,
		Config:                restConfig,
	}
	By("starting the test env")
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())
}

func stopDPUControllerManager(cancel context.CancelFunc, wg *sync.WaitGroup) {
	By("shut down controller manager")
	cancel()
	wg.Wait()
}

func WaitForDaemonSetReady(daemonSet *appsv1.DaemonSet, k8sClient client.Client, namespace, name string) {
	Eventually(func() error {
		return k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, daemonSet)
	}, testAPITimeout, testRetryInterval).ShouldNot(HaveOccurred())
}

func waitAllNodesReady(client client.Client) {
	var nodes corev1.NodeList
	Eventually(func() error {
		return client.List(context.Background(), &nodes)
	}, testAPITimeout, testRetryInterval).Should(Succeed())

	Eventually(func() bool {
		var latestNodes corev1.NodeList
		if err := client.List(context.Background(), &latestNodes); err != nil {
			return false
		}
		readyNodes := 0
		for _, node := range latestNodes.Items {
			for _, cond := range node.Status.Conditions {
				if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
					readyNodes++
					break
				}
			}
		}
		return readyNodes == len(latestNodes.Items)
	}, testInitialSetupTimeout, testRetryInterval).Should(BeTrue())
}

var _ = Describe("Main Controller", Ordered, func() {
	var cancel context.CancelFunc
	var ctx context.Context
	var wg sync.WaitGroup
	var mgr ctrl.Manager

	BeforeAll(func() {
		opts := zap.Options{
			Development: true,
		}
		ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
		fmt.Println(os.Args[0])
		client := prepareKindCluster()
		bootstrapTestEnv(client)
		ctx, cancel = context.WithCancel(context.Background())
		wg = sync.WaitGroup{}
		mgr = startDPUControllerManager(ctx, client, &wg)
		waitAllNodesReady(mgr.GetClient())

		found := configv1.DpuOperatorConfig{}
		Eventually(func() error {
			err := mgr.GetClient().Get(context.Background(), types.NamespacedName{Namespace: "", Name: ""}, &found)
			if errors.IsNotFound(err) {
				return nil
			} else {
				return err
			}
		}, testAPITimeout, testRetryInterval).Should(Succeed())
	})
	AfterAll(func() {
		stopDPUControllerManager(cancel, &wg)
		if os.Getenv("FAST_TEST") == "false" {
			deleteKindTestCluster()
		}
	})
	Context("When Host controller manager has started without DpuOperatorConfig CR", func() {
		var cr *configv1.DpuOperatorConfig

		Context("When DpuOperatorConfig CR exists with host mode", func() {
			BeforeAll(func() {
				ns := dpuOperatorNameSpace()
				cr = dpuOperatorCR("operator-config", "host", ns)
				createNameSpace(mgr.GetClient(), ns)
				createDpuOperatorCR(mgr.GetClient(), cr)
			})
			It("should have DPU daemon daemonsets created by controller manager", func() {
				daemonSet := appsv1.DaemonSet{}
				WaitForDaemonSetReady(&daemonSet, mgr.GetClient(), testNamespace, testDpuDaemonName)
				Expect(daemonSet.Spec.Template.Spec.Containers[0].Args[1]).To(Equal(cr.Spec.Mode))
			})
			It("should have SR-IOV device plugin daemonsets created by controller manager", func() {
				daemonSet1 := appsv1.DaemonSet{}
				WaitForDaemonSetReady(&daemonSet1, mgr.GetClient(), testNamespace, testSriovDevicePlugin)
				deleteDpuOperatorCR(mgr.GetClient(), cr)
			})
			It("should not have the network function NAD created by controller manager", func() {
				nad := netattdefv1.NetworkAttachmentDefinition{}
				err := mgr.GetClient().Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testNetworkFunctionNAD}, &nad)
				Expect(errors.IsNotFound(err)).To(BeTrue())
			})
			AfterAll(func() {
				ns := dpuOperatorNameSpace()
				cr = dpuOperatorCR("operator-config", "host", ns)
				deleteDpuOperatorCR(mgr.GetClient(), cr)
			})
		})

		Context("When DpuOperatorConfig CR is created with dpu mode", func() {
			BeforeAll(func() {
				ns := dpuOperatorNameSpace()
				cr = dpuOperatorCR("operator-config", "dpu", ns)
				createNameSpace(mgr.GetClient(), ns)
				createDpuOperatorCR(mgr.GetClient(), cr)
			})
			It("should have DPU daemon daemonsets created by controller manager", func() {
				daemonSet := &appsv1.DaemonSet{}
				WaitForDaemonSetReady(daemonSet, mgr.GetClient(), testNamespace, testDpuDaemonName)
				Expect(daemonSet.Spec.Template.Spec.Containers[0].Args[1]).To(Equal(cr.Spec.Mode))
			})
			It("should not have SR-IOV device plugin daemonsets created by controller manager", func() {
				daemonSet := &appsv1.DaemonSet{}
				err := mgr.GetClient().Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testSriovDevicePlugin}, daemonSet)
				Expect(errors.IsNotFound(err)).To(BeTrue())
			})
			It("should have SR-IOV device plugin daemonsets created by controller manager", func() {
				nad := &netattdefv1.NetworkAttachmentDefinition{}
				Eventually(func() error {
					return mgr.GetClient().Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testNetworkFunctionNAD}, nad)
				}, testAPITimeout, testRetryInterval).ShouldNot(HaveOccurred())
			})
			AfterAll(func() {
				ns := dpuOperatorNameSpace()
				cr = dpuOperatorCR("operator-config", "host", ns)
				deleteDpuOperatorCR(mgr.GetClient(), cr)
			})
		})
	})
})
