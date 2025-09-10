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
	MrvlVendorID     string = "177d"
	MrvlDPUdeviceID  string = "a0f7"
	MrvlHostDeviceID string = "b900"
)

type MarvellDetector struct {
	name string
}

func NewMarvellDetector() *MarvellDetector {
	return &MarvellDetector{
		name: "Marvell DPU",
	}
}

func (d *MarvellDetector) Name() string {
	return d.name
}

func (pi *MarvellDetector) IsDPU(platform Platform, pci ghw.PCIDevice, dpuDevices []plugin.DpuIdentifier) (bool, error) {
	if pci.Vendor.ID == MrvlVendorID &&
		pci.Product.ID == MrvlHostDeviceID {
		return true, nil
	}

	return false, nil
}

// IsDpuPlatform checks if the platform is a Marvell DPU
func (pi *MarvellDetector) IsDpuPlatform(platform Platform) (bool, error) {
	devices, err := platform.PciDevices()
	if err != nil {
		return false, errors.Errorf("Error getting devices: %v", err)
	}

	for _, pci := range devices {
		if pci.Vendor.ID == MrvlVendorID &&
			pci.Product.ID == MrvlDPUdeviceID {
			return true, nil
		}
	}

	return false, nil
}

func (pi *MarvellDetector) GetDpuIdentifier(platform Platform, pci *ghw.PCIDevice) (plugin.DpuIdentifier, error) {
	identifier := fmt.Sprintf("marvell-dpu-%s", SanitizePCIAddress(pci.Address))
	return plugin.DpuIdentifier(identifier), nil
}

func (pi *MarvellDetector) VspPlugin(dpuMode bool, imageManager images.ImageManager, client client.Client, pm utils.PathManager, dpuIdentifier plugin.DpuIdentifier) (*plugin.GrpcPlugin, error) {
	template_vars := plugin.NewVspTemplateVars()
	vspImage, err := imageManager.GetImage(images.VspImageMarvell)
	if err != nil {
		return nil, errors.Errorf("Error getting Marvell VSP image: %v", err)
	}
	template_vars.VendorSpecificPluginImage = vspImage
	template_vars.Command = `[ "/vsp-mrvl" ]`
	return plugin.NewGrpcPlugin(dpuMode, dpuIdentifier, client, plugin.WithVsp(template_vars), plugin.WithPathManager(pm))
}

// GetVendorName returns the name of the vendor
func (d *MarvellDetector) GetVendorName() string {
	return "marvell"
}

func (d *MarvellDetector) DpuPlatformIdentifier() plugin.DpuIdentifier {
	return "marvell-dpu"
}
