package plugin

import (
	"context"
	"net"

	"github.com/go-logr/logr"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/openshift/dpu-operator/internal/utils"
	opi "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	ctrl "sigs.k8s.io/controller-runtime"
)

type VendorPlugin interface {
	Start() (string, int32, error)
	Stop()
	CreateBridgePort(bpr *opi.CreateBridgePortRequest) (*opi.BridgePort, error)
	DeleteBridgePort(bpr *opi.DeleteBridgePortRequest) error
	CreateNetworkFunction(input string, output string) error
	DeleteNetworkFunction(input string, output string) error
}

type GrpcPlugin struct {
	log         logr.Logger
	client      pb.LifeCycleServiceClient
	opiClient   opi.BridgePortServiceClient
	nfclient    pb.NetworkFunctionServiceClient
	dpuMode     bool
	conn        *grpc.ClientConn
	pathManager utils.PathManager
}

func (g *GrpcPlugin) Start() (string, int32, error) {
	g.ensureConnected()
	ipPort, err := g.client.Init(context.TODO(), &pb.InitRequest{DpuMode: g.dpuMode})

	if err != nil {
		g.log.Error(err, "Failed to start serving")
		return "", 0, err
	}

	return ipPort.Ip, ipPort.Port, nil
}

func (g *GrpcPlugin) Stop() {
	g.conn.Close()
}

func WithPathManager(pathManager utils.PathManager) func(*GrpcPlugin) {
	return func(d *GrpcPlugin) {
		d.pathManager = pathManager
	}
}

func NewGrpcPlugin(dpuMode bool, opts ...func(*GrpcPlugin)) *GrpcPlugin {
	gp := &GrpcPlugin{
		dpuMode:     dpuMode,
		log:         ctrl.Log.WithName("GrpcPlugin"),
		pathManager: *utils.NewPathManager("/"),
	}

	for _, opt := range opts {
		opt(gp)
	}

	return gp
}

func (g *GrpcPlugin) ensureConnected() error {
	if g.client != nil {
		return nil
	}
	dialOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.Dial("unix", addr)
		}),
	}

	conn, err := grpc.DialContext(context.Background(), g.pathManager.VendorPluginSocket(), dialOptions...)

	if err != nil {
		g.log.Error(err, "Failed to connect to vendor plugin")
		return err
	}
	g.conn = conn

	g.client = pb.NewLifeCycleServiceClient(conn)
	g.nfclient = pb.NewNetworkFunctionServiceClient(conn)
	g.opiClient = opi.NewBridgePortServiceClient(conn)
	return nil
}

func (g *GrpcPlugin) CreateBridgePort(createRequest *opi.CreateBridgePortRequest) (*opi.BridgePort, error) {
	g.ensureConnected()
	return g.opiClient.CreateBridgePort(context.TODO(), createRequest)
}

func (g *GrpcPlugin) DeleteBridgePort(deleteRequest *opi.DeleteBridgePortRequest) error {
	g.ensureConnected()
	_, err := g.opiClient.DeleteBridgePort(context.TODO(), deleteRequest)
	return err
}

func (g *GrpcPlugin) CreateNetworkFunction(input string, output string) error {
	g.log.Info("CreateNetworkFunction", "input", input, "output", output)
	g.ensureConnected()
	req := pb.NFRequest{Input: input, Output: output}
	_, err := g.nfclient.CreateNetworkFunction(context.TODO(), &req)
	return err
}

func (g *GrpcPlugin) DeleteNetworkFunction(input string, output string) error {
	g.log.Info("DeleteNetworkFunction", "input", input, "output", output)
	g.ensureConnected()
	req := pb.NFRequest{Input: input, Output: output}
	_, err := g.nfclient.DeleteNetworkFunction(context.TODO(), &req)
	return err
}
