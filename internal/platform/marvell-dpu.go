package platform

import (
	"github.com/jaypipes/ghw"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kind/pkg/errors"
)

const (
	MrvlVendorID     string = "177d"
	MrvlDPUdeviceID  string = "a0f7"
	MrvlHostDeviceID string = "b900"
)

type MarvellDetector struct {
	Name string
}

func NewMarvellDetector() *MarvellDetector {
	return &MarvellDetector{
		Name: "Marvell DPU",
	}
}

// IsDPU checks if the PCI device Attached to the host is a Marvell DPU
// It returns true if device has Marvell DPU
func (pi *MarvellDetector) IsDPU(pci ghw.PCIDevice) (bool, error) {
	if pci.Vendor.ID == MrvlVendorID &&
		pci.Product.ID == MrvlHostDeviceID {
		return true, nil
	}

	return false, nil
}

// IsDpuPlatform checks if the platform is a Marvell DPU
func (pi *MarvellDetector) IsDpuPlatform() (bool, error) {
	pci, err := ghw.PCI()
	if err != nil {
		return false, errors.Errorf("Error getting product info: %v", err)
	}

	for _, pci := range pci.Devices {
		if pci.Vendor.ID == MrvlVendorID &&
			pci.Product.ID == MrvlDPUdeviceID {
			return true, nil
		}
	}

	return false, nil
}

func (pi *MarvellDetector) VspPlugin(dpuMode bool, vspImages map[string]string, client client.Client) (*plugin.GrpcPlugin, error) {
	template_vars := plugin.NewVspTemplateVars()
	template_vars.VendorSpecificPluginImage = vspImages[plugin.VspImageMarvell]
	template_vars.Command = `[ "/vsp-mrvl" ]`
	return plugin.NewGrpcPlugin(dpuMode, client, plugin.WithVsp(template_vars))
}

// GetVendorName returns the name of the vendor
func (d *MarvellDetector) GetVendorName() string {
	return "marvell"
}
