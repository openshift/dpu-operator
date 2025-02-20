package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaypipes/ghw"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"github.com/openshift/dpu-operator/internal/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kind/pkg/errors"
)

type IntelDetector struct {
	name string
}

func NewIntelDetector() *IntelDetector {
	return &IntelDetector{name: "Intel IPU"}
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

func (d *IntelDetector) IsDPU(pci ghw.PCIDevice) (bool, error) {
	// VFs for the Intel IPU have the same PCIe info as the PF
	isVF, err := d.isVirtualFunction(pci.Address)
	if err != nil {
		return false, fmt.Errorf("Error determining if device %s is a VF or PF: %v", pci.Address, err)
	}

	return !isVF &&
		pci.Class.Name == "Network controller" &&
		pci.Vendor.Name == "Intel Corporation" &&
		pci.Product.Name == "Infrastructure Data Path Function", nil
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

func (pi *IntelDetector) VspPlugin(dpuMode bool, vspImages map[string]string, client client.Client, pm utils.PathManager) (*plugin.GrpcPlugin, error) {
	template_vars := plugin.NewVspTemplateVars()
	template_vars.VendorSpecificPluginImage = vspImages[plugin.VspImageIntel]
	template_vars.Command = `[ "/usr/bin/ipuplugin" ]`
	template_vars.Args = `[ "-v=debug", "--p4rtName=vsp-p4-service.default.svc.cluster.local" ]`
	return plugin.NewGrpcPlugin(dpuMode, client, plugin.WithVsp(template_vars), plugin.WithPathManager(pm))
}

func (d *IntelDetector) GetVendorName() string {
	return "intel"
}
