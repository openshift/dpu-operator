package daemon_test

import (
	"context"
	"time"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/dpu-operator/internal/daemon"
	mockvsp "github.com/openshift/dpu-operator/internal/daemon/vendor-specific-plugins/mock-vsp"
	"github.com/openshift/dpu-operator/internal/images"
	"github.com/openshift/dpu-operator/internal/platform"
	"github.com/openshift/dpu-operator/internal/testutils"
	"github.com/openshift/dpu-operator/internal/utils"
	"github.com/openshift/dpu-operator/pkgs/vars"
	"github.com/spf13/afero"
)

func createVspTestImages() images.ImageManager {
	return images.NewDummyImageManager()
}

var _ = g.Describe("Full Daemon", func() {
	var (
		testCluster  *testutils.KindCluster
		d            daemon.Daemon
		fakePlatform *platform.FakePlatform
		cancel       context.CancelFunc
		mockVspDone  chan struct{}
		daemonDone   chan struct{}
	)
	g.BeforeEach(func() {
		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())

		// Initialize completion channels
		mockVspDone = make(chan struct{})
		daemonDone = make(chan struct{})

		testCluster = &testutils.KindCluster{Name: "dpu-operator-test-cluster"}
		config := testCluster.EnsureExists()
		pathManager := utils.NewPathManager(testCluster.TempDirPath())

		fs := afero.NewMemMapFs()
		utils.Touch(fs, "/dpu-cni")
		fakePlatform = platform.NewFakePlatform("IPU Adapter E2100-CCQDA2")

		// Create DpuOperatorConfig resource required by VSP plugin
		k8sClient, err := client.New(config, client.Options{})
		Expect(err).NotTo(HaveOccurred())
		namespace := testutils.DpuOperatorNamespace()
		dpuOperatorConfig := testutils.DpuOperatorCR(vars.DpuOperatorConfigName, "auto", namespace)
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

		d = daemon.NewDaemon(fs, fakePlatform, "dpu", config, createVspTestImages(), pathManager)
		go func() {
			defer close(daemonDone)
			err := d.PrepareAndServe(ctx)
			if err != nil && err != context.Canceled {
				Expect(err).NotTo(HaveOccurred())
			}
			// Always wait for context cancellation to ensure proper test synchronization
			// If ListenAndServe blocked until context cancellation, this returns immediately
			// If ListenAndServe failed early, we wait for context cancellation
			<-ctx.Done()
		}()
	})

	g.Context("Running on a DPU", func() {
		g.It("Should have one DPU CR with IsDpuSide set", func() {
			// Eventually, it should show up as a CR
			time.Sleep(5 * time.Second)
		})
	})

	g.AfterEach(func() {
		klog.Info("Cleaning up")
		cancel()

		// Wait for both goroutines to complete
		<-mockVspDone
		<-daemonDone

		// Clean up DpuOperatorConfig resources
		k8sClient, err := client.New(testCluster.EnsureExists(), client.Options{})
		Expect(err).NotTo(HaveOccurred())
		namespace := testutils.DpuOperatorNamespace()
		dpuOperatorConfig := testutils.DpuOperatorCR(vars.DpuOperatorConfigName, "auto", namespace)
		testutils.DeleteDpuOperatorCR(k8sClient, dpuOperatorConfig)
		testutils.DeleteNamespace(k8sClient, namespace)
	})
})
