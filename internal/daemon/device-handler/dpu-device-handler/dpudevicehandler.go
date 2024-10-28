package dpudevicehandler

import (
	"context"
	"fmt"
	"net"

	"github.com/go-logr/logr"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/sriovutils"
	dp "github.com/openshift/dpu-operator/internal/daemon/device-plugin"
	"github.com/openshift/dpu-operator/internal/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// dpuDeviceHandler handles NF networking devices
type dpuDeviceHandler struct {
	log logr.Logger
	// Connection client to the API for the VSP DeviceService
	client           pb.DeviceServiceClient
	conn             *grpc.ClientConn
	pathManager      utils.PathManager
	setupDevicesDone chan struct{}
	dpuMode          bool
}

func normalizeDeviceToPci(device string) (string, error) {

	if sriovutils.IsValidPCIAddress(device) {
		return device, nil
	}

	pciAddr, err := sriovutils.GetPciFromNetDev(device)
	if err != nil {
		return device, fmt.Errorf("failed to get PCI address for netdev %s: %v", device, err)
	}

	return pciAddr, nil
}

func (d *dpuDeviceHandler) GetDevices() (*dp.DeviceList, error) {
	// Wait for devices to be done initializing
	<-d.setupDevicesDone

	err := d.ensureConnected()
	if err != nil {
		return nil, fmt.Errorf("failed to ensure connection to plugin: %v", err)
	}

	Devices, err := d.client.GetDevices(context.Background(), &pb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("failed to handle GetDevices request: %v", err)
	}

	devices := make(dp.DeviceList)

	for _, device := range Devices.Devices {
		devPciId, err := normalizeDeviceToPci(device.ID)
		if err != nil {
			return nil, fmt.Errorf("Failed to normalize device %s from GetDevice request: %v", device.ID, err)
		}
		devices[devPciId] = pluginapi.Device{ID: devPciId, Health: pluginapi.Healthy}
	}

	return &devices, nil
}

// ensureConnected makes sure we are connected to the VSP's gRPC
func (d *dpuDeviceHandler) ensureConnected() error {
	if d.client != nil {
		return nil
	}
	dialOptions := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.Dial("unix", addr)
		}),
	}
	conn, err := grpc.DialContext(context.Background(), d.pathManager.VendorPluginSocket(), dialOptions...)
	if err != nil {
		return fmt.Errorf("failed to connect to vendor plugin: %v", err)
	}
	d.conn = conn

	d.client = pb.NewDeviceServiceClient(d.conn)
	d.log.Info("Connected to DeviceServiceClient")
	return nil
}

// SetupDevices
func (d *dpuDeviceHandler) SetupDevices() error {
	d.setupDevicesDone = make(chan struct{})

	defer close(d.setupDevicesDone)

	// Currently NF devices do not require any setup outside the VSP
	if d.dpuMode {
		d.log.Info(("Dpu mode detected, skipping devHandler devices setup"))
		return nil
	}

	err := d.ensureConnected()
	if err != nil {
		return fmt.Errorf("failed to ensure connection to vsp: %v", err)
	}

	vfCount := &pb.VfCount{
		VfCnt: 8,
	}

	numVfs, err := d.client.SetNumVfs(context.Background(), vfCount)
	if err != nil {
		return fmt.Errorf("Failed to set sriov numVfs: %v", err)
	}

	if numVfs.VfCnt == 0 {
		return fmt.Errorf("SetNumVfs ran, but numVfs == 0")
	}

	d.log.Info("Num VFs set by VSP", "vf_count", numVfs.VfCnt)

	return nil
}

func WithDpuMode(dpuMode bool) func(*dpuDeviceHandler) {
	return func(d *dpuDeviceHandler) {
		d.dpuMode = dpuMode
	}
}

func WithPathManager(pathManager utils.PathManager) func(*dpuDeviceHandler) {
	return func(d *dpuDeviceHandler) {
		d.pathManager = pathManager
	}
}

func NewDpuDeviceHandler(opts ...func(*dpuDeviceHandler)) *dpuDeviceHandler {
	devHandler := &dpuDeviceHandler{
		log:     ctrl.Log.WithName("DpuDeviceHandler"),
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
