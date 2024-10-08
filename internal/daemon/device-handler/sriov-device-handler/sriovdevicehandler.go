package sriovdevicehandler

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-logr/logr"
	"github.com/jaypipes/ghw"
	devicehandler "github.com/openshift/dpu-operator/internal/daemon/device-handler"
	dp "github.com/openshift/dpu-operator/internal/daemon/device-plugin"
	"github.com/openshift/dpu-operator/internal/platform"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// sriovDeviceHandler handles NF networking devices
type sriovDeviceHandler struct {
	log              logr.Logger
	vfFilterFunc     FilterFunc
	setupDevicesDone chan struct{}
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

func (s *sriovDeviceHandler) SetSriovNumVfs(pciAddr string, numVfs int) error {
	s.log.Info("SetSriovNumVfs(): set sriov_numvfs", "device", pciAddr, "numVfs", numVfs)

	numVfsFilePath := filepath.Join("/sys/bus/pci/devices", pciAddr, "sriov_numvfs")

	bs := []byte(strconv.Itoa(numVfs))

	// We must always write 0 to sriov_numvfs first before changing the number of VFs.
	err := os.WriteFile(numVfsFilePath, []byte("0"), os.ModeAppend)
	if err != nil {
		return fmt.Errorf("SetSriovNumVfs(): fail to reset %s: %v", numVfsFilePath, err)
	}

	if numVfs != 0 {
		err = os.WriteFile(numVfsFilePath, bs, os.ModeAppend)
		if err != nil {
			return fmt.Errorf("SetSriovNumVfs(): fail to set %s: %v", numVfsFilePath, err)
		}
		// NOTE: VF netdevs are not guaranteed to appear immediately after writing the file
	}

	return nil
}

func (s *sriovDeviceHandler) GetPcieDevices() ([]*ghw.PCIDevice, error) {
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

func (s *sriovDeviceHandler) GetDevices() (*dp.DeviceList, error) {
	devices := make(dp.DeviceList)

	// Wait for devices to be done initializing
	<-s.setupDevicesDone

	pciDevices, err := s.GetPcieDevices()
	if err != nil {
		return nil, err
	}
	for _, pciDevice := range pciDevices {
		matchedDevice, err := s.vfFilterFunc(pciDevice)
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

// ensureConnected makes sure we are connected to the VSP's gRPC
func (s *sriovDeviceHandler) ensureConnected() error {
	// TODO: FIXME, design proto API for VSP
	return nil
}

// SetupDevices
func (s *sriovDeviceHandler) SetupDevices() error {
	s.setupDevicesDone = make(chan struct{})

	err := s.ensureConnected()
	if err != nil {
		return fmt.Errorf("failed to ensure connection to vsp: %v", err)
	}

	// TODO: The VSP should pass the hardcoded parameters to create a filter
	pi := platform.NewPlatformInfo()
	vendorName, _ := pi.Getvendorname()
	var pfAddr string
	if vendorName == "marvell" {
		vendorID, deviceID, pfaddr, _ := pi.GetPcieDevFilter()
		s.vfFilterFunc = CreatePcieDevFilter(vendorID, deviceID, "octeon_ep")
		pfAddr = pfaddr
	} else if vendorName == "intel" {
		vendorID, deviceID, pfaddr, _ := pi.GetPcieDevFilter()
		s.vfFilterFunc = CreatePcieDevFilter(vendorID, deviceID, "idpf")
		pfAddr = pfaddr
	} else {
		return fmt.Errorf("Invalid vendor name detected: %s", vendorName)
	}

	// TODO: The VSP should pass in the PF
	err = s.SetSriovNumVfs(pfAddr, 8)
	if err != nil {
		return fmt.Errorf("failed to set sriov numVfs: %v", err)
	}

	close(s.setupDevicesDone)

	return nil
}

func NewSriovDeviceHandler() *sriovDeviceHandler {
	devHandler := &sriovDeviceHandler{
		log: ctrl.Log.WithName("SriovDeviceHandler"),
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
