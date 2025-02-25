package platform

import (
	"github.com/jaypipes/ghw"
	configv1 "github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
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

func (d *MarvellDetector) Detect(platform Platform, vspImages map[string]string, client client.Client) (*configv1.DataProcessingUnit, *plugin.GrpcPlugin, error) {
	return nil, nil, errors.New("Not implemented")
}

// IsDPU checks if the PCI device Attached to the host is a Marvell DPU
// It returns true if device has Marvell DPU
func (pi *MarvellDetector) IsDPU(pci ghw.PCIDevice) (*configv1.DataProcessingUnit, error) {
	if pci.Vendor.ID == MrvlVendorID &&
		pci.Product.ID == MrvlHostDeviceID {

		ret := configv1.DataProcessingUnit{}
		ret.SetName("octeon10-" + normalizePciAddress(pci.Address))
		ret.Spec.DpuType = "Marvell Octeon 10"
		ret.Spec.IsDpuSide = false

		return &ret, nil
	}

	return nil, nil
}

// IsDpuPlatform checks if the platform is a Marvell DPU
func (pi *MarvellDetector) IsDpuPlatform(platform Platform) (*configv1.DataProcessingUnit, error) {
	pci, err := ghw.PCI()
	if err != nil {
		return nil, errors.Errorf("Error getting product info: %v", err)
	}

	for _, pci := range pci.Devices {
		if pci.Vendor.ID == MrvlVendorID &&
			pci.Product.ID == MrvlDPUdeviceID {

			ret := configv1.DataProcessingUnit{}
			ret.SetName("octeon10-" + normalizePciAddress(pci.Address))
			ret.Spec.DpuType = "Marvell Octeon 10"
			ret.Spec.IsDpuSide = true
			return &ret, nil
		}
	}

	return nil, nil
}

func (pi *MarvellDetector) VspPlugin(dpuMode bool, vspImages map[string]string, client client.Client, pm utils.PathManager) (*plugin.GrpcPlugin, error) {
	template_vars := plugin.NewVspTemplateVars()
	template_vars.VendorSpecificPluginImage = vspImages[plugin.VspImageMarvell]
	template_vars.Command = `[ "/vsp-mrvl" ]`
	return plugin.NewGrpcPlugin(dpuMode, client, plugin.WithVsp(template_vars), plugin.WithPathManager(pm))
}

// GetVendorName returns the name of the vendor
func (d *MarvellDetector) GetVendorName() string {
	return "marvell"
}
