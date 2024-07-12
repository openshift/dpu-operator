package platform

import (
	"strings"

	"github.com/jaypipes/ghw"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"sigs.k8s.io/kind/pkg/errors"
)

type IntelDetector struct {
	Name string
}

func NewIntelDetector() *IntelDetector {
	return &IntelDetector{Name: "Intel IPU"}
}

func (d *IntelDetector) IsDPU(pci ghw.PCIDevice) bool {
	return pci.Class.Name == "Ethernet controller" &&
		pci.Vendor.Name == "Intel Corporation" &&
		pci.Product.Name == "Infrastructure Data Path Function"
}

func (pi *IntelDetector) IsDpuPlatform() (bool, error) {
	product, err := ghw.Product()
	if err != nil {
		return false, errors.Errorf("Error getting product info: %v", err)
	}

	if strings.Contains(product.Name, "IPU Adapter E2100-CCQDA2") {
		return true, nil
	}
	return false, nil
}

func (pi *IntelDetector) VspPlugin(dpuMode bool) *plugin.GrpcPlugin {
	return plugin.NewGrpcPlugin(dpuMode)
}
