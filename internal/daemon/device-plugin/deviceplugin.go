package deviceplugin

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	"github.com/openshift/dpu-operator/internal/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	resourceName = "openshift.io/dpu"
)

// dpServer manages the k8s Device Plugin Server
type dpServer struct {
	devices    map[string]pluginapi.Device // for Kubelet DP API
	grpcServer *grpc.Server
	pluginapi.DevicePluginServer
	log           logr.Logger
	pathManager   utils.PathManager
	deviceHandler DeviceHandler
}

type DevicePlugin interface {
	Start() error
}

func (dp *dpServer) sendDevices(stream pluginapi.DevicePlugin_ListAndWatchServer, devices *DeviceList) error {
	resp := new(pluginapi.ListAndWatchResponse)
	for _, dev := range *devices {
		resp.Devices = append(resp.Devices, &dev)
	}

	dp.log.Info("SendDevices:", "resp", resp)
	if err := stream.Send(resp); err != nil {
		dp.log.Error(err, "Cannot send devices to ListAndWatch server")
		dp.grpcServer.Stop()
		return err
	}
	return nil
}

func (dp *dpServer) devicesEqual(d1, d2 *DeviceList) bool {
	if len(*d1) != len(*d2) {
		return false
	}

	for d1key, d1value := range *d1 {
		if d2value, ok := (*d2)[d1key]; !ok || !reflect.DeepEqual(d1value, d2value) {
			return false
		}
	}

	return true
}

func (dp *dpServer) setDeviceCache(devices *DeviceList) {
	dp.devices = *devices
	for id, dev := range dp.devices {
		dp.log.Info("Cached device", "id", id, "dev.ID", dev.ID)
	}
}

func (dp *dpServer) checkCachedDeviceHealth(id string) (bool, error) {
	dev, ok := dp.devices[id]
	if !ok {
		return false, fmt.Errorf("invalid allocation request with non-existing device: %s", id)
	}
	return dev.Health == pluginapi.Healthy, nil
}

func (dp *dpServer) ListAndWatch(empty *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	oldDevices := make(DeviceList)
	for {
		newDevices, err := dp.deviceHandler.GetDevices()
		if err != nil {
			dp.log.Error(err, "Failed to get Devices")
			return err
		}
		if !dp.devicesEqual(&oldDevices, newDevices) {
			err := dp.sendDevices(stream, newDevices)
			if err != nil {
				dp.log.Error(err, "Failed to send Devices")
				return err
			}
			oldDevices = *newDevices
			dp.setDeviceCache(newDevices)
		}
		time.Sleep(5 * time.Second)
	}
}

// Allocate passes the dev name as an env variable to the requesting container
func (dp *dpServer) Allocate(ctx context.Context, rqt *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	resp := new(pluginapi.AllocateResponse)
	devName := ""
	for _, container := range rqt.ContainerRequests {
		containerResp := new(pluginapi.ContainerAllocateResponse)
		for _, id := range container.DevicesIDs {
			dp.log.Info("DeviceID in Allocate:", "id", id)
			isHealthy, err := dp.checkCachedDeviceHealth(id)
			if err != nil {
				return nil, err
			}
			dp.log.Info("DeviceID Health", "id", id, "isHealthy", isHealthy, "err", err)

			if !isHealthy {
				return nil, fmt.Errorf("invalid allocation request with unhealthy device: %s", id)
			}

			devName = devName + id + ","
		}

		dp.log.Info("Device(s) allocated:", "devName", devName)
		envmap := make(map[string]string)
		envmap["NF-DEV"] = devName

		containerResp.Envs = envmap
		resp.ContainerResponses = append(resp.ContainerResponses, containerResp)
	}
	return resp, nil
}

