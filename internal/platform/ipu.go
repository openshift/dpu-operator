package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaypipes/ghw"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"github.com/openshift/dpu-operator/pkgs/vars"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kind/pkg/errors"
)

// The below is present in config/dev/local-images-template.yaml
const VspP4ImageIntelEnv string = "IntelVspP4Image"
const VspP4ServiceName string = "vsp-p4-service"

type IntelDetector struct {
	Name string
}

func NewIntelDetector() *IntelDetector {
	return &IntelDetector{Name: "Intel IPU"}
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

func (pi *IntelDetector) VspPlugin(dpuMode bool, vspImages map[string]string, client client.Client) (*plugin.GrpcPlugin, error) {
	p4Image := os.Getenv(VspP4ImageIntelEnv)
	if p4Image == "" {
		return nil, errors.Errorf("Error getting vsp-p4 image: Can't start Intel vsp without vsp-p4")
	}
	args := fmt.Sprintf(`[ "-v=debug", "--p4rtName=%s.%s.svc.cluster.local", "--p4Image=%s" ]`,
		VspP4ServiceName, vars.Namespace, p4Image)
	template_vars := plugin.NewVspTemplateVars()
	template_vars.VendorSpecificPluginImage = vspImages[plugin.VspImageIntel]
	template_vars.Command = `[ "/usr/bin/ipuplugin" ]`
	template_vars.Args = args
	return plugin.NewGrpcPlugin(dpuMode, client, plugin.WithVsp(template_vars))
}

func (d *IntelDetector) GetVendorName() string {
	return "intel"
}
