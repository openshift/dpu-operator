package platform

import (
	"github.com/jaypipes/ghw"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"github.com/openshift/dpu-operator/internal/images"
	"github.com/openshift/dpu-operator/internal/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kind/pkg/errors"
)

const (
	IntelVendorID                        string = "8086"
	IntelNetSecBackplaneDeviceID         string = "124c"
	IntelNetSecSFPDeviceID               string = "124d"
	IntelNetSecHostDeviceID              string = "1599"
	IntelNetSecHostVfDeviceID            string = "1889" // Intel Corporation Ethernet Adaptive Virtual Function
	IntelNetSecDpuSFPf0PCIeAddress       string = "0000:f4:00.0"
	IntelNetSecDpuSFPf1PCIeAddress       string = "0000:f4:00.1"
	IntelNetSecDpuBackplanef2PCIeAddress string = "0000:f4:00.2"
	IntelNetSecDpuBackplanef3PCIeAddress string = "0000:f4:00.3"
)

type NetsecAcceleratorDetector struct {
	name string
}

func NewNetsecAcceleratorDetector() *NetsecAcceleratorDetector {
	return &NetsecAcceleratorDetector{name: "Intel Netsec Accelerator"}
}

func (d *NetsecAcceleratorDetector) Name() string {
	return d.name
}

func (pi *NetsecAcceleratorDetector) IsDPU(platform Platform, pci ghw.PCIDevice, dpuDevices []plugin.DpuIdentifier) (bool, error) {
	if pci.Vendor.ID == IntelVendorID &&
		pci.Product.ID == IntelNetSecHostDeviceID {
		serial, err := platform.ReadDeviceSerialNumber(&pci)
		if err != nil {
			// Intel NetSec Network Devices should return a serial number.
			return false, errors.Errorf("Error reading device serial number for %s: %v", pci.Address, err)
		}
		for _, dpuDevice := range dpuDevices {
			if plugin.DpuIdentifier(serial) == dpuDevice {
				// This is a dual port device ignore the second port.
				return false, nil
			}
		}
		return true, nil
	}

	return false, nil
}

func (pi *NetsecAcceleratorDetector) IsDpuPlatform(platform Platform) (bool, error) {
	devices, err := platform.PciDevices()
	if err != nil {
		return false, errors.Errorf("Error getting devices: %v", err)
	}

	for _, pci := range devices {
		if pci.Vendor.ID == IntelVendorID &&
			pci.Product.ID == IntelNetSecBackplaneDeviceID {
			return true, nil
		}
	}

	return false, nil
}

func (pi *NetsecAcceleratorDetector) GetDpuIdentifier(platform Platform, pci *ghw.PCIDevice) (plugin.DpuIdentifier, error) {
	serial, err := platform.ReadDeviceSerialNumber(pci)
	return plugin.DpuIdentifier(SanitizePCIAddress(serial)), err
}

func (pi *NetsecAcceleratorDetector) VspPlugin(dpuMode bool, imageManager images.ImageManager, client client.Client, pm utils.PathManager, dpuIdentifier plugin.DpuIdentifier) (*plugin.GrpcPlugin, error) {
	return plugin.NewGrpcPlugin(dpuMode, dpuIdentifier, client, plugin.WithPathManager(pm))
}

// GetVendorName returns the name of the vendor
func (d *NetsecAcceleratorDetector) GetVendorName() string {
	return "intel"
}

func (d *NetsecAcceleratorDetector) DpuPlatformName() string {
	return "intel-netsec"
}

func (d *NetsecAcceleratorDetector) DpuPlatformIdentifier(platform Platform) (plugin.DpuIdentifier, error) {
	// Get the network device MAC Address from the SFP port 0's PCIe address
	macAddress, err := platform.GetNetDevMACAddressFromPCIeAddr(IntelNetSecDpuSFPf0PCIeAddress)
	if err != nil {
		return "", errors.Errorf("Error getting network device MAC address for %s: %v", IntelNetSecDpuSFPf0PCIeAddress, err)
	}

	return plugin.DpuIdentifier(SanitizePCIAddress(macAddress)), nil
}
