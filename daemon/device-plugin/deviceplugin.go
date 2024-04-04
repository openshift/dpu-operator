package deviceplugin

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/go-logr/logr"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	pb "github.com/openshift/dpu-operator/tree/main/dpu-api/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	VendorPluginSocketPath string = cnitypes.DaemonBaseDir + "vendor-plugin/vendor-plugin.sock"

	// Device plugin settings.
	pluginMountPath = "/var/lib/kubelet/device-plugins"
	kubeletEndpoint = "kubelet.sock"
	pluginEndpoint  = "sriovNet.sock"
	resourceName    = "intel.com/ipu"
	prefix          = "intel.com"
)

// sriovManager manages sriov networking devices
type apfManager struct {
	socketFile string
	devices    map[string]pluginapi.Device // for Kubelet DP API
	grpcServer *grpc.Server
	pluginapi.DevicePluginServer
	log    logr.Logger
	client pb.DeviceServiceClient
	conn   *grpc.ClientConn
}

type DevicePlugin interface {
	Start() error
}

func (sm *apfManager) ListAndWatch(empty *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	changed := true
	for {
		for id, dev := range sm.devices {
			state := sm.GetDeviceState(id)
			if dev.Health != state {
				changed = true
				dev.Health = state
				sm.devices[id] = dev
			}
		}
		if changed {
			resp := new(pluginapi.ListAndWatchResponse)
			for _, dev := range sm.devices {
				resp.Devices = append(resp.Devices, &pluginapi.Device{ID: dev.ID, Health: dev.Health})
			}
			fmt.Printf("ListAndWatch: send devices %v\n", resp)
			if err := stream.Send(resp); err != nil {
				fmt.Printf("Error. Cannot update device states: %v\n", err)
				sm.grpcServer.Stop()
				return err
			}
		}
		changed = false
		time.Sleep(5 * time.Second)
	}
}

// Allocate passes the dev name as an env variable to the requesting container
func (sm *apfManager) Allocate(ctx context.Context, rqt *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	resp := new(pluginapi.AllocateResponse)
	devName := ""
	for _, container := range rqt.ContainerRequests {
		containerResp := new(pluginapi.ContainerAllocateResponse)
		for _, id := range container.DevicesIDs {
			fmt.Printf("DeviceID in Allocate: %v \n", id)
			dev, ok := sm.devices[id]
			if !ok {
				fmt.Printf("Error. Invalid allocation request with non-existing device %s", id)
			}
			if dev.Health != pluginapi.Healthy {
				fmt.Printf("Error. Invalid allocation request with unhealthy device %s", id)
			}

			devName = devName + id + ","
		}

		fmt.Printf("device(s) allocated: %s\n", devName)
		envmap := make(map[string]string)
		envmap["APF-DEV"] = devName

		containerResp.Envs = envmap
		resp.ContainerResponses = append(resp.ContainerResponses, containerResp)
	}
	return resp, nil
}

func (sm *apfManager) GetDeviceState(DeviceName string) string {
	// TODO: Discover device health
	return pluginapi.Healthy
}

func (sm *apfManager) Start() error {

	sm.ensureConnected()

	ctx := context.Background()

	Devices, err := sm.client.GetDevices(ctx, &emptypb.Empty{})

	for _, device := range Devices.Devices {
		sm.devices[device.ID] = pluginapi.Device{ID: device.ID, Health: pluginapi.Healthy}
	}

	for dev := range sm.devices {
		sm.log.Info(dev)
	}

	if err != nil {
		sm.log.Error(err, "Failed to handle GetDevices Request")
		return err
	}

	pluginEndpoint := filepath.Join(pluginapi.DevicePluginPath, sm.socketFile)
	fmt.Printf("Starting APF Device Plugin server at: %s\n", pluginEndpoint)
	lis, err := net.Listen("unix", pluginEndpoint)
	if err != nil {
		fmt.Printf("Error: Starting APF Device Plugin server failed: %v", err)
	}
	sm.grpcServer = grpc.NewServer()

	kubeletEndpoint := filepath.Join("unix:", DeprecatedSockDir, KubeEndPoint)

	conn, err := grpc.Dial(kubeletEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))

	if err != nil {
		fmt.Printf("%s device plugin unable connect to Kubelet : %v", resourceName, err)
		return err
	}
	defer conn.Close()

	pluginapi.RegisterDevicePluginServer(sm.grpcServer, sm)

	client := pluginapi.NewRegistrationClient(conn)

	go sm.grpcServer.Serve(lis)

	// Wait for server to start by launching a blocking connection
	ctx, _ = context.WithTimeout(context.TODO(), 5*time.Second)
	conn, err = grpc.DialContext(
		ctx, "unix:"+pluginEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())

	if err != nil {
		fmt.Printf("error. unable to establish test connection with %s gRPC server: %v", resourceName, err)
		return err
	}
	fmt.Printf("%s device plugin endpoint started serving \n", resourceName)
	conn.Close()

	ctx = context.Background()

	request := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     sm.socketFile,
		ResourceName: resourceName,
	}

	if _, err = client.Register(ctx, request); err != nil {
		fmt.Printf("%s device plugin unable to register with Kubelet : %v \n", resourceName, err)
		return err
	}
	fmt.Printf("%s device plugin registered with Kubelet\n", resourceName)

	return nil
}

func (g *apfManager) ensureConnected() error {
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

	g.client = pb.NewDeviceServiceClient(conn)
	return nil
}

func (sm *apfManager) Stop() error {
	fmt.Printf("Stopping Device Plugin gRPC server..")
	if sm.grpcServer == nil {
		return nil
	}

	sm.grpcServer.Stop()
	sm.grpcServer = nil

	return sm.cleanup()
}

func (sm *apfManager) cleanup() error {
	pluginEndpoint := filepath.Join(pluginapi.DevicePluginPath, sm.socketFile)
	if err := os.Remove(pluginEndpoint); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

func (sm *apfManager) PreStartContainer(ctx context.Context, psRqt *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

func (sm *apfManager) GetDevicePluginOptions(ctx context.Context, empty *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{
		PreStartRequired: false,
	}, nil
}

func NewGrpcPlugin() *apfManager {
	return &apfManager{
		log:        ctrl.Log.WithName("GrpcPlugin"),
		devices:    make(map[string]pluginapi.Device),
		socketFile: pluginEndpoint,
	}
}
