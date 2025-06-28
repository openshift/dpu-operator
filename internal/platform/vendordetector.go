package platform

import (
	stderrors "errors"
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

func (pi *DpuDetectorManager) GetPcieDevFilter() (string, string, string, error) {
	devices, err := pi.platform.PciDevices()
	if err != nil {
		return "", "", "", errors.Errorf("Failed to get PCI devices: %v", err)
	}
	for _, pci := range devices {
		for _, detector := range pi.detectors {
			isDPU, err := detector.IsDPU(*pci)
			if err != nil {
				return "", "", "", err

			}

			if isDPU {
				return pci.Vendor.ID, pci.Product.ID, pci.Address, nil
			}
		}
	}

	return "", "", "", errors.Errorf("No vendor found")
}

func (pi *DpuDetectorManager) IsDpu() (bool, error) {
	detector, err := pi.detectDpuPlatform(false)
	return detector != nil, err
}

func (pi *DpuDetectorManager) detectDpuPlatform(required bool) (VendorDetector, error) {
	var activeDetectors []VendorDetector
	var errResult error

	for _, detector := range pi.detectors {
		isDPU, err := detector.IsDpuPlatform(pi.platform)
		if err != nil {
			errResult = stderrors.Join(errResult, err)
			continue
		}
		if isDPU {
			activeDetectors = append(activeDetectors, detector)
		}
	}
	if errResult != nil {
		return nil, errors.Errorf("Failed to detect DPU platform: %v", errResult)
	}
	if len(activeDetectors) != 1 {
		if len(activeDetectors) != 0 {
			return nil, errors.Errorf("Failed to detect DPU platform unambiguously: %v", activeDetectors)
		}
		if required {
			return nil, errors.Errorf("Failed to detect any DPU platform")
		}
		return nil, nil
	}
	return activeDetectors[0], nil
}

func (pi *DpuDetectorManager) listDpuDevices() ([]ghw.PCIDevice, []VendorDetector, error) {
	devices, err := pi.platform.PciDevices()
	if err != nil {
		return nil, nil, errors.Errorf("Failed to get PCI devices: %v", err)
	}

	var dpuDevices []ghw.PCIDevice
	var activeDetectors []VendorDetector
	for _, pci := range devices {
		for _, detector := range pi.detectors {
			isDPU, err := detector.IsDPU(*pci)
			if err != nil {
				return nil, nil, err
			}
			if isDPU {
				dpuDevices = append(dpuDevices, *pci)
				activeDetectors = append(activeDetectors, detector)
				break
			}
		}
	}
	return dpuDevices, activeDetectors, nil
}

func (pi *DpuDetectorManager) detectDpuSystem(required bool) (VendorDetector, error) {
	dpuDevices, detectors, err := pi.listDpuDevices()
	if err != nil {
		return nil, errors.Errorf("Failed to get VspPlugin from platform: %v", err)
	}
	if len(dpuDevices) != 1 {
		if len(dpuDevices) != 0 {
			return nil, fmt.Errorf("%v DPU devices detected. Currently only supporting exactly 1 DPU per node", len(dpuDevices))
		}
		if required {
			return nil, fmt.Errorf("Failed to detect any DPU devices")
		}
		return nil, nil
	}
	return detectors[0], nil
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
