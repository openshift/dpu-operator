package plugin

import (
	"context"
	"embed"
	"fmt"
	"net"
	"os"

	"github.com/go-logr/logr"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/openshift/dpu-operator/internal/utils"
	"github.com/openshift/dpu-operator/pkgs/render"
	opi "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed bindata/*
var binData embed.FS

var VspImages = []string{
	"IntelVspImage",
	"MarvellVspImage",
	// TODO: Add future supported vendor plugins here
}

func CreateVspImagesMap(fromEnv bool, logger logr.Logger) map[string]string {
	vspImages := make(map[string]string)

	for _, vspImageName := range VspImages {
		var value string

		if fromEnv {
			value = os.Getenv(vspImageName)
			if value == "" {
				logger.Info("VspImage env var not set", "VspImage", vspImageName)
			}
		}
		vspImages[vspImageName] = value
	}

	return vspImages
}

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
	k8sClient   client.Client
	opiClient   opi.BridgePortServiceClient
	nfclient    pb.NetworkFunctionServiceClient
	dpuMode     bool
	vspImage    string
	conn        *grpc.ClientConn
	pathManager utils.PathManager
}

func CreateVspImageVars(vspImage string) map[string]string {
	// All the CRs will be in the same namespace as the operator config
	return map[string]string{
		"Namespace":                 "openshift-dpu-operator",
		"ImagePullPolicy":           "Always",
		"VendorSpecificPluginImage": vspImage,
		"Command":                   "[ ]",
		"Args":                      "[ ]",
	}
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

func WithPathManager(pathManager utils.PathManager) func(*GrpcPlugin) {
	return func(d *GrpcPlugin) {
		d.pathManager = pathManager
	}
}

func WithVspImage(template_vars map[string]string) func(*GrpcPlugin) {
	return func(d *GrpcPlugin) {
		vspImage := template_vars["VendorSpecificPluginImage"]
		d.vspImage = vspImage
		d.log.Info("VSP Image", "vspImage", d.vspImage)
		if vspImage != "" {
			err := render.ApplyAllFromBinData(d.log, "vsp-ds", template_vars, binData, d.k8sClient, nil, nil)
			if err != nil {
				d.log.Error(err, "Failed to start vendor plugin container", "vspImage", d.vspImage)
			}
		}
	}
}

func NewGrpcPlugin(dpuMode bool, client client.Client, opts ...func(*GrpcPlugin)) *GrpcPlugin {
	gp := &GrpcPlugin{
		dpuMode:     dpuMode,
		vspImage:    "",
		k8sClient:   client,
		log:         ctrl.Log.WithName("GrpcPlugin"),
		pathManager: *utils.NewPathManager("/"),
	}

	for _, opt := range opts {
		opt(gp)
	}

	if gp.vspImage == "" {
		gp.log.Info("WARNING: VSP Image not set, skipping vendor plugin container startup")
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