func (dp *dpServer) RegisterDevicePlugin() error {
	pluginEndpoint := dp.pathManager.PluginEndpoint()
	dp.log.Info("Starting Device Plugin server at:", "pluginEndpoint", pluginEndpoint)
	lis, err := net.Listen("unix", pluginEndpoint)
	if err != nil {
		return fmt.Errorf("resource %s failed to listen to Device Plugin server: %v", resourceName, err)
	}
	dp.grpcServer = grpc.NewServer()

	kubeletEndpoint := filepath.Join("unix:", dp.pathManager.KubeletEndPoint())
	conn, err := grpc.Dial(kubeletEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("resource %s unable connect to Kubelet: %v", resourceName, err)
	}
	defer conn.Close()

	pluginapi.RegisterDevicePluginServer(dp.grpcServer, dp)

	client := pluginapi.NewRegistrationClient(conn)

	go func() {
		err := dp.grpcServer.Serve(lis)
		if err != nil {
			dp.log.Error(err, "Serving Device Plugin incoming requests failed.")
		}
	}()

	// Use connectWithRetry for the pluginEndpoint call
	conn, err = dp.connectWithRetry("unix:" + pluginEndpoint)
	if err != nil {
		return fmt.Errorf("resource %s unable to establish test connection with gRPC server: %v", resourceName, err)
	}
	dp.log.Info("Device plugin endpoint started serving:", "resourceName", resourceName)
	conn.Close()

	request := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     utils.PluginEndpointSocket,
		ResourceName: resourceName,
	}

	if _, err = client.Register(context.Background(), request); err != nil {
		return fmt.Errorf("unable to register resource %s with Kubelet: %v", resourceName, err)
	}
	dp.log.Info("Device plugin registered with Kubelet", "resourceName", resourceName)

	return nil
}

func (dp *dpServer) Start() error {
	err := dp.cleanup()
	if err != nil {
		return fmt.Errorf("failed to cleanup: %v", err)
	}

	err = dp.RegisterDevicePlugin()
	if err != nil {
		return fmt.Errorf("failed to register the device plugin: %v", err)
	}

	return nil
}

// connectWithRetry tries to establish a connection with the given endpoint, with retries.
func (dp *dpServer) connectWithRetry(endpoint string) (*grpc.ClientConn, error) {
	var conn *grpc.ClientConn
	var err error

	retryPolicy := `{
		"methodConfig": [{
		  "waitForReady": true,
		  "retryPolicy": {
			  "MaxAttempts": 40,
			  "InitialBackoff": "1s",
			  "MaxBackoff": "16s",
			  "BackoffMultiplier": 2.0,
			  "RetryableStatusCodes": [ "UNAVAILABLE" ]
		  }
		}]}`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err = grpc.DialContext(
		ctx,
		endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithDefaultServiceConfig(retryPolicy),
	)
	if err != nil {
		dp.log.Error(err, "Failed to establish connection with retry", "endpoint", endpoint)
		return nil, err
	}

	return conn, nil
}

// func (dp *dpServer) Stop() error {
// 	fmt.Printf("Stopping Device Plugin gRPC server..")
// 	if dp.grpcServer == nil {
// 		return nil
// 	}

// 	dp.grpcServer.Stop()
// 	dp.grpcServer = nil

// 	return dp.cleanup()
// }

func (dp *dpServer) cleanup() error {
	pluginEndpoint := dp.pathManager.PluginEndpoint()
	if err := os.Remove(pluginEndpoint); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

func (dp *dpServer) PreStartContainer(ctx context.Context, psRqt *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

func (dp *dpServer) GetDevicePluginOptions(ctx context.Context, empty *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{
		PreStartRequired: false,
	}, nil
}

func WithPathManager(pathManager utils.PathManager) func(*dpServer) {
	return func(d *dpServer) {
		d.pathManager = pathManager
	}
}

func NewDevicePlugin(dh DeviceHandler, opts ...func(*dpServer)) *dpServer {
	dp := &dpServer{
		log:           ctrl.Log.WithName("DevicePlugin"),
		pathManager:   *utils.NewPathManager("/"),
		devices:       make(map[string]pluginapi.Device),
		deviceHandler: dh,
	}

	for _, opt := range opts {
		opt(dp)
	}

	return dp
}
