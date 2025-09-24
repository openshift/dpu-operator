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
	b := NewDpuOperatorConfigReconciler(mgr.GetClient(), mockImageManager)
	err = b.SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	// Note: Webhooks are not set up in tests due to envtest limitations
	// The controller will handle finalizer addition for test scenarios

	wg.Add(1)
	go func() {
		defer GinkgoRecover()
		setupLog.Info("starting manager")
		err := mgr.Start(ctx)
		if err != nil {
			setupLog.Error(err, "Manager failed to start")
		}
		Expect(err).NotTo(HaveOccurred())
		wg.Done()
	}()

	// Wait for the manager to be elected (controller ready)
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

		// Ensure clean state - remove any existing DpuOperatorConfig resources
		crList := &configv1.DpuOperatorConfigList{}
		err := mgr.GetClient().List(context.Background(), crList)
		if err == nil {
			for _, existingCr := range crList.Items {
				// Remove finalizers first to allow deletion
				if len(existingCr.Finalizers) > 0 {
					existingCr.Finalizers = []string{}
					err := mgr.GetClient().Update(context.Background(), &existingCr)
					if err != nil && !errors.IsNotFound(err) {
						setupLog.Error(err, "Failed to remove finalizers from existing DpuOperatorConfig", "name", existingCr.Name)
					}
				}
				err := mgr.GetClient().Delete(context.Background(), &existingCr)
				if err != nil && !errors.IsNotFound(err) {
					setupLog.Error(err, "Failed to delete existing DpuOperatorConfig", "name", existingCr.Name)
				}
			}
		}

		// Wait for all DpuOperatorConfig resources to be deleted
		Eventually(func() int {
			crList := &configv1.DpuOperatorConfigList{}
			err := mgr.GetClient().List(context.Background(), crList)
			Expect(err).NotTo(HaveOccurred())
			return len(crList.Items)
		}, testutils.TestAPITimeout, testutils.TestRetryInterval).Should(Equal(0))
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
				cr = testutils.DpuOperatorCR(testDpuOperatorConfigName, ns)

				// Ensure any existing CR is cleaned up first
				existingCr := testutils.DpuOperatorCR(testDpuOperatorConfigName, ns)
				testutils.DeleteDpuOperatorCR(mgr.GetClient(), existingCr)

				testutils.CreateNamespace(mgr.GetClient(), ns)
				testutils.CreateDpuOperatorCR(mgr.GetClient(), cr)

				// Wait for DpuOperatorConfig to be Ready
				testutils.EventuallyDpuOperatorConfigReady(mgr.GetClient(), setupLog, cr, testutils.TestAPITimeout*10, testutils.TestRetryInterval)
			})
			It("should have DPU daemon daemonsets created by controller manager", func() {
				daemonSet := appsv1.DaemonSet{}
				testutils.WaitForDaemonSetReady(&daemonSet, mgr.GetClient(), testNamespace, testDpuDaemonName)
			})
			It("should have the network function NAD created by controller manager", func() {
				nad := &netattdefv1.NetworkAttachmentDefinition{}
				Eventually(func() error {
					return mgr.GetClient().Get(context.Background(), types.NamespacedName{Namespace: "default", Name: testNetworkFunctionNADHost}, nad)
				}, testutils.TestAPITimeout*3, testutils.TestRetryInterval).ShouldNot(HaveOccurred())
			})
			AfterAll(func() {
				ns := testutils.DpuOperatorNamespace()
				cr = testutils.DpuOperatorCR(testDpuOperatorConfigName, ns)
				testutils.DeleteDpuOperatorCR(mgr.GetClient(), cr)
				testutils.EventuallyNoDpuOperatorConfig(mgr.GetClient(), testutils.TestAPITimeout*2, testutils.TestRetryInterval)
			})
		})

		Context("When DpuOperatorConfig CR is created with dpu mode", func() {
			BeforeAll(func() {
				ns := testutils.DpuOperatorNamespace()
				cr = testutils.DpuOperatorCR("operator-config", ns)

				// Ensure any existing CR is cleaned up first
				existingCr := testutils.DpuOperatorCR("operator-config", ns)
				testutils.DeleteDpuOperatorCR(mgr.GetClient(), existingCr)

				testutils.CreateNamespace(mgr.GetClient(), ns)
				testutils.CreateDpuOperatorCR(mgr.GetClient(), cr)

				// Wait for DpuOperatorConfig to be Ready
				testutils.EventuallyDpuOperatorConfigReady(mgr.GetClient(), setupLog, cr, testutils.TestAPITimeout*10, testutils.TestRetryInterval)
			})
			It("should have DPU daemon daemonsets created by controller manager", func() {
				daemonSet := &appsv1.DaemonSet{}
				testutils.WaitForDaemonSetReady(daemonSet, mgr.GetClient(), testNamespace, testDpuDaemonName)
			})
			It("should have the network function NAD created by controller manager", func() {
				nad := &netattdefv1.NetworkAttachmentDefinition{}
				Eventually(func() error {
					return mgr.GetClient().Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: testNetworkFunctionNADDpu}, nad)
				}, testutils.TestAPITimeout*3, testutils.TestRetryInterval).ShouldNot(HaveOccurred())
			})
			AfterAll(func() {
				ns := testutils.DpuOperatorNamespace()
				cr = testutils.DpuOperatorCR("operator-config", ns)
				testutils.DeleteDpuOperatorCR(mgr.GetClient(), cr)
				testutils.EventuallyNoDpuOperatorConfig(mgr.GetClient(), testutils.TestAPITimeout*2, testutils.TestRetryInterval)
			})
		})

		Context("When testing finalizer functionality", func() {
			var cr *configv1.DpuOperatorConfig

			It("should be Ready when DpuOperatorConfig is created", func() {
				ns := testutils.DpuOperatorNamespace()
				cr = testutils.DpuOperatorCR(testDpuOperatorConfigName, ns)

				// Ensure any existing CR is cleaned up first
				existingCr := testutils.DpuOperatorCR(testDpuOperatorConfigName, ns)
				testutils.DeleteDpuOperatorCR(mgr.GetClient(), existingCr)

				testutils.CreateNamespace(mgr.GetClient(), ns)
				testutils.CreateDpuOperatorCR(mgr.GetClient(), cr)

				testutils.EventuallyDpuOperatorConfigReady(mgr.GetClient(), setupLog, cr, testutils.TestAPITimeout, testutils.TestRetryInterval)
			})

			It("should delete DpuOperatorConfig successfully", func() {
				ns := testutils.DpuOperatorNamespace()
				cr := testutils.DpuOperatorCR(testDpuOperatorConfigName, ns)
				testutils.DeleteDpuOperatorCR(mgr.GetClient(), cr)
				testutils.EventuallyNoDpuOperatorConfig(mgr.GetClient(), testutils.TestAPITimeout*2, testutils.TestRetryInterval)
			})
		})
	})
})
