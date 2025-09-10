package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaypipes/ghw"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"github.com/openshift/dpu-operator/internal/images"
	"github.com/openshift/dpu-operator/internal/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kind/pkg/errors"
)

type IntelDetector struct {
	name string
}

func NewIntelDetector() *IntelDetector {
	return &IntelDetector{name: "Intel IPU E2100"}
}

func (d *IntelDetector) Name() string {
	return d.name
}

func (d *IntelDetector) isVirtualFunction(device string) (bool, error) {
	physfnPath := filepath.Join("/sys/bus/pci/devices", device, "physfn")

	if _, err := os.Stat(physfnPath); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, fmt.Errorf("Error when stating path %s: %v", device, err)
	}
}

func (d *IntelDetector) IsDPU(platform Platform, pci ghw.PCIDevice, dpuDevices []plugin.DpuIdentifier) (bool, error) {
	// VFs for the Intel IPU have the same PCIe info as the PF
	isVF, err := d.isVirtualFunction(pci.Address)
	if err != nil {
		return false, fmt.Errorf("Error determining if device %s is a VF or PF: %v", pci.Address, err)
	}

	// Check basic PCI device properties
	if isVF ||
		pci.Class.Name != "Network controller" ||
		pci.Vendor.Name != "Intel Corporation" ||
		pci.Product.Name != "Infrastructure Data Path Function" {
		return false, nil
	}

	netdevNames, err := platform.GetNetDevNameFromPCIeAddr(pci.Address)
	if err != nil {
		return false, fmt.Errorf("Error getting network device name for PCI address %s: %v", pci.Address, err)
	}

	for _, netdevName := range netdevNames {
		if strings.HasSuffix(netdevName, "d2") {
			return true, nil
		}
	}

	return false, nil
}

func (pi *IntelDetector) IsDpuPlatform(platform Platform) (bool, error) {
	product, err := platform.Product()
	if err != nil {
		return false, errors.Errorf("Error getting product info: %v", err)
	}

	if strings.Contains(product.Name, "IPU Adapter E2100-CCQDA2") {
		return true, nil
	}
	return false, nil
}

func (pi *IntelDetector) GetDpuIdentifier(platform Platform, pci *ghw.PCIDevice) (plugin.DpuIdentifier, error) {
	// TODO: rethink if it's possible to use something else than a pci address. Serial number doesn't seem to be the
	// right choice for IPU.
	identifier := fmt.Sprintf("intel-ipu-%s", SanitizePCIAddress(pci.Address))
	return plugin.DpuIdentifier(identifier), nil
}

func (pi *IntelDetector) VspPlugin(dpuMode bool, imageManager images.ImageManager, client client.Client, pm utils.PathManager, dpuIdentifier plugin.DpuIdentifier) (*plugin.GrpcPlugin, error) {
	return plugin.NewGrpcPlugin(dpuMode, dpuIdentifier, client, plugin.WithPathManager(pm))
}

func (d *IntelDetector) GetVendorName() string {
	return "intel"
}

func (d *IntelDetector) DpuPlatformIdentifier() plugin.DpuIdentifier {
	return "intel-ipu"
}
