package platform

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jaypipes/ghw"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"sigs.k8s.io/kind/pkg/errors"
)

type PlatformInfoProvider interface {
}

type VendorDetector interface {
	IsDpuPlatform() (bool, error)
	VspPlugin(dpuMode bool) *plugin.GrpcPlugin
	IsDPU(pci ghw.PCIDevice) bool
}

type PlatformInfo struct {
	Detectors []VendorDetector
}

func NewPlatformInfo() *PlatformInfo {
	return &PlatformInfo{
		Detectors: []VendorDetector{
			NewIntelDetector(),
			// add more detectors here
		},
	}
}

func (pi *PlatformInfo) IsDpu() (bool, error) {
	for _, detector := range pi.Detectors {
		isDpu, err := detector.IsDpuPlatform()
		if err != nil {
			return false, err
		}
		if isDpu {
			return true, nil
		}
	}
	return false, nil
}

func (pi *PlatformInfo) listDpuDevices() ([]ghw.PCIDevice, []VendorDetector, error) {
	pci, err := ghw.PCI()
	if err != nil {
		return nil, nil, errors.Errorf("Error getting PCI info: %v", err)
	}

	var dpuDevices []ghw.PCIDevice
	var activeDetectors []VendorDetector
	for _, pci := range pci.ListDevices() {
		for _, detector := range pi.Detectors {
			if detector.IsDPU(*pci) {
				dpuDevices = append(dpuDevices, *pci)
				activeDetectors = append(activeDetectors, detector)
				break
			}
		}
	}
	return dpuDevices, activeDetectors, nil
}

func isVirtualFunction(device string) (bool, error) {
	physfnPath := filepath.Join("/sys/bus/pci/devices", device, "physfn")

	if _, err := os.Stat(physfnPath); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, fmt.Errorf("Error when stating path %s: %v", device, err)
	}
}

func (pi *PlatformInfo) VspPlugin(dpuMode bool) (*plugin.GrpcPlugin, error) {
	if dpuMode {
		return plugin.NewGrpcPlugin(dpuMode), nil
	} else {
		dpuDevices, detectors, err := pi.listDpuDevices()
		if err != nil {
			return nil, errors.Errorf("Failed to get VspPlugin from platform: %v", err)
		}
		if len(dpuDevices) == 0 {
			return nil, fmt.Errorf("Failed to detect any DPU devices")
		}

		var pfDevices []*ghw.PCIDevice
		var pfDetectors []VendorDetector

		for i, device := range dpuDevices {
			isVF, err := isVirtualFunction(device.Address)
			if err != nil {
				return nil, fmt.Errorf("Error determining if device %s is a VF or PF: %v", device.Address, err)
			}
			if !isVF {
				pfDevices = append(pfDevices, &device)
				pfDetectors = append(pfDetectors, detectors[i])
			}
		}

		if len(pfDevices) != 1 {
			return nil, fmt.Errorf("%v DPU devices detected. Currently only supporting exactly 1 DPU per node", len(pfDevices))
		}

		return pfDetectors[0].VspPlugin(dpuMode), nil
	}
}
