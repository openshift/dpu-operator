package plugin

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/openshift/dpu-operator/internal/utils"
	opi "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const ReadyConditionType = "Ready"

type DpuIdentifier string

type VendorPlugin interface {
	Start(ctx context.Context) (string, int32, error)
	Close()
	CreateBridgePort(bpr *opi.CreateBridgePortRequest) (*opi.BridgePort, error)
	DeleteBridgePort(bpr *opi.DeleteBridgePortRequest) error
	CreateNetworkFunction(input string, output string) error
	DeleteNetworkFunction(input string, output string) error
	GetDevices() (*pb.DeviceListResponse, error)
	SetNumVfs(vfCount int32) (*pb.VfCount, error)
	RebootDpu(nodeName, pciAddress string) (*pb.ManualOperationResponse, error)
	UpgradeSdk(nodeName, pciAddress string) (*pb.ManualOperationResponse, error)
}

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
	conn          *grpc.ClientConn
	pathManager   utils.PathManager
	initialized   bool
	initMutex     sync.RWMutex
	moclient    pb.ManualOperationServiceClient
}

func (g *GrpcPlugin) Start(ctx context.Context) (string, int32, error) {
	start := time.Now()
	interval := 100 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			return "", 0, ctx.Err()
		default:
		}

		err := g.ensureConnected()
		if err != nil {
			select {
			case <-ctx.Done():
				return "", 0, ctx.Err()
			case <-time.After(interval):
			}
			continue
		}

		ipPort, err := g.client.Init(ctx, &pb.InitRequest{DpuMode: g.dpuMode, DpuIdentifier: string(g.dpuIdentifier)})
		if err != nil {
			if strings.Contains(err.Error(), "already initialized") {
				// VSP was already initialized, mark as initialized and return the error
				g.SetInitDone(false)
				return "", 0, err
			}
			select {
			case <-ctx.Done():
				return "", 0, ctx.Err()
			case <-time.After(interval):
			}
			continue
		}

		// Init succeeded, mark as initialized
		g.SetInitDone(true)

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

func NewGrpcPlugin(dpuMode bool, dpuIdentifier DpuIdentifier, client client.Client, opts ...func(*GrpcPlugin)) (*GrpcPlugin, error) {
	gp := &GrpcPlugin{
		dpuMode:       dpuMode,
		dpuIdentifier: dpuIdentifier,
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
	g.moclient = pb.NewManualOperationServiceClient(conn)
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

// IsInitialized returns true if the VSP has been successfully initialized
func (g *GrpcPlugin) IsInitialized() bool {
	g.initMutex.RLock()
	defer g.initMutex.RUnlock()
	return g.initialized
}

// SetInitDone sets the initialization status with proper mutex locking
func (g *GrpcPlugin) SetInitDone(initialized bool) {
	g.initMutex.Lock()
	defer g.initMutex.Unlock()
	g.initialized = initialized
}

func (g *GrpcPlugin) RebootDpu(nodeName, pciAddress string) (*pb.ManualOperationResponse, error) {
	err := g.ensureConnected()
	if err != nil {
		return nil, fmt.Errorf("SetNumvfs failed to ensure GRPC connection: %v", err)
	}
	pciInfo := &pb.ManualOperationRequest{
		NodeName: nodeName,
		PciAddress: pciAddress,
	}
	return g.moclient.ManualRebootDpuFunction(context.Background(), pciInfo)
}

func (g *GrpcPlugin) UpgradeSdk(nodeName, pciAddress string) (*pb.ManualOperationResponse, error) {
	err := g.ensureConnected()
	if err != nil {
		return nil, fmt.Errorf("SetNumvfs failed to ensure GRPC connection: %v", err)
	}
	pciInfo := &pb.ManualOperationRequest{
		NodeName: nodeName,
		PciAddress: pciAddress,
	}
	return g.moclient.ManualUpgradeSdkFunction(context.Background(), pciInfo)
}