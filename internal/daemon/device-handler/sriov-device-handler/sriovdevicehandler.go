package sriovdevicehandler

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/jaypipes/ghw"
	devicehandler "github.com/openshift/dpu-operator/internal/daemon/device-handler"
	dp "github.com/openshift/dpu-operator/internal/daemon/device-plugin"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// sriovDeviceHandler handles NF networking devices
type sriovDeviceHandler struct {
	log        logr.Logger
	filterFunc FilterFunc
}

type FilterFunc func(*ghw.PCIDevice) (bool, error)

func CreatePcieDevFilter(vendorID, deviceID, driverName string) FilterFunc {
	return func(device *ghw.PCIDevice) (bool, error) {
		if device.Vendor.ID == vendorID && device.Product.ID == deviceID {
			name, err := devicehandler.GetDriverName(device.Address)
			return name == driverName, err
		}
		return false, nil
	}
}

func (nf *sriovDeviceHandler) GetPcieDevices() ([]*ghw.PCIDevice, error) {
	pci, err := ghw.PCI()
	if err != nil {
		return nil, fmt.Errorf("error getting PCI info: %v", err)
	}

	pciDevices := pci.ListDevices()
	if len(pciDevices) == 0 {
		return nil, fmt.Errorf("no PCI network device found")
	}

	return pciDevices, nil
}

func (nf *sriovDeviceHandler) GetDevices() (*dp.DeviceList, error) {
	devices := make(dp.DeviceList)
	pciDevices, err := nf.GetPcieDevices()
	if err != nil {
		return nil, err
	}
	for _, pciDevice := range pciDevices {
		matchedDevice, err := nf.filterFunc(pciDevice)
		if err != nil {
			return nil, err
		}
		if matchedDevice {
			var topology *pluginapi.TopologyInfo
			numaNode := devicehandler.GetNumaNode(pciDevice.Address)
			if numaNode >= 0 {
				topology = &pluginapi.TopologyInfo{
					Nodes: []*pluginapi.NUMANode{
						{ID: int64(numaNode)},
					},
				}
			}
			devices[pciDevice.Address] = pluginapi.Device{
				ID:       pciDevice.Address,
				Health:   pluginapi.Healthy,
				Topology: topology,
			}
		}
	}

	return &devices, nil
}

func NewSriovDeviceHandler() *sriovDeviceHandler {
	// TODO: The VSP should pass the following parameters to create a filter
	filterFunc := CreatePcieDevFilter("8086", "145c", "idpf")
	return &sriovDeviceHandler{
		log:        ctrl.Log.WithName("SriovDeviceHandler"),
		filterFunc: filterFunc,
	}
}
