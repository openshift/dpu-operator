package platform

import (
	"github.com/jaypipes/ghw"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"sigs.k8s.io/kind/pkg/errors"
	"strings"
)

type MarvellDetector struct {
	Name string
}

func NewMarvellDetector() *MarvellDetector {
	return &MarvellDetector{Name: "Marvell DPU"}
}

func (d *MarvellDetector) IsDPU(pci ghw.PCIDevice) (bool, error) {
	if strings.Contains(pci.Vendor.Name, "Cavium, Inc.") &&
		pci.Product.ID == "b900" {
		return true, nil
	}

	return false, nil
}

func (pi *MarvellDetector) IsDpuPlatform() (bool, error) {
	pci, err := ghw.PCI()
	if err != nil {
		return false, errors.Errorf("Error getting product info: %v", err)
	}

	for _, pci := range pci.ListDevices() {
		if pci.Vendor.ID == "177d" &&
			pci.Product.ID == "a0f7" {
			return true, nil
		}
	}

	return false, nil
}

func (pi *MarvellDetector) VspPlugin(dpuMode bool) *plugin.GrpcPlugin {
	return plugin.NewGrpcPlugin(dpuMode)
}
