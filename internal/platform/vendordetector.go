package platform

import (
	"fmt"

	"github.com/jaypipes/ghw"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"github.com/openshift/dpu-operator/internal/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kind/pkg/errors"
)

type DpuDetectorManager struct {
	platform  Platform
	detectors []VendorDetector
}

type VendorDetector interface {
	Name() string
	VspPlugin(dpuMode bool, vspImages map[string]string, client client.Client, pm utils.PathManager) (*plugin.GrpcPlugin, error)
	IsDpuPlatform(platform Platform) (bool, error)
	IsDPU(pci ghw.PCIDevice) (bool, error)
}

func NewDpuDetectorManager(platform Platform) *DpuDetectorManager {
	return &DpuDetectorManager{
		platform: platform,
		detectors: []VendorDetector{
			NewIntelDetector(),
			NewMarvellDetector(),
			// add more detectors here
		},
	}
}

func (d *DpuDetectorManager) Detect(vspImages map[string]string, client client.Client, pm utils.PathManager) (bool, *plugin.GrpcPlugin, error) {
	for _, detector := range d.detectors {
		dpuPlatform, err := detector.IsDpuPlatform(d.platform)
		if err != nil {
			return false, nil, fmt.Errorf("Error detecting if running on DPU platform with detector %v: %v", detector.Name(), err)
		}

		if dpuPlatform {
			vsp, err := detector.VspPlugin(true, vspImages, client, pm)
			if err != nil {
				return true, nil, err
			}
			return true, vsp, nil
		}

		devices, err := d.platform.PciDevices()
		if err != nil {
			return false, nil, errors.Errorf("Error getting PCI info: %v", err)
		}

		for _, pci := range devices {
			isDpu, err := detector.IsDPU(*pci)
			if err != nil {
				return false, nil, errors.Errorf("Error detecting if device is DPU with detector %v: %v", detector.Name(), err)
			}
			if isDpu {
				vsp, err := detector.VspPlugin(false, vspImages, client, pm)
				if err != nil {
					return true, nil, err
				}
				return false, vsp, nil
			}
		}
	}
	return false, nil, nil
}
