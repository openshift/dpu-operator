package main_test

import (
	"flag"
	"os"
	// "path/filepath"
	// "time"

	g "github.com/onsi/ginkgo/v2"
	"go.uber.org/zap/zapcore"

	// . "github.com/onsi/gomega"

	"github.com/containernetworking/cni/pkg/skel"
	// current "github.com/containernetworking/cni/pkg/types/100"
	// . "github.com/openshift/dpu-operator/daemon"
	// "github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	// "github.com/openshift/dpu-operator/dpu-cni/pkgs/cni"
	opi "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type DummyPlugin struct {
}

func NewDummyPlugin() *DummyPlugin {
	return &DummyPlugin{}
}

func (v *DummyPlugin) Start() (string, int32, error) {
	return "127.0.0.1", 50051, nil
}

func (v *DummyPlugin) Stop() {

}

func (v *DummyPlugin) CreateBridgePort(createRequest *opi.CreateBridgePortRequest) (*opi.BridgePort, error) {
	return &opi.BridgePort{}, nil
}

func (v *DummyPlugin) DeleteBridgePort(deleteRequest *opi.DeleteBridgePortRequest) error {
	return nil
}

func PrepArgs(cniVersion string, command string) *skel.CmdArgs {
	cniConfig := "{\"cniVersion\": \"" + cniVersion + "\",\"name\": \"dpucni\",\"type\": \"dpucni\"}"
	cmdArgs := &skel.CmdArgs{
		ContainerID: "fakecontainerid",
		Netns:       "fakenetns",
		IfName:      "fakeeth0",
		Args:        "",
		Path:        "fakepath",
		StdinData:   []byte(cniConfig),
	}
	os.Clearenv()
	os.Setenv("CNI_COMMAND", command)
	os.Setenv("CNI_ARGS", "K8S_POD_NAMESPACE=x;K8S_POD_NAME=y;K8S_POD_UID=z")
	os.Setenv("CNI_CONTAINERID", cmdArgs.ContainerID)
	os.Setenv("CNI_NETNS", cmdArgs.Netns)
	os.Setenv("CNI_IFNAME", cmdArgs.IfName)
	os.Setenv("CNI_PATH", cmdArgs.Path)

	return cmdArgs
}

var _ = g.BeforeSuite(func() {
	opts := zap.Options{
		Development: true,
		Level:       zapcore.DebugLevel,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
})

var _ = g.Describe("Main", func() {
	g.Context("Connection", func() {
		// tmpDir, err := os.MkdirTemp("", "cniserver")
		// defer os.RemoveAll(tmpDir)
		// Expect(err).NotTo(HaveOccurred())
		// serverSocketPath := filepath.Join(tmpDir, "server.socket")
		// dummyPluginDPU := NewDummyPlugin()
		// DpuDaemon := NewDpuDaemon(dummyPluginDPU)

		// dummyPluginHost := NewDummyPlugin()
		// HostDaemon := NewHostDaemon(dummyPluginHost).WithCniServerPath(serverSocketPath)

		// g.It("Should connect successfully if the DPU Daemon is up first", func() {
		// 	DpuDaemon.Start()
		// 	go func() {
		// 		HostDaemon.Start()
		// 	}()
		// 	time.Sleep(time.Second)
		// 	_, err := HostDaemon.CreateBridgePort(1, 1, 1, "00:00:00:00:00:00")
		// 	Expect(err).ShouldNot(HaveOccurred())

		// 	HostDaemon.Stop()
		// 	DpuDaemon.Stop()
		// })
		// g.It("Should connect successfully if the Host Daemon is up first", func() {
		// 	go func() {
		// 		HostDaemon.Start()
		// 	}()
		// 	time.Sleep(time.Second)
		// 	DpuDaemon.Start()
		// 	_, err := HostDaemon.CreateBridgePort(1, 1, 1, "00:00:00:00:00:00")

		// 	Expect(err).ShouldNot(HaveOccurred())
		// 	HostDaemon.Stop()
		// 	DpuDaemon.Stop()
		// })
	})

	g.Context("Daemon on host should receive request from CNI", func() {
		// tmpDir, err := os.MkdirTemp("", "cniserver")
		// defer os.RemoveAll(tmpDir)
		// Expect(err).NotTo(HaveOccurred())
		// serverSocketPath := filepath.Join(tmpDir, "server.socket")

		// dummyPluginHost := NewDummyPlugin()
		// HostDaemon := NewHostDaemon(dummyPluginHost).WithCniServerPath(serverSocketPath)
		// go func() {
		// 	HostDaemon.Start()
		// }()
		// time.Sleep(time.Second)

		// p := &cni.Plugin{SocketPath: serverSocketPath}

		// g.Context("CNI propagetes to vendor plugin on DPU", func() {
		// 	g.When("Normal ADD request", func() {
		// 		cniVersion := "0.4.0"
		// 		expectedResult := &current.Result{
		// 			CNIVersion: cniVersion,
		// 		}
		// 		g.It("should get a correct response from the post request", func() {
		// 			resp, ver, err := p.PostRequest(PrepArgs(cniVersion, cnitypes.CNIAdd))
		// 			Expect(err).NotTo(HaveOccurred())
		// 			Expect(ver).To(Equal(cniVersion))
		// 			Expect(resp.Result).To(Equal(expectedResult))
		// 		})
		// 	})
		// })
	})
})
