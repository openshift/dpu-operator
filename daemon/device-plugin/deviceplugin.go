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
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	VendorPluginSocketPath string = cnitypes.DaemonBaseDir + "vendor-plugin/vendor-plugin.sock"

	// Device plugin settings.
	pluginMountPath = "/var/lib/kubelet/device-plugins"
	kubeletEndpoint = "kubelet.sock"
	pluginEndpoint  = "sriovNet.sock"
	resourceName    = "openshift.io/dpu"
)

// nfResources manages NF networking devices
type nfResources struct {
	socketFile string
	devices    map[string]pluginapi.Device // for Kubelet DP API
	grpcServer *grpc.Server
	pluginapi.DevicePluginServer
	log           logr.Logger
	deviceHandler DeviceHandler
}

type DevicePlugin interface {
	Start() error
}

func (nf *nfResources) sendDevices(stream pluginapi.DevicePlugin_ListAndWatchServer, devices *DeviceList) error {
	resp := new(pluginapi.ListAndWatchResponse)
	for _, dev := range *devices {
		resp.Devices = append(resp.Devices, &pluginapi.Device{ID: dev.ID, Health: dev.Health})
	}

	nf.log.Info("SendDevices:", "resp", resp)
	if err := stream.Send(resp); err != nil {
		nf.log.Error(err, "Cannot send devices to ListAndWatch server")
		nf.grpcServer.Stop()
		return err
	}
	return nil
}

func (nf *nfResources) devicesEqual(d1, d2 *DeviceList) bool {
	if len(*d1) != len(*d2) {
		return false
	}

	for d1key, d1value := range *d1 {
		if d2value, ok := (*d2)[d1key]; !ok || d2value != d1value {
			return false
		}
	}

	return true
}

func (nf *nfResources) setDeviceCache(devices *DeviceList) {
	nf.devices = *devices
	for id, dev := range nf.devices {
		nf.log.Info("Cached device", "id", id, "dev.ID", dev.ID)
	}
}

func (nf *nfResources) checkCachedDeviceHealth(id string) (bool, error) {
	dev, ok := nf.devices[id]
	if !ok {
		return false, fmt.Errorf("invalid allocation request with non-existing device: %s", id)
	}
	return dev.Health == pluginapi.Healthy, nil
}

func (nf *nfResources) ListAndWatch(empty *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	oldDevices := make(DeviceList)
	for {
		newDevices, err := nf.deviceHandler.GetDevices()
		if err != nil {
			nf.log.Error(err, "Failed to get Devices")
			return err
		}
		if !nf.devicesEqual(&oldDevices, newDevices) {
			err := nf.sendDevices(stream, newDevices)
			if err != nil {
				nf.log.Error(err, "Failed to send Devices")
				return err
			}
			oldDevices = *newDevices
			nf.setDeviceCache(newDevices)
		}
		time.Sleep(5 * time.Second)
	}
}

// Allocate passes the dev name as an env variable to the requesting container
func (nf *nfResources) Allocate(ctx context.Context, rqt *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	resp := new(pluginapi.AllocateResponse)
	devName := ""
	for _, container := range rqt.ContainerRequests {
		containerResp := new(pluginapi.ContainerAllocateResponse)
		for _, id := range container.DevicesIDs {
			nf.log.Info("DeviceID in Allocate:", "id", id)
			isHealthy, err := nf.checkCachedDeviceHealth(id)
			if err != nil {
				return nil, err
			}
			nf.log.Info("DeviceID Health", "id", id, "isHealthy", isHealthy, "err", err)

			if !isHealthy {
				return nil, fmt.Errorf("invalid allocation request with unhealthy device: %s", id)
			}

			devName = devName + id + ","
		}

		nf.log.Info("Device(s) allocated:", "devName", devName)
		envmap := make(map[string]string)
		envmap["NF-DEV"] = devName

		containerResp.Envs = envmap
		resp.ContainerResponses = append(resp.ContainerResponses, containerResp)
	}
	return resp, nil
}

func (nf *nfResources) RegisterDevicePlugin() error {
	pluginEndpoint := filepath.Join(pluginapi.DevicePluginPath, nf.socketFile)
	nf.log.Info("Starting Device Plugin server at:", "pluginEndpoint", pluginEndpoint)
	lis, err := net.Listen("unix", pluginEndpoint)
	if err != nil {
		return fmt.Errorf("resource %s failed to listen to Device Plugin server: %v", resourceName, err)
	}
	nf.grpcServer = grpc.NewServer()

	kubeletEndpoint := filepath.Join("unix:", DeprecatedSockDir, KubeEndPoint)
	conn, err := grpc.Dial(kubeletEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("resource %s unable connect to Kubelet: %v", resourceName, err)
	}
	defer conn.Close()

	pluginapi.RegisterDevicePluginServer(nf.grpcServer, nf)

	client := pluginapi.NewRegistrationClient(conn)

	go func() {
		err := nf.grpcServer.Serve(lis)
		if err != nil {
			nf.log.Error(err, "Serving Device Plugin incoming requests failed.")
		}
	}()

	// Use connectWithRetry for the pluginEndpoint call
	conn, err = nf.connectWithRetry("unix:" + pluginEndpoint)
	if err != nil {
		return fmt.Errorf("resource %s unable to establish test connection with gRPC server: %v", resourceName, err)
	}
	nf.log.Info("Device plugin endpoint started serving:", "resourceName", resourceName)
	conn.Close()

	request := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     nf.socketFile,
		ResourceName: resourceName,
	}

	if _, err = client.Register(context.Background(), request); err != nil {
		return fmt.Errorf("unable to register resource %s with Kubelet: %v", resourceName, err)
	}
	nf.log.Info("Device plugin registered with Kubelet", "resourceName", resourceName)

	return nil
}

func (nf *nfResources) Start() error {
	err := nf.cleanup()
	if err != nil {
		return fmt.Errorf("failed to cleanup: %v", err)
	}

	err = nf.RegisterDevicePlugin()
	if err != nil {
		return fmt.Errorf("failed to register the device plugin: %v", err)
	}

	return nil
}

// connectWithRetry tries to establish a connection with the given endpoint, with retries.
func (nf *nfResources) connectWithRetry(endpoint string) (*grpc.ClientConn, error) {
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
		nf.log.Error(err, "Failed to establish connection with retry", "endpoint", endpoint)
		return nil, err
	}

	return conn, nil
}

// func (nf *nfResources) Stop() error {
// 	fmt.Printf("Stopping Device Plugin gRPC server..")
// 	if nf.grpcServer == nil {
// 		return nil
// 	}

// 	nf.grpcServer.Stop()
// 	nf.grpcServer = nil

// 	return nf.cleanup()
// }

func (nf *nfResources) cleanup() error {
	pluginEndpoint := filepath.Join(pluginapi.DevicePluginPath, nf.socketFile)
	if err := os.Remove(pluginEndpoint); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

func (nf *nfResources) PreStartContainer(ctx context.Context, psRqt *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

func (nf *nfResources) GetDevicePluginOptions(ctx context.Context, empty *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{
		PreStartRequired: false,
	}, nil
}

func NewDevicePlugin(dh DeviceHandler) *nfResources {
	return &nfResources{
		log:           ctrl.Log.WithName("DevicePlugin"),
		devices:       make(map[string]pluginapi.Device),
		socketFile:    pluginEndpoint,
		deviceHandler: dh,
	}
}
