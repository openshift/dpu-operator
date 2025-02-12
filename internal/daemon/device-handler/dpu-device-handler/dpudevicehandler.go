package dpudevicehandler

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/sriovutils"
	dh "github.com/openshift/dpu-operator/internal/daemon/device-handler"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"github.com/openshift/dpu-operator/internal/utils"
	"google.golang.org/grpc"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// dpuDeviceHandler handles NF networking devices
type dpuDeviceHandler struct {
	log              logr.Logger
	conn             *grpc.ClientConn
	pathManager      utils.PathManager
	setupDevicesDone chan struct{}
	dpuMode          bool
	vsp              plugin.VendorPlugin
}

func NewDpuDeviceHandler(vsp plugin.VendorPlugin, opts ...func(*dpuDeviceHandler)) *dpuDeviceHandler {
	devHandler := &dpuDeviceHandler{
		log:     ctrl.Log.WithName("DpuDeviceHandler"),
		dpuMode: false,
		vsp:     vsp,
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

func (d *dpuDeviceHandler) GetDevices() (*dh.DeviceList, error) {
	// Wait for devices to be done initializing
	<-d.setupDevicesDone

	Devices, err := d.vsp.GetDevices()
	if err != nil {
		return nil, fmt.Errorf("failed to handle GetDevices request: %v", err)
	}

	devices := make(dh.DeviceList)

	// TODO: We need to properly enforce API boundaries at the VSP level. The host side requires pci-addresses when handling devices, however the dpu side requires a higher level of abstraction. For now, just enforce PCI addresses for device ID on the host only.
	for _, device := range Devices.Devices {
		if d.dpuMode {
			devices[device.ID] = pluginapi.Device{ID: device.ID, Health: pluginapi.Healthy}
			continue
		}

		devPciId, err := normalizeDeviceToPci(device.ID)
		if err != nil {
			return nil, fmt.Errorf("Failed to normalize device %s from GetDevice request: %v", device.ID, err)
		}
		devices[devPciId] = pluginapi.Device{ID: devPciId, Health: pluginapi.Healthy}
	}

	return &devices, nil
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

	numVfs, err := d.vsp.SetNumVfs(8)
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
