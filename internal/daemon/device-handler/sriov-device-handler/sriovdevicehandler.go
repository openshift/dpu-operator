package sriovdevicehandler

import (
	"context"
	"fmt"
	"net"

	"github.com/go-logr/logr"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	dp "github.com/openshift/dpu-operator/internal/daemon/device-plugin"
	"github.com/openshift/dpu-operator/internal/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// sriovDeviceHandler handles NF networking devices
type sriovDeviceHandler struct {
	log logr.Logger
	// Connection client to the API for the VSP DeviceService
	client           pb.DeviceServiceClient
	conn             *grpc.ClientConn
	pathManager      utils.PathManager
	setupDevicesDone chan struct{}
	dpuMode          bool
}

func (s *sriovDeviceHandler) GetDevices() (*dp.DeviceList, error) {
	// Wait for devices to be done initializing
	<-s.setupDevicesDone

	err := s.ensureConnected()
	if err != nil {
		return nil, fmt.Errorf("failed to ensure connection to plugin: %v", err)
	}

	Devices, err := s.client.GetDevices(context.Background(), &pb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("failed to handle GetDevices request: %v", err)
	}

	devices := make(dp.DeviceList)

	for _, device := range Devices.Devices {
		devices[device.ID] = pluginapi.Device{ID: device.ID, Health: pluginapi.Healthy}
	}

	return &devices, nil
}

// ensureConnected makes sure we are connected to the VSP's gRPC
func (s *sriovDeviceHandler) ensureConnected() error {
	if s.client != nil {
		return nil
	}
	dialOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.Dial("unix", addr)
		}),
	}
	conn, err := grpc.DialContext(context.Background(), s.pathManager.VendorPluginSocket(), dialOptions...)
	if err != nil {
		return fmt.Errorf("failed to connect to vendor plugin: %v", err)
	}
	s.conn = conn

	s.client = pb.NewDeviceServiceClient(s.conn)
	s.log.Info("Connected to DeviceServiceClient")
	return nil
}

// SetupDevices
func (s *sriovDeviceHandler) SetupDevices() error {
	// Currently NF devices do not require any setup outside the VSP
	if s.dpuMode {
		s.log.Info(("Dpu mode detected, skipping devHandler devices setup"))
		return nil
	}

	s.setupDevicesDone = make(chan struct{})

	defer close(s.setupDevicesDone)

	err := s.ensureConnected()
	if err != nil {
		return fmt.Errorf("failed to ensure connection to vsp: %v", err)
	}

	vfCount := &pb.VfCount{
		VfCnt: 8,
	}

	numVfs, err := s.client.SetNumVfs(context.Background(), vfCount)
	if err != nil {
		return fmt.Errorf("Failed to set sriov numVfs: %v", err)
	}

	if numVfs.VfCnt == 0 {
		return fmt.Errorf("SetNumVfs ran, but numVfs == 0")
	}

	s.log.Info("Num vfs set to %d by vsp", numVfs)

	return nil
}

func NewSriovDeviceHandler(opts ...func(*sriovDeviceHandler)) *sriovDeviceHandler {
	devHandler := &sriovDeviceHandler{
		log:     ctrl.Log.WithName("SriovDeviceHandler"),
		dpuMode: false,
	}

	for _, opt := range opts {
		opt(devHandler)
	}

	// TODO: When changing the SRIOV numVfs, we should do the following:
	// 1) Drain all pods running on the node with a drain controller running
	// on the control plane. The nodes will be marked for draining and read by
	// the drain controller.
	// 2) Clearly define which PF and how many VFs from an API. User facing or
	// otherwise
	err := devHandler.SetupDevices()
	if err != nil {
		devHandler.log.Error(err, "Failed to setup devices")
	}

	return devHandler
}
