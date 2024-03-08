package plugin

import (
	"context"
	"net"

	"github.com/go-logr/logr"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	VendorPluginDir        string = "/var/run/daemon/vendor-plugin"
	VendorPluginSocketPath string = VendorPluginDir + "/vendor-plugin.sock"
)

type VendorPlugin interface {
	Start() (string, int32, error)
	Stop()
}

type DummyPlugin struct {
	log logr.Logger
}

func NewDummyPlugin() *DummyPlugin {
	return &DummyPlugin{
		log: ctrl.Log.WithName("VSP"),
	}
}

func (v *DummyPlugin) Start() (string, int32, error) {
	return "127.0.0.1", 50051, nil
}

func (v *DummyPlugin) Stop() {

}

type GrpcPlugin struct {
	log     logr.Logger
	client  pb.LifeCycleServiceClient
	dpuMode bool
	conn    *grpc.ClientConn
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
	return nil
}
