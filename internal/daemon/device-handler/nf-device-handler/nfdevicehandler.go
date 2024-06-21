package nfdevicehandler

import (
	"context"
	"fmt"
	"net"

	"github.com/go-logr/logr"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	dp "github.com/openshift/dpu-operator/internal/daemon/device-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	VendorPluginSocketPath string = cnitypes.DaemonBaseDir + "vendor-plugin/vendor-plugin.sock"
)

// nfDeviceHandler handles NF networking devices
type nfDeviceHandler struct {
	log logr.Logger
	// Connection client to the API for the VSP DeviceService
	client pb.DeviceServiceClient
	// Connection to the VSP
	conn *grpc.ClientConn
}

// ensureConnected makes sure we are connected to the VSP's gRPC
func (nf *nfDeviceHandler) ensureConnected() error {
	if nf.client != nil {
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
		return fmt.Errorf("failed to connect to vendor plugin: %v", err)
	}
	nf.conn = conn

	nf.client = pb.NewDeviceServiceClient(nf.conn)
	nf.log.Info("Connected to DeviceServiceClient")
	return nil
}

// GetDevices for NFs come from the VSP which will handle the detection of the devices
func (nf *nfDeviceHandler) GetDevices() (*dp.DeviceList, error) {
	err := nf.ensureConnected()
	if err != nil {
		return nil, fmt.Errorf("failed to ensure connection to plugin: %v", err)
	}

	Devices, err := nf.client.GetDevices(context.Background(), &pb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("failed to handle GetDevices request: %v", err)
	}

	devices := make(dp.DeviceList)

	for _, device := range Devices.Devices {
		devices[device.ID] = pluginapi.Device{ID: device.ID, Health: pluginapi.Healthy}
	}

	return &devices, nil
}

// Currently NF devices do not require any setup outside the VSP
func (nf *nfDeviceHandler) SetupDevices() error {
	return nil
}

func NewNfDeviceHandler() *nfDeviceHandler {
	devHandler := &nfDeviceHandler{
		log: ctrl.Log.WithName("NfDeviceHandler"),
	}

	err := devHandler.SetupDevices()
	if err != nil {
		devHandler.log.Error(err, "Failed to setup devices")
	}

	return devHandler
}
