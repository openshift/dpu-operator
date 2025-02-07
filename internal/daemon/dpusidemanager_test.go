package daemon

import (
	"context"
	"os"

	g "github.com/onsi/ginkgo/v2"
	"k8s.io/client-go/rest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	deviceplugin "github.com/openshift/dpu-operator/internal/daemon/device-plugin"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	mockvsp "github.com/openshift/dpu-operator/internal/daemon/vendor-specific-plugins/mock-vsp"
	"github.com/openshift/dpu-operator/internal/testutils"
	"github.com/openshift/dpu-operator/internal/utils"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func waitAllNodesDpuAllocatable(client client.Client) {
	var nodes corev1.NodeList
	Eventually(func() error {
		return client.List(context.Background(), &nodes)
	}, testutils.TestAPITimeout, testutils.TestRetryInterval).Should(Succeed())

	Eventually(func() bool {
		var latestNodes corev1.NodeList
		if err := client.List(context.Background(), &latestNodes); err != nil {
			return false
		}
		readyNodes := 0
		for _, node := range latestNodes.Items {
			allocatableQuantity, ok := node.Status.Allocatable[deviceplugin.DpuResourceName]
			if ok {
				allocatable, _ := allocatableQuantity.AsInt64()
				if allocatable > 0 {
					readyNodes++
				}
			}
		}
		return readyNodes == len(latestNodes.Items)
	}, testutils.TestInitialSetupTimeout, testutils.TestRetryInterval).Should(BeTrue())
}

var _ = g.Describe("DPU side maanger", Ordered, func() {
	var (
		dpuDaemon   *DpuSideManager
		config      *rest.Config
		testCluster testutils.KindCluster
		client      client.Client
	)
	g.BeforeEach(func() {
		testCluster = testutils.KindCluster{Name: "dpu-operator-test-cluster"}
		config = testCluster.EnsureExists()

		pathManager := *utils.NewPathManager(testCluster.TempDirPath())

		mockVsp := mockvsp.NewMockVsp(mockvsp.WithPathManager(pathManager))
		mockVspListen, err := mockVsp.Listen()
		Expect(err).NotTo(HaveOccurred())
		go func() {
			err = mockVsp.Serve(mockVspListen)
			Expect(err).NotTo(HaveOccurred())
		}()

		dpuPlugin := plugin.NewGrpcPlugin(true,
			client,
			plugin.WithPathManager(pathManager))
		dpuDaemon = NewDpuSideManger(dpuPlugin, config, WithPathManager(pathManager))

		dpuListen, err := dpuDaemon.Listen()
		Expect(err).NotTo(HaveOccurred())
		go func() {
			err = dpuDaemon.Serve(dpuListen)
			Expect(err).NotTo(HaveOccurred())
		}()
		client = dpuDaemon.manager.GetClient()
	})

	g.AfterEach(func() {
		if os.Getenv("FAST_TEST") == "false" {
			testCluster.EnsureDeleted()
		}
		dpuDaemon.Stop()
	})

	g.Context("Device Plugin", func() {
		g.It("Should allocate devices", func() {
			waitAllNodesDpuAllocatable(client)
		})
	})
})
