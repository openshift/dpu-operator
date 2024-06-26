package daemon

import (
	"os"
	"time"

	g "github.com/onsi/ginkgo/v2"
	"k8s.io/client-go/rest"

	. "github.com/onsi/gomega"
	"github.com/openshift/dpu-operator/internal/testutils"
	"github.com/openshift/dpu-operator/internal/utils"
)

var _ = g.Describe("Main", func() {
	var (
		dpuDaemon   *DpuDaemon
		client      *rest.Config
		testCluster testutils.TestCluster
	)
	g.BeforeEach(func() {
		testCluster = testutils.TestCluster{Name: "dpu-operator-test-cluster"}
		client = testCluster.EnsureExists()
	})

	g.AfterEach(func() {
		if os.Getenv("FAST_TEST") == "false" {
			testCluster.EnsureDeleted()
		}
	})

	g.Context("Host daemon", func() {
		g.It("should respond to CNI calls", func() {
			pathManager := *utils.NewPathManager(testCluster.TempDirPath())

			dummyPluginDPU := NewDummyPlugin()
			dpuDaemon = NewDpuDaemon(dummyPluginDPU, &DummyDevicePlugin{},
				WithClient(client),
				WithPathManager(pathManager))
			dpuListen, err := dpuDaemon.Listen()
			Expect(err).NotTo(HaveOccurred())
			go func() {
				err = dpuDaemon.Serve(dpuListen)
				Expect(err).NotTo(HaveOccurred())
			}()
			time.Sleep(1 * time.Second)
			dpuDaemon.Stop()
		})
	})
})
