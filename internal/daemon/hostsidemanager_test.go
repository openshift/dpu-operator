package daemon

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"

	g "github.com/onsi/ginkgo/v2"
	"go.uber.org/zap/zapcore"
	"k8s.io/klog/v2"

	. "github.com/onsi/gomega"

	"github.com/containernetworking/cni/pkg/skel"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ns"
	pb2 "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cni"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	"github.com/openshift/dpu-operator/internal/testutils"
	"github.com/openshift/dpu-operator/internal/utils"
	opi "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"google.golang.org/grpc"
	emptypb "google.golang.org/protobuf/types/known/emptypb"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type DummyDevicePlugin struct{}

func (d *DummyDevicePlugin) ListenAndServe() error {
	return nil
}

func (d *DummyDevicePlugin) Stop() error {
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

type DummyDpuDaemon struct {
	pb.UnimplementedBridgePortServiceServer
	server      *grpc.Server
	bridgePorts int
}

func (s *DummyDpuDaemon) CreateBridgePort(context context.Context, bpr *pb.CreateBridgePortRequest) (*pb.BridgePort, error) {
	s.bridgePorts += 1
	return &pb.BridgePort{}, nil
}

func (s *DummyDpuDaemon) DeleteBridgePort(context context.Context, bpr *pb.DeleteBridgePortRequest) (*emptypb.Empty, error) {
	s.bridgePorts -= 1
	return &emptypb.Empty{}, nil
}

func (d *DummyDpuDaemon) Listen() (net.Listener, error) {
	addr := "127.0.0.1"
	port := 50051
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", addr, port))
	if err != nil {
		return lis, fmt.Errorf("Failed to start to listen on addr %v port %v", addr, port)
	}
	return lis, nil
}

func (d *DummyDpuDaemon) Serve(listen net.Listener) error {
	d.server = grpc.NewServer()
	pb.RegisterBridgePortServiceServer(d.server, d)
	if err := d.server.Serve(listen); err != nil {
		return fmt.Errorf("Fialed to start serving: %v", err)
	}
	return nil
}

func (d *DummyDpuDaemon) Stop() {
	d.server.Stop()
}

func (d *DummyPlugin) GetDevices() (*pb2.DeviceListResponse, error) {
	ret := pb2.DeviceListResponse{}
	return &ret, nil
}

func (g *DummyPlugin) SetNumVfs(count int32) (*pb2.VfCount, error) {
	c := &pb2.VfCount{
		VfCnt: count,
	}
	return c, nil
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

var _ = g.Describe("Host Daemon", func() {
	var (
		err           error
		fakeDpuDaemon *DummyDpuDaemon
		hostDaemon    *HostSideManager
		testCluster   *testutils.KindCluster
		pathManager   *utils.PathManager
	)
	g.BeforeEach(func() {
		testCluster = &testutils.KindCluster{Name: "dpu-operator-test-cluster"}
		client := testCluster.EnsureExists()
		pathManager = utils.NewPathManager(testCluster.TempDirPath())
		Expect(err).NotTo(HaveOccurred())
		fakeDpuDaemon = &DummyDpuDaemon{}
		dummyPluginHost := NewDummyPlugin()
		m := SriovManagerStub{}
		hostDaemon = NewHostSideManager(dummyPluginHost, WithPathManager2(pathManager), WithSriovManager(m), WithClient(client))
	})

	g.AfterEach(func() {
		klog.Info("Cleaning up")
		fakeDpuDaemon.Stop()
		hostDaemon.Stop()
	})

	g.Context("CNI Server", func() {
		g.It("should respond to CNI calls", func() {
			dpuListen, err := fakeDpuDaemon.Listen()
			Expect(err).NotTo(HaveOccurred())
			go func() {
				err = fakeDpuDaemon.Serve(dpuListen)
				Expect(err).NotTo(HaveOccurred())
			}()

			hostListen, err := hostDaemon.Listen()
			Expect(err).NotTo(HaveOccurred())
			go func() {
				err := hostDaemon.Serve(hostListen)
				Expect(err).NotTo(HaveOccurred())
			}()

			cniVersion := "0.4.0"
			expectedResult := &current.Result{
				CNIVersion: cniVersion,
			}

			resp, ver, err := cmdAdd(cniVersion, pathManager.CNIServerPath())
			Expect(err).NotTo(HaveOccurred())
			Expect(ver).To(Equal(cniVersion))
			Expect(resp.Result).To(Equal(expectedResult))

			Expect(fakeDpuDaemon.bridgePorts).To(Equal(1))
		})
	})
})
