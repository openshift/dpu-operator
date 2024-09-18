package platform

import (
	"fmt"

	"github.com/jaypipes/ghw"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"k8s.io/klog/v2"
	"sigs.k8s.io/kind/pkg/errors"
)

type PlatformInfoProvider interface {
}

type VendorDetector interface {
	IsDpuPlatform() (bool, error)
	VspPlugin(dpuMode bool) *plugin.GrpcPlugin
	IsDPU(pci ghw.PCIDevice) (bool, error)
	GetVendorName() string
}

type PlatformInfo struct {
	Detectors []VendorDetector
}

func NewPlatformInfo() *PlatformInfo {
	return &PlatformInfo{
		Detectors: []VendorDetector{
			NewIntelDetector(),
			NewMarvellDetector(),
			// add more detectors here
		},
	}
}

func (pi *PlatformInfo) Getvendorname() (string, error) {
	klog.Infof("Detecting  Platform is DPU or not")
	for _, detector := range pi.Detectors {
		isDpu, err := detector.IsDpuPlatform()
		if err != nil {
			return "", err

		}

		if isDpu {
			klog.Infof("Platform is a %v's DPU detected", detector.GetVendorName())
			return detector.GetVendorName(), nil
		}
	}
	klog.Infof("Detecting Host has DPU or not")
	pci, err := ghw.PCI()
	if err != nil {
		return "", errors.Errorf("Error getting PCI info: %v", err)
	}

	for _, pci := range pci.ListDevices() {
		for _, detector := range pi.Detectors {
			isDPU, err := detector.IsDPU(*pci)
			if err != nil {
				return "", err

			}

			if isDPU {
				klog.Infof("Platform Host has %v's DPU detected", detector.GetVendorName())
				return detector.GetVendorName(), nil
			}
		}
	}

	return "", errors.Errorf("No vendor found")
}

func (pi *PlatformInfo) GetPcieDevFilter() (string, string, string, error) {
	PCI, err := ghw.PCI()
	if err != nil {
		return "", "", "", errors.Errorf("Error getting PCI info: %v", err)
	}

	for _, pci := range PCI.ListDevices() {
		for _, detector := range pi.Detectors {
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
		if len(dpuDevices) != 1 {
			return nil, fmt.Errorf("%v DPU devices detected. Currently only supporting exactly 1 DPU per node", len(dpuDevices))
		}

		return detectors[0].VspPlugin(dpuMode), nil
	}
}
