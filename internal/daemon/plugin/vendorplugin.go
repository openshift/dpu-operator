package plugin

import (
	"context"
	"fmt"
	"net"

	"github.com/go-logr/logr"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	opi "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	VendorPluginSocketPath string = cnitypes.DaemonBaseDir + "vendor-plugin/vendor-plugin.sock"
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
	log       logr.Logger
	client    pb.LifeCycleServiceClient
	opiClient opi.BridgePortServiceClient
	nfclient  pb.NetworkFunctionServiceClient
	dpuMode   bool
	conn      *grpc.ClientConn
}

func (g *GrpcPlugin) Start() (string, int32, error) {
	err := g.ensureConnected()
	if err != nil {
		return "", 0, fmt.Errorf("Failed to ensure GRPC connection on grpcPlugin start: %v", err)
	}
	ipPort, err := g.client.Init(context.TODO(), &pb.InitRequest{DpuMode: g.dpuMode})

	if err != nil {
		return "", 0, fmt.Errorf("Failed to start serving on grpcPlugin start: %v", err)
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
	g.nfclient = pb.NewNetworkFunctionServiceClient(conn)
	g.opiClient = opi.NewBridgePortServiceClient(conn)
	return nil
}

func (g *GrpcPlugin) CreateBridgePort(createRequest *opi.CreateBridgePortRequest) (*opi.BridgePort, error) {
	err := g.ensureConnected()
	if err != nil {
		return nil, fmt.Errorf("CreateBridgePort failed to ensure GRPC connection: %v", err)
	}
	return g.opiClient.CreateBridgePort(context.TODO(), createRequest)
}

func (g *GrpcPlugin) DeleteBridgePort(deleteRequest *opi.DeleteBridgePortRequest) error {
	err := g.ensureConnected()
	if err != nil {
		return fmt.Errorf("DeleteBridgePort failed to ensure GRPC connection: %v", err)
	}
	_, err = g.opiClient.DeleteBridgePort(context.TODO(), deleteRequest)
	return err
}

func (g *GrpcPlugin) CreateNetworkFunction(input string, output string) error {
	g.log.Info("CreateNetworkFunction", "input", input, "output", output)
	err := g.ensureConnected()
	if err != nil {
		return fmt.Errorf("CreateNetworkFunction failed to ensure GRPC connection: %v", err)
	}
	req := pb.NFRequest{Input: input, Output: output}
	_, err = g.nfclient.CreateNetworkFunction(context.TODO(), &req)
	return err
}

func (g *GrpcPlugin) DeleteNetworkFunction(input string, output string) error {
	g.log.Info("DeleteNetworkFunction", "input", input, "output", output)
	err := g.ensureConnected()
	if err != nil {
		return fmt.Errorf("DeleteNetworkFunction failed to ensure GRPC connection: %v", err)
	}
	req := pb.NFRequest{Input: input, Output: output}
	_, err = g.nfclient.DeleteNetworkFunction(context.TODO(), &req)
	return err
}
