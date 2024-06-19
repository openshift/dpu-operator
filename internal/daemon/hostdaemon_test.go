package daemon

import (
	"flag"
	"os"
	"path/filepath"

	g "github.com/onsi/ginkgo/v2"
	"go.uber.org/zap/zapcore"
	"k8s.io/klog/v2"

	. "github.com/onsi/gomega"

	"github.com/containernetworking/cni/pkg/skel"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cni"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	opi "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type DummyDevicePlugin struct{}

func (d *DummyDevicePlugin) Start() error {
	return nil
}

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

func (g *DummyPlugin) CreateNetworkFunction(input string, output string) error {
	return nil
}

func (g *DummyPlugin) DeleteNetworkFunction(input string, output string) error {
	return nil
}

type SriovManagerStub struct{}

func (m SriovManagerStub) SetupVF(conf *cnitypes.NetConf, podifName string, netns ns.NetNS) error {
	return nil
}

func (m SriovManagerStub) ReleaseVF(conf *cnitypes.NetConf, podifName string, netns ns.NetNS) error {
	return nil
}

func (m SriovManagerStub) ResetVFConfig(conf *cnitypes.NetConf) error {
	return nil
}

func (m SriovManagerStub) ApplyVFConfig(conf *cnitypes.NetConf) error {
	return nil
}

func (m SriovManagerStub) FillOriginalVfInfo(conf *cnitypes.NetConf) error {
	return nil
}

func (m SriovManagerStub) CmdAdd(req *cnitypes.PodRequest) (*current.Result, error) {
	result := &current.Result{}
	result.CNIVersion = req.CNIConf.CNIVersion
	return result, nil
}

func (m SriovManagerStub) CmdDel(req *cnitypes.PodRequest) error {
	return nil
}

func PrepArgs(cniVersion string, command string) *skel.CmdArgs {
	cniConfig := "{\"cniVersion\": \"" + cniVersion + "\",\"name\": \"dpucni\",\"type\": \"dpucni\", \"OrigVfState\": {\"EffectiveMac\": \"00:11:22:33:44:55\"}, \"vlan\": 7}"
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

func cmdAdd(cniVersion string, serverSocketPath string) (*cnitypes.Response, string, error) {
	p := &cni.Plugin{SocketPath: serverSocketPath}
	return p.PostRequest(PrepArgs(cniVersion, cnitypes.CNIAdd))
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
	var (
		tmpDir           string
		err              error
		serverSocketPath string
		dpuDaemon        *DpuDaemon
		hostDaemon       *HostDaemon
	)
	g.BeforeEach(func() {
		tmpDir, err = os.MkdirTemp("", "cniserver")
		Expect(err).NotTo(HaveOccurred())
		serverSocketPath = filepath.Join(tmpDir, "server.socket")

		dummyPluginDPU := NewDummyPlugin()
		dpuDaemon = NewDpuDaemon(dummyPluginDPU, &DummyDevicePlugin{})
		dummyPluginHost := NewDummyPlugin()
		m := SriovManagerStub{}
		hostDaemon = NewHostDaemon(dummyPluginHost, &DummyDevicePlugin{}).
			WithCniServerPath(serverSocketPath).
			WithSriovManager(m)
	})

	g.AfterEach(func() {
		klog.Info("Cleaning up")
		os.RemoveAll(tmpDir)
		dpuDaemon.Stop()
		hostDaemon.Stop()
	})

	g.Context("Host daemon", func() {
		g.It("should respond to CNI calls", func() {
			dpuListen, err := dpuDaemon.Listen()
			Expect(err).NotTo(HaveOccurred())
			go func() {
				err = dpuDaemon.Serve(dpuListen)
				Expect(err).NotTo(HaveOccurred())
			}()

			hostListen, err := hostDaemon.Listen()
			Expect(err).NotTo(HaveOccurred())
			go func() {
				hostDaemon.Serve(hostListen)
			}()

			cniVersion := "0.4.0"
			expectedResult := &current.Result{
				CNIVersion: cniVersion,
			}

			resp, ver, err := cmdAdd(cniVersion, serverSocketPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(ver).To(Equal(cniVersion))
			Expect(resp.Result).To(Equal(expectedResult))
		})
	})
})
