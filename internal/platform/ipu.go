package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jaypipes/ghw"
	configv1 "github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"github.com/openshift/dpu-operator/internal/utils"
	"github.com/openshift/dpu-operator/pkgs/vars"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kind/pkg/errors"
)

// The below is present in config/dev/local-images-template.yaml
const VspP4ImageIntelEnv string = "IntelVspP4Image"
const VspP4ServiceName string = "vsp-p4-service"

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

func normalizePciAddress(pciAddress string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	normalized := re.ReplaceAllString(pciAddress, "-")
	normalized = strings.ToLower(normalized)
	return normalized
}

func (d *IntelDetector) IsDPU(pci ghw.PCIDevice) (*configv1.DataProcessingUnit, error) {
	// VFs for the Intel IPU have the same PCIe info as the PF
	isVF, err := d.isVirtualFunction(pci.Address)
	if err != nil {
		return nil, fmt.Errorf("Error determining if device %s is a VF or PF: %v", pci.Address, err)
	}

	isDpuPciDevice := !isVF && pci.Class.Name == "Network controller" && pci.Vendor.Name == "Intel Corporation" && pci.Product.Name == "Infrastructure Data Path Function"

	if !isDpuPciDevice {
		return nil, nil
	}

	ret := configv1.DataProcessingUnit{}
	ret.SetName("e2100-" + normalizePciAddress(pci.Address))
	ret.Spec.DpuType = "IPU Adapter E2100-CCQDA2"
	ret.Spec.IsDpuSide = false

	return &ret, nil
}

func (pi *IntelDetector) IsDpuPlatform(platform Platform) (*configv1.DataProcessingUnit, error) {
	product, err := platform.Product()
	if err != nil {
		return nil, errors.Errorf("Error getting product info: %v", err)
	}

	if strings.Contains(product.Name, "IPU Adapter E2100-CCQDA2") {
		ret := configv1.DataProcessingUnit{}
		ret.SetName("e2100")
		ret.Spec.DpuType = "IPU Adapter E2100-CCQDA2"
		ret.Spec.IsDpuSide = true
		return &ret, nil
	}
	return nil, nil
}

func (pi *IntelDetector) VspPlugin(dpuMode bool, vspImages map[string]string, client client.Client, pm utils.PathManager) (*plugin.GrpcPlugin, error) {
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
	return plugin.NewGrpcPlugin(dpuMode, client, plugin.WithVsp(template_vars), plugin.WithPathManager(pm))
}

func (d *IntelDetector) GetVendorName() string {
	return "intel"
}
