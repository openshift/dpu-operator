package platform

import (
	"fmt"

	"github.com/jaypipes/ghw"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"github.com/openshift/dpu-operator/internal/images"
	"github.com/openshift/dpu-operator/internal/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kind/pkg/errors"
)

const (
	// SynaXG DPU uses Marvell OCTEON Fusion CNF105xx (CNF10KA) silicon,
	// so the PCIe Vendor ID is Marvell's (0x177d).
	// These must match the constants in internal/daemon/vendor-specific-plugins/synaxg/main.go.
	SgVendorID     string = "177d" // Marvell Semiconductor (SynaXG uses Marvell CNF105xx chip)
	SgDPUdeviceID  string = "a0f7" // CNF10KA DPU-side device ID (seen from ARM/card side)
	SgHostDeviceID string = "ba00" // CNF10KA host-side PF device ID (octeon_ep driver)
)

type SynaXGDetector struct {
	name string
}

func NewSynaXGDetector() *SynaXGDetector {
	return &SynaXGDetector{
		name: "SynaXG DPU",
	}
}

func (d *SynaXGDetector) Name() string {
	return d.name
}

func (pi *SynaXGDetector) IsDPU(platform Platform, pci ghw.PCIDevice, dpuDevices []plugin.DpuIdentifier) (bool, error) {
	if pci.Vendor.ID == SgVendorID &&
		pci.Product.ID == SgHostDeviceID {
		// Dedup: if a DPU with the same PCIe address was already detected
		// (e.g. multi-port card), skip it.
		identifier, err := pi.GetDpuIdentifier(platform, &pci)
		if err != nil {
			return false, err
		}
		for _, existing := range dpuDevices {
			if existing == identifier {
				return false, nil
			}
		}
		return true, nil
	}

	return false, nil
}

// IsDpuPlatform checks if the platform is a SynaXG DPU
func (pi *SynaXGDetector) IsDpuPlatform(platform Platform) (bool, error) {
	devices, err := platform.PciDevices()
	if err != nil {
		return false, errors.Errorf("Error getting devices: %v", err)
	}

	for _, pci := range devices {
		if pci.Vendor.ID == SgVendorID &&
			pci.Product.ID == SgDPUdeviceID {
			return true, nil
		}
	}

	return false, nil
}

func (pi *SynaXGDetector) GetDpuIdentifier(platform Platform, pci *ghw.PCIDevice) (plugin.DpuIdentifier, error) {
	identifier := fmt.Sprintf("SynaXG-dpu-%s", SanitizePCIAddress(pci.Address))
	return plugin.DpuIdentifier(identifier), nil
}

func (pi *SynaXGDetector) VspPlugin(dpuMode bool, imageManager images.ImageManager, client client.Client, pm utils.PathManager, dpuIdentifier plugin.DpuIdentifier) (*plugin.GrpcPlugin, error) {
	return plugin.NewGrpcPlugin(dpuMode, dpuIdentifier, client, plugin.WithPathManager(pm))
}

// GetVendorName returns the name of the vendor
func (d *SynaXGDetector) GetVendorName() string {
	return "SynaXG"
}

func (d *SynaXGDetector) DpuPlatformName() string {
	return "synaxg"
}

// FIXME: Must be a unique value on the DPU that is non changing.
func (d *SynaXGDetector) DpuPlatformIdentifier(platform Platform) (plugin.DpuIdentifier, error) {
	return plugin.DpuIdentifier("SynaXG-dpu"), nil
}
