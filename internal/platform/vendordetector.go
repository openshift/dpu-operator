package platform

import (
	stderrors "errors"
	"fmt"

	"github.com/jaypipes/ghw"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kind/pkg/errors"
)

type PlatformInfoProvider interface {
}

type VendorDetector interface {
	IsDpuPlatform(platform Platform) (bool, error)
	VspPlugin(dpuMode bool, vspImages map[string]string, client client.Client) (*plugin.GrpcPlugin, error)
	IsDPU(pci ghw.PCIDevice) (bool, error)
	GetVendorName() string
}

type PlatformInfo struct {
	p         Platform
	Detectors []VendorDetector
}

func NewPlatformInfo(p Platform) *PlatformInfo {
	return &PlatformInfo{
		p: p,
		Detectors: []VendorDetector{
			NewIntelDetector(),
			NewMarvellDetector(),
			// add more detectors here
		},
	}
}

func (pi *PlatformInfo) NewVspPlugin(dpuMode bool, vspImages map[string]string, client client.Client) (*plugin.GrpcPlugin, error) {
	var detector VendorDetector
	var err error

	if dpuMode {
		detector, err = pi.detectDpuPlatform(true)
	} else {
		detector, err = pi.detectDpuSystem(true)
	}
	if err != nil {
		return nil, err
	}
	vspPlugin, err := detector.VspPlugin(dpuMode, vspImages, client)
	if err != nil {
		return nil, errors.Errorf("Error encountered when deploying VspPlugin: %v", err)
	}

	return vspPlugin, nil
}

func (pi *PlatformInfo) Getvendorname() (string, error) {
	klog.Infof("Detecting  Platform is DPU or not")
	for _, detector := range pi.Detectors {
		isDpu, err := detector.IsDpuPlatform(pi.p)
		if err != nil {
			return "", err

		}

		if isDpu {
			klog.Infof("Platform is a %v's DPU detected", detector.GetVendorName())
			return detector.GetVendorName(), nil
		}
	}
	klog.Infof("Detecting Host has DPU or not")
	devices, err := pi.p.PciDevices()
	if err != nil {
		return "", errors.Errorf("Failed to get PCI devices: %v", err)
	}

	for _, pci := range devices {
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
	devices, err := pi.p.PciDevices()
	if err != nil {
		return "", "", "", errors.Errorf("Failed to get PCI devices: %v", err)
	}
	for _, pci := range devices {
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
	detector, err := pi.detectDpuPlatform(false)
	return detector != nil, err
}

func (pi *PlatformInfo) detectDpuPlatform(required bool) (VendorDetector, error) {
	var activeDetectors []VendorDetector
	var errResult error

	for _, detector := range pi.Detectors {
		isDPU, err := detector.IsDpuPlatform(pi.p)
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

func (pi *PlatformInfo) listDpuDevices() ([]ghw.PCIDevice, []VendorDetector, error) {
	devices, err := pi.p.PciDevices()
	if err != nil {
		return nil, nil, errors.Errorf("Failed to get PCI devices: %v", err)
	}

	var dpuDevices []ghw.PCIDevice
	var activeDetectors []VendorDetector
	for _, pci := range devices {
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

func (pi *PlatformInfo) detectDpuSystem(required bool) (VendorDetector, error) {
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
