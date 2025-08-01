package controller

import (
	"context"
	"os"
	"sync"

	netattdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"

	"k8s.io/apimachinery/pkg/api/errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/dpu-operator/internal/images"
	"github.com/openshift/dpu-operator/internal/scheme"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"

	appsv1 "k8s.io/api/apps/v1"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/openshift/dpu-operator/internal/testutils"
	"github.com/openshift/dpu-operator/pkgs/vars"

	configv1 "github.com/openshift/dpu-operator/api/v1"
)

var (
	testNamespace              = vars.Namespace
	testDpuOperatorConfigName  = vars.DpuOperatorConfigName
	testDpuOperatorConfigKind  = "DpuOperatorConfig"
	testDpuDaemonName          = "dpu-daemon"
	testNetworkFunctionNADDpu  = "dpunfcni-conf"
	testNetworkFunctionNADHost = vars.DefaultHostNADName
	testClusterName            = "dpu-operator-test-cluster"
	setupLog                   = ctrl.Log.WithName("setup")
)

func startDPUControllerManager(ctx context.Context, client *rest.Config, wg *sync.WaitGroup) ctrl.Manager {
	var err error

	mgr, err := ctrl.NewManager(client, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: server.Options{
			BindAddress: ":18001",
		},
		WebhookServer:    webhook.NewServer(webhook.Options{Port: 9443}),
		LeaderElectionID: "1e46962d.openshift.io",
	})
	Expect(err).NotTo(HaveOccurred())

	mockImageManager := images.NewDummyImageManager()
	b := NewDpuOperatorConfigReconciler(mgr.GetClient(), mgr.GetScheme(), mockImageManager)
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

func stopDPUControllerManager(cancel context.CancelFunc, wg *sync.WaitGroup) {
	By("shut down controller manager")
	cancel()
	wg.Wait()
}

var _ = Describe("Main Controller", Ordered, func() {
	var cancel context.CancelFunc
	var ctx context.Context
	var wg sync.WaitGroup
	var mgr ctrl.Manager
	var testCluster testutils.KindCluster

	BeforeAll(func() {
		opts := zap.Options{
			Development: true,
		}
		ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
		testCluster = testutils.KindCluster{Name: testClusterName}
		client := testCluster.EnsureExists()
		ctx, cancel = context.WithCancel(context.Background())
		wg = sync.WaitGroup{}
		mgr = startDPUControllerManager(ctx, client, &wg)
		testutils.WaitAllNodesReady(mgr.GetClient())

		found := configv1.DpuOperatorConfig{}
		Eventually(func() error {
			err := mgr.GetClient().Get(context.Background(), types.NamespacedName{Namespace: "", Name: ""}, &found)
			if errors.IsNotFound(err) {
				return nil
			} else {
				return err
			}
		}, testutils.TestAPITimeout, testutils.TestRetryInterval).Should(Succeed())
	})
	AfterAll(func() {
		stopDPUControllerManager(cancel, &wg)
		if os.Getenv("FAST_TEST") == "false" {
			testCluster.EnsureDeleted()
		}
	})
	Context("When Host controller manager has started without DpuOperatorConfig CR", func() {
		var cr *configv1.DpuOperatorConfig

		Context("When DpuOperatorConfig CR exists with host mode", func() {
			BeforeAll(func() {
				ns := testutils.DpuOperatorNamespace()
				cr = testutils.DpuOperatorCR(testDpuOperatorConfigName, "host", ns)
				testutils.CreateNamespace(mgr.GetClient(), ns)
				testutils.CreateDpuOperatorCR(mgr.GetClient(), cr)
			})
			It("should have DPU daemon daemonsets created by controller manager", func() {
				daemonSet := appsv1.DaemonSet{}
				testutils.WaitForDaemonSetReady(&daemonSet, mgr.GetClient(), testNamespace, testDpuDaemonName)
				Expect(daemonSet.Spec.Template.Spec.Containers[0].Args[1]).To(Equal("auto"))
			})
			It("should have the network function NAD created by controller manager", func() {
				nad := &netattdefv1.NetworkAttachmentDefinition{}
				Eventually(func() error {
					return mgr.GetClient().Get(context.Background(), types.NamespacedName{Namespace: "default", Name: testNetworkFunctionNADHost}, nad)
				}, testutils.TestAPITimeout*3, testutils.TestRetryInterval).ShouldNot(HaveOccurred())
			})
			AfterAll(func() {
				ns := testutils.DpuOperatorNamespace()
				cr = testutils.DpuOperatorCR(testDpuOperatorConfigName, "host", ns)
				testutils.DeleteDpuOperatorCR(mgr.GetClient(), cr)
			})
		})

		Context("When DpuOperatorConfig CR is created with dpu mode", func() {
			BeforeAll(func() {
				ns := testutils.DpuOperatorNamespace()
				cr = testutils.DpuOperatorCR("operator-config", "dpu", ns)
				testutils.CreateNamespace(mgr.GetClient(), ns)
				testutils.CreateDpuOperatorCR(mgr.GetClient(), cr)
			})
			It("should have DPU daemon daemonsets created by controller manager", func() {
				daemonSet := &appsv1.DaemonSet{}
				Eventually(func() string {
					testutils.WaitForDaemonSetReady(daemonSet, mgr.GetClient(), testNamespace, testDpuDaemonName)
					return daemonSet.Spec.Template.Spec.Containers[0].Args[1]
				}, testutils.TestAPITimeout*2, testutils.TestRetryInterval).Should(Equal("auto"))
			})
			It("should have the network function NAD created by controller manager", func() {
				nad := &netattdefv1.NetworkAttachmentDefinition{}
				Eventually(func() error {
					return mgr.GetClient().Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testNetworkFunctionNADDpu}, nad)
				}, testutils.TestAPITimeout*3, testutils.TestRetryInterval).ShouldNot(HaveOccurred())
			})
			AfterAll(func() {
				ns := testutils.DpuOperatorNamespace()
				cr = testutils.DpuOperatorCR("operator-config", "host", ns)
				testutils.DeleteDpuOperatorCR(mgr.GetClient(), cr)
			})
		})
	})
})
