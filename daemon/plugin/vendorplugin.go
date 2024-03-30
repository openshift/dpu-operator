package plugin

import (
	"context"
	"net"

	"github.com/go-logr/logr"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	opi "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	DaemonBaseDir          string = "/var/run/daemon/"
	VendorPluginSocketPath string = DaemonBaseDir + "vendor-plugin/vendor-plugin.sock"
)

type VendorPlugin interface {
	Start() (string, int32, error)
	Stop()
	CreateBridgePort(bpr *opi.CreateBridgePortRequest) error
	DeleteBridgePort(bpr *opi.DeleteBridgePortRequest) error
}

type GrpcPlugin struct {
	log       logr.Logger
	client    pb.LifeCycleServiceClient
	opiClient opi.BridgePortServiceClient
	dpuMode   bool
	conn      *grpc.ClientConn
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

func NewGrpcPlugin(dpuMode bool) *GrpcPlugin {
	return &GrpcPlugin{
		dpuMode: dpuMode,
		log:     ctrl.Log.WithName("GrpcPlugin"),
	}
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

	conn, err := grpc.DialContext(context.Background(), VendorPluginSocketPath, dialOptions...)

	if err != nil {
		g.log.Error(err, "Failed to connect to vendor plugin")
		return err
	}
	g.conn = conn

	g.client = pb.NewLifeCycleServiceClient(conn)
	g.opiClient = opi.NewBridgePortServiceClient(conn)
	return nil
}

func (g *GrpcPlugin) CreateBridgePort(createRequest *opi.CreateBridgePortRequest) error {
	g.ensureConnected()
	_, err := g.opiClient.CreateBridgePort(context.TODO(), createRequest)
	return err
}
func (g *GrpcPlugin) DeleteBridgePort(deleteRequest *opi.DeleteBridgePortRequest) error {
	g.ensureConnected()
	_, err := g.opiClient.DeleteBridgePort(context.TODO(), deleteRequest)
	return err
}
