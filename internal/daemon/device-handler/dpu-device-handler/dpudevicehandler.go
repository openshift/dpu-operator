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

	return devHandler
}

func validatePciDevice(device string) (string, error) {
	if sriovutils.IsValidPCIAddress(device) {
		return device, nil
	}

	return device, fmt.Errorf("netdev %s is not a valid PCI device", device)
}

func (d *dpuDeviceHandler) GetDevices() (*dh.DeviceList, error) {
	// Wait for devices to be done initializing
	<-d.setupDevicesDone

	Devices, err := d.vsp.GetDevices()
	if err != nil {
		return nil, fmt.Errorf("failed to handle GetDevices request: %v", err)
	}

	devices := make(dh.DeviceList)

	// In terms of the API boundaries between components, the host side requires pci-addresses
	// when handling devices, however the dpu side requires a higher level of abstraction. For
	// now, we will just enforce PCI addresses as the device ID on the host only.
	for _, device := range Devices.Devices {
		if d.dpuMode {
			devices[device.ID] = pluginapi.Device{ID: device.ID, Health: pluginapi.Healthy}
			continue
		}

		devPciId, err := validatePciDevice(device.ID)
		if err != nil {
			return nil, fmt.Errorf("Error in deviceHandler: device %s from GetDevice request: %v", device.ID, err)
		}
		devices[devPciId] = pluginapi.Device{ID: devPciId, Health: pluginapi.Healthy}
	}

	return &devices, nil
}

// TODO: When changing the SRIOV numVfs, we should do the following:
// 1) Drain all pods running on the node with a drain controller running
// on the control plane. The nodes will be marked for draining and read by
// the drain controller.
// 2) Clearly define which PF and how many VFs from an API. User facing or
// otherwise
func (d *dpuDeviceHandler) SetupDevices() error {
	d.setupDevicesDone = make(chan struct{})

	defer close(d.setupDevicesDone)

	numVfs, err := d.vsp.SetNumVfs(8)
	if err != nil {
		// Currently NF devices do not require any setup outside the VSP
		// ignore the error if we are in DPU mode.
		if d.dpuMode {
			d.log.Info("Failed to set sriov numVFs, but ignoring error in DPU mode", "error", err)
		} else {
			return fmt.Errorf("failed to set sriov numVfs: %v", err)
		}
	} else {
		if numVfs.VfCnt == 0 {
			return fmt.Errorf("SetNumVfs ran, but numVfs == %d", numVfs.VfCnt)
		}
		d.log.Info("Num VFs set by VSP", "vf_count", numVfs.VfCnt)
	}

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
