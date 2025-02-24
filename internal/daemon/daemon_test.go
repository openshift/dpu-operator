package daemon

import (
	"context"
	"time"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/klog/v2"

	mockvsp "github.com/openshift/dpu-operator/internal/daemon/vendor-specific-plugins/mock-vsp"
	"github.com/openshift/dpu-operator/internal/platform"
	"github.com/openshift/dpu-operator/internal/testutils"
	"github.com/openshift/dpu-operator/internal/utils"
	"github.com/openshift/dpu-operator/internal/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"github.com/spf13/afero"
)

func createVspTestImages() map[string]string {
	vspImages := make(map[string]string)
	vspImages["IntelVspImage"] = "Intel-image"
	vspImages["MarvellVspImage"] = "Marvell-image"
	return vspImages
}

var _ = g.Describe("Full Daemon", func() {
	var (
		testCluster  *testutils.KindCluster
		daemon       Daemon
		fakePlatform *platform.FakePlatform
		cancel       context.CancelFunc
		k8sClient    client.Client
	)
	g.BeforeEach(func() {
		var ctx context.Context
		var err error
		testCluster = &testutils.KindCluster{Name: "dpu-operator-test-cluster"}
		config := testCluster.EnsureExists()
		pathManager := utils.NewPathManager(testCluster.TempDirPath())
		k8sClient, err = client.New(config, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())
		ns := testutils.DpuOperatorNamespace()
		cr := testutils.DpuOperatorCR("dpu-operator-config", "host", ns)
		testutils.CreateNamespace(k8sClient, ns)
		testutils.CreateDpuOperatorCR(k8sClient, cr)

		fs := afero.NewMemMapFs()
		utils.Touch(fs, "/dpu-cni")
		fakePlatform = platform.NewFakePlatform("IPU Adapter E2100-CCQDA2")

		mockVsp := mockvsp.NewMockVsp(mockvsp.WithPathManager(*pathManager))
		mockVspListen, err := mockVsp.Listen()
		Expect(err).NotTo(HaveOccurred())
		go func() {
			err = mockVsp.Serve(mockVspListen)
			Expect(err).NotTo(HaveOccurred())
		}()

		daemon = NewDaemon(fs, fakePlatform, "dpu", config, createVspTestImages(), pathManager)
		ctx, cancel = context.WithCancel(context.Background())
		go func() {
			daemon.Start(ctx)
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
		daemon.Wait()
		ns := testutils.DpuOperatorNamespace()
		cr := testutils.DpuOperatorCR("dpu-operator-config", "host", ns)
		testutils.DeleteDpuOperatorCR(k8sClient, cr)
	})
})
