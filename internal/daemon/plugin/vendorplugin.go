package plugin

import (
	"context"
	"embed"
	"fmt"
	"net"
	"time"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/dpu-operator/api/v1"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/openshift/dpu-operator/internal/scheme"
	"github.com/openshift/dpu-operator/internal/utils"
	"github.com/openshift/dpu-operator/pkgs/render"
	"github.com/openshift/dpu-operator/pkgs/vars"
	opi "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed bindata/*
var binData embed.FS

type DpuIdentifier string

type VendorPlugin interface {
	Start() (string, int32, error)
	Close()
	CreateBridgePort(bpr *opi.CreateBridgePortRequest) (*opi.BridgePort, error)
	DeleteBridgePort(bpr *opi.DeleteBridgePortRequest) error
	CreateNetworkFunction(input string, output string) error
	DeleteNetworkFunction(input string, output string) error
	GetDevices() (*pb.DeviceListResponse, error)
	SetNumVfs(vfCount int32) (*pb.VfCount, error)
}

type GrpcPlugin struct {
	log           logr.Logger
	client        pb.LifeCycleServiceClient
	k8sClient     client.Client
	opiClient     opi.BridgePortServiceClient
	nfclient      pb.NetworkFunctionServiceClient
	dsClient      pb.DeviceServiceClient
	dpuMode       bool
	dpuIdentifier DpuIdentifier
	vsp           VspTemplateVars
	conn          *grpc.ClientConn
	pathManager   utils.PathManager
}

func NewVspTemplateVars() VspTemplateVars {
	return VspTemplateVars{
		VendorSpecificPluginImage: "",
		Namespace:                 vars.Namespace,
		ImagePullPolicy:           "Always",
		Command:                   "[ ]",
		Args:                      "[ ]",
	}
}

type VspTemplateVars struct {
	VendorSpecificPluginImage string
	Namespace                 string
	ImagePullPolicy           string
	Command                   string
	Args                      string
}

func (v VspTemplateVars) ToMap() map[string]string {
	return map[string]string{
		"VendorSpecificPluginImage": v.VendorSpecificPluginImage,
		"Namespace":                 v.Namespace,
		"ImagePullPolicy":           v.ImagePullPolicy,
		"Command":                   v.Command,
		"Args":                      v.Args,
	}
}

func (g *GrpcPlugin) Start() (string, int32, error) {
	start := time.Now()
	timeout := 10 * time.Second
	interval := 100 * time.Millisecond

	err := g.deployVsp()
	if err != nil {
		return "", 0, err
	}

	for {
		err := g.ensureConnected()
		if err != nil {
			if time.Since(start) >= timeout {
				return "", 0, fmt.Errorf("failed to ensure GRPC connection after %v: %v", timeout, err)
			}
			time.Sleep(interval)
			continue
		}

		ipPort, err := g.client.Init(context.TODO(), &pb.InitRequest{DpuMode: g.dpuMode, DpuIdentifier: string(g.dpuIdentifier)})
		if err != nil {
			if time.Since(start) >= timeout {
				return "", 0, fmt.Errorf("failed to start serving after %v: %v", timeout, err)
			}
			time.Sleep(interval)
			continue
		}

		g.log.Info("GrpcPlugin Start() succeeded", "duration", time.Since(start), "ip", ipPort.Ip, "port", ipPort.Port, "dpuMode",
			g.dpuMode, "dpuIdentifier", g.dpuIdentifier)
		return ipPort.Ip, ipPort.Port, nil
	}
}

func (g *GrpcPlugin) Close() {
	if g.conn != nil {
		g.conn.Close()
		g.conn = nil
		g.client = nil
		g.nfclient = nil
		g.opiClient = nil
		g.dsClient = nil
	}
}

func WithPathManager(pathManager utils.PathManager) func(*GrpcPlugin) {
	return func(d *GrpcPlugin) {
		d.pathManager = pathManager
	}
}

func WithVsp(template_vars VspTemplateVars) func(*GrpcPlugin) {
	return func(d *GrpcPlugin) {
		d.vsp = template_vars
		d.log.Info("Setting VSP", "vsp", d.vsp.VendorSpecificPluginImage)
	}
}

func (gp *GrpcPlugin) deployVsp() error {
	vspImage := gp.vsp.VendorSpecificPluginImage

	// It is not mandatory that a vsp image is provided. If not, we can assume this will be handled by the user and still return a GrpcClient
	if vspImage == "" {
		gp.log.Info("WARNING: VSP Image not set, skipping vendor plugin container startup")
		return nil
	}

	// Retrieve the Dpu Operator Config which owns the Dpu Daemonset so we can ensure the vsp shares the same owner reference.
	dpuOperatorConfig := &configv1.DpuOperatorConfig{}
	err := gp.k8sClient.Get(context.TODO(), client.ObjectKey{Name: vars.DpuOperatorConfigName, Namespace: vars.Namespace}, dpuOperatorConfig)
	if err != nil {
		return fmt.Errorf("encountered error when retrieving DpuOperatorConfig %s: %v", vars.DpuOperatorConfigName, err)
	}

	gp.log.Info("Deploying VSP", "vspImage", vspImage, "command", gp.vsp.Command, "args", gp.vsp.Args)
	err = render.ApplyAllFromBinData(gp.log, "vsp-ds", gp.vsp.ToMap(), binData, gp.k8sClient, dpuOperatorConfig, scheme.Scheme)
	if err != nil {
		return fmt.Errorf("failed to start vendor plugin container (vspImage: %s): %v", vspImage, err)
	}

	return nil
}

func NewGrpcPlugin(dpuMode bool, dpuIdentifier DpuIdentifier, client client.Client, opts ...func(*GrpcPlugin)) (*GrpcPlugin, error) {
	gp := &GrpcPlugin{
		dpuMode:       dpuMode,
		dpuIdentifier: dpuIdentifier,
		vsp:           VspTemplateVars{},
		k8sClient:     client,
		log:           ctrl.Log.WithName("GrpcPlugin"),
		pathManager:   *utils.NewPathManager("/"),
	}

	for _, opt := range opts {
		opt(gp)
	}

	return gp, nil
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
	g.dsClient = pb.NewDeviceServiceClient(conn)
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

func (g *GrpcPlugin) GetDevices() (*pb.DeviceListResponse, error) {
	err := g.ensureConnected()
	if err != nil {
		return nil, fmt.Errorf("GetDevices failed to ensure GRPC connection: %v", err)
	}
	return g.dsClient.GetDevices(context.Background(), &pb.Empty{})
}

func (g *GrpcPlugin) SetNumVfs(count int32) (*pb.VfCount, error) {
	err := g.ensureConnected()
	if err != nil {
		return nil, fmt.Errorf("SetNumvfs failed to ensure GRPC connection: %v", err)
	}
	c := &pb.VfCount{
		VfCnt: count,
	}
	return g.dsClient.SetNumVfs(context.Background(), c)
}
