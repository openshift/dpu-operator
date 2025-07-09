package daemon

import (
	"context"
	"os"
	"time"

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
		// Call to client might hang if we don't put a bound on how long it can run
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		return client.List(ctx, &nodes)
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

var _ = g.Describe("DPU side manager", Ordered, func() {
	var (
		dpuDaemon     *DpuSideManager
		config        *rest.Config
		testCluster   testutils.KindCluster
		client        client.Client
		ctx           context.Context
		cancel        context.CancelFunc
		mockVspDone   chan error
		dpuDaemonDone chan error
	)
	g.BeforeEach(func() {
		testCluster = testutils.KindCluster{Name: "dpu-operator-test-cluster"}
		config = testCluster.EnsureExists()

		pathManager := *utils.NewPathManager(testCluster.TempDirPath())

		ctx, cancel = context.WithCancel(context.Background())

		mockVsp := mockvsp.NewMockVsp(mockvsp.WithPathManager(pathManager))
		mockVspListen, err := mockVsp.Listen()
		Expect(err).NotTo(HaveOccurred())
		mockVspDone = make(chan error, 1)
		go func() {
			err = mockVsp.Serve(ctx, mockVspListen)
			mockVspDone <- err
		}()

		dpuPlugin, err := plugin.NewGrpcPlugin(true, "testDpuIdentifier",
			client,
			plugin.WithPathManager(pathManager))
		Expect(err).NotTo(HaveOccurred())
		dpuDaemon, err = NewDpuSideManager(dpuPlugin, config, WithPathManager(pathManager))
		Expect(err).NotTo(HaveOccurred())
		err = dpuDaemon.StartVsp()
		Expect(err).NotTo(HaveOccurred())
		err = dpuDaemon.SetupDevices()
		Expect(err).NotTo(HaveOccurred())

		dpuListen, err := dpuDaemon.Listen()
		Expect(err).NotTo(HaveOccurred())
		dpuDaemonDone = make(chan error, 1)
		go func() {
			err = dpuDaemon.Serve(ctx, dpuListen)
			dpuDaemonDone <- err
		}()
		client = dpuDaemon.manager.GetClient()
	})

	g.AfterEach(func() {
		if os.Getenv("FAST_TEST") == "false" {
			testCluster.EnsureDeleted()
		}
		cancel()
		<-mockVspDone
		<-dpuDaemonDone
	})

	g.Context("Device Plugin", func() {
		g.It("Should allocate devices", func() {
			waitAllNodesDpuAllocatable(client)
		})
	})
})
