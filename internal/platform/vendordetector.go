package platform

import (
	"fmt"

	"github.com/jaypipes/ghw"
	configv1 "github.com/openshift/dpu-operator/api/v1"
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
	IsDpuPlatform(platform Platform) (*configv1.DataProcessingUnit, error)
	IsDPU(pci ghw.PCIDevice) (*configv1.DataProcessingUnit, error)
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

func (d *DpuDetectorManager) Detect(vspImages map[string]string, client client.Client, pm utils.PathManager) (*configv1.DataProcessingUnit, *plugin.GrpcPlugin, error) {
	for _, detector := range d.detectors {
		dpu, err := detector.IsDpuPlatform(d.platform)
		if err != nil {
			return nil, nil, fmt.Errorf("Error detecting if running on DPU platform with detector %v: %v", detector.Name(), err)
		}

		if dpu != nil {
			vsp, err := detector.VspPlugin(true, vspImages, client, pm)
			if err != nil {
				return nil, nil, err
			}
			return dpu, vsp, nil
		}

		devices, err := d.platform.PciDevices()
		if err != nil {
			return nil, nil, errors.Errorf("Error getting PCI info: %v", err)
		}

		for _, pci := range devices {
			dpu, err := detector.IsDPU(*pci)
			if err != nil {
				return nil, nil, errors.Errorf("Error detecting if device is DPU with detector %v: %v", detector.Name(), err)
			}
			if dpu != nil {
				vsp, err := detector.VspPlugin(false, vspImages, client, pm)
				if err != nil {
					return nil, nil, err
				}
				return dpu, vsp, nil
			}
		}
	}
	return nil, nil, nil
}
