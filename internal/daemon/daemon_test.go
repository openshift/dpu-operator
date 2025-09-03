package daemon_test

import (
	"context"
	"fmt"
	"os"
	"time"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/internal/daemon"
	mockvsp "github.com/openshift/dpu-operator/internal/daemon/vendor-specific-plugins/mock-vsp"
	"github.com/openshift/dpu-operator/internal/images"
	"github.com/openshift/dpu-operator/internal/platform"
	"github.com/openshift/dpu-operator/internal/scheme"
	"github.com/openshift/dpu-operator/internal/testutils"
	"github.com/openshift/dpu-operator/internal/utils"
	"github.com/openshift/dpu-operator/pkgs/vars"
	"github.com/spf13/afero"
)

func createVspTestImages() images.ImageManager {
	return images.NewDummyImageManager()
}

func EventuallyNoDpuCR(k8sClient client.Client) {
	Eventually(func() error {
		dpuList := &v1.DataProcessingUnitList{}
		err := k8sClient.List(context.TODO(), dpuList)
		if err != nil {
			return err
		}
		if len(dpuList.Items) != 0 {
			klog.Infof("Found %v DPU CRs, still waiting for cleanup", len(dpuList.Items))
			return fmt.Errorf("Found %v DPU CRs but expecting 0", len(dpuList.Items))
		}
		klog.Info("All DPU CRs cleaned up successfully")
		return nil
	}, testutils.TestAPITimeout*3, testutils.TestRetryInterval).Should(Succeed())
}

var _ = g.Describe("Full Daemon", func() {
	var (
		testCluster  *testutils.KindCluster
		d            daemon.Daemon
		fakePlatform *platform.FakePlatform
		cancel       context.CancelFunc
		mockVspDone  chan struct{}
		daemonDone   chan struct{}
		k8sClient    client.Client
	)
	g.BeforeEach(func() {
		var ctx context.Context
		var err error
		ctx, cancel = context.WithCancel(context.Background())

		// Initialize completion channels
		mockVspDone = make(chan struct{})
		daemonDone = make(chan struct{})

		testCluster = &testutils.KindCluster{Name: "dpu-operator-test-cluster"}
		config := testCluster.EnsureExists()
		pathManager := utils.NewPathManager(testCluster.TempDirPath())
		k8sClient, err = client.New(config, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())
		ns := testutils.DpuOperatorNamespace()
		cr := testutils.DpuOperatorCR("dpu-operator-config", ns)

		// Clean up any existing resources first
		existingCr := testutils.DpuOperatorCR("dpu-operator-config", ns)
		testutils.DeleteDpuOperatorCR(k8sClient, existingCr)

		testutils.CreateNamespace(k8sClient, ns)
		testutils.CreateDpuOperatorCR(k8sClient, cr)

		fs := afero.NewMemMapFs()
		utils.Touch(fs, "/dpu-cni")
		fakePlatform = platform.NewFakePlatform("IPU Adapter E2100-CCQDA2")

		// Create DpuOperatorConfig resource required by VSP plugin
		k8sClient, err := client.New(config, client.Options{})
		Expect(err).NotTo(HaveOccurred())
		namespace := testutils.DpuOperatorNamespace()
		dpuOperatorConfig := testutils.DpuOperatorCR(vars.DpuOperatorConfigName, namespace)

		// Clean up any existing resources first
		existingDpuOperatorConfig := testutils.DpuOperatorCR(vars.DpuOperatorConfigName, namespace)
		testutils.DeleteDpuOperatorCR(k8sClient, existingDpuOperatorConfig)

		testutils.CreateNamespace(k8sClient, namespace)
		testutils.CreateDpuOperatorCR(k8sClient, dpuOperatorConfig)

		mockVsp := mockvsp.NewMockVsp(mockvsp.WithPathManager(*pathManager))
		mockVspListen, err := mockVsp.Listen()
		Expect(err).NotTo(HaveOccurred())
		go func() {
			defer g.GinkgoRecover()
			defer close(mockVspDone)
			err = mockVsp.Serve(ctx, mockVspListen)
			// Context cancellation is expected, so check for that specifically
			if err != nil && err != context.Canceled {
				Expect(err).NotTo(HaveOccurred())
			}
		}()

		d = daemon.NewDaemon(fs, fakePlatform, "dpu", config, createVspTestImages(), pathManager, "test-node")
		go func() {
			defer close(daemonDone)
			err := d.PrepareAndServe(ctx)
			if err != nil && err != context.Canceled {
				Expect(err).NotTo(HaveOccurred())
			}
		}()
	})

	g.Context("Running on a DPU", func() {
		g.It("Should have one DPU CR with IsDpuSide set", func() {
			// First, wait for the DPU CR to be created
			Eventually(func() error {
				dpuCR := &v1.DataProcessingUnit{}
				err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: "intel-ipu"}, dpuCR)
				if err != nil {
					return err
				}

				Expect(err).NotTo(HaveOccurred())
				Expect(dpuCR.Spec.IsDpuSide).To(BeTrue())
				Expect(dpuCR.Spec.DpuProductName).To(Equal("Intel IPU"))
				return nil
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})
	})

	g.AfterEach(func() {
		klog.Info("Calling cancel")
		cancel()
		klog.Info("Cancel called, waiting for go routines for vsp and daemon to complete")

		// Wait for goroutines to complete with a timeout
		timeout := time.After(10 * time.Second)
		select {
		case <-mockVspDone:
			klog.Info("Mock VSP completed")
		case <-timeout:
			klog.Error(nil, "Timeout waiting for mock VSP to complete")
		}

		select {
		case <-daemonDone:
			klog.Info("Daemon completed")
		case <-timeout:
			klog.Error(nil, "Timeout waiting for daemon to complete")
		}
		klog.Info("Clean up DpuOperatorConfig resources")
		namespace := testutils.DpuOperatorNamespace()
		dpuOperatorConfig := testutils.DpuOperatorCR(vars.DpuOperatorConfigName, namespace)
		klog.Infof("Deleting DpuOperatorConfig: %s", vars.DpuOperatorConfigName)
		testutils.DeleteDpuOperatorCR(k8sClient, dpuOperatorConfig)

		// Also clean up the original CR
		cr := testutils.DpuOperatorCR("dpu-operator-config", namespace)
		klog.Info("Deleting original CR: dpu-operator-config")
		testutils.DeleteDpuOperatorCR(k8sClient, cr)
		klog.Infof("Deleting namespace: %s", namespace.Name)
		testutils.DeleteNamespace(k8sClient, namespace)

		klog.Info("Ensuring that there is no DPU CR anymore")
		EventuallyNoDpuCR(k8sClient)

		if os.Getenv("FAST_TEST") == "false" {
			testCluster.EnsureDeleted()
		}
	})
})
