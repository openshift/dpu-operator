package platform

import (
	stderrors "errors"
	"fmt"

	"github.com/jaypipes/ghw"
	"github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"github.com/openshift/dpu-operator/internal/images"
	"github.com/openshift/dpu-operator/internal/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kind/pkg/errors"
)

type DpuDetectorManager struct {
	platform  Platform
	detectors []VendorDetector
}

type VendorDetector interface {
	Name() string

	// Returns true if the platform is a DPU, otherwise false.
	// platform - The platform of the host system (host being the DPU).
	IsDpuPlatform(platform Platform) (bool, error)

	// Returns a VSP plugin for the detected DPU platform.
	// dpuMode - If true, the plugin is created for DPU mode, otherwise for host mode.
	// imageManager - The image manager to retrieve VSP images.
	// client - The Kubernetes client used to deploy the VSP.
	// dpuPciDevice - The PCI device of the DPU, if available. This is used to identify the DPU device for the plugin.
	VspPlugin(dpuMode bool, imageManager images.ImageManager, client client.Client, pm utils.PathManager, dpuIdentifier plugin.DpuIdentifier) (*plugin.GrpcPlugin, error)

	// Returns true if the device is a DPU detected by the detector, otherwise false.
	// platform - The platform of the host system (host with DPU).
	// pci - This argument is the PCI device to check if it matches what the detector is looking for.
	// dpuDevices (optional) - Is a list of already detected DPU devices used for excluding multi-port devices to be counted more than once.
	IsDPU(platform Platform, pci ghw.PCIDevice, dpuDevices []plugin.DpuIdentifier) (bool, error)

	// Returns a unique identifier for the DPU device.
	// platform - The platform of the host system (host with DPU).
	// pci - The PCI device of the DPU's network interface.
	GetDpuIdentifier(platform Platform, pci *ghw.PCIDevice) (plugin.DpuIdentifier, error)

	GetVendorName() string

	// A unique identifier for when detection happens on the DPU
	DpuPlatformIdentifier() plugin.DpuIdentifier
}

func NewDpuDetectorManager(platform Platform) *DpuDetectorManager {
	return &DpuDetectorManager{
		platform: platform,
		detectors: []VendorDetector{
			NewIntelDetector(),
			NewMarvellDetector(),
			NewNetsecAcceleratorDetector(),
			// add more detectors here
		},
	}
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

type DetectedDpuWithPlugin struct {
	DpuCR  *v1.DataProcessingUnit
	Plugin *plugin.GrpcPlugin
}

func (d *DpuDetectorManager) DetectAll(imageManager images.ImageManager, client client.Client, pm utils.PathManager) ([]*DetectedDpuWithPlugin, error) {
	var detectedDpus []*DetectedDpuWithPlugin

	for _, detector := range d.detectors {
		dpuPlatform, err := detector.IsDpuPlatform(d.platform)
		if err != nil {
			return nil, fmt.Errorf("Error detecting if running on DPU platform with detector %v: %v", detector.Name(), err)
		}

		if dpuPlatform {
			identifier := detector.DpuPlatformIdentifier()
			vsp, err := detector.VspPlugin(true, imageManager, client, pm, identifier)
			if err != nil {
				return nil, err
			}

			dpuCR := &v1.DataProcessingUnit{
				ObjectMeta: metav1.ObjectMeta{
					Name: string(identifier),
				},
				Spec: v1.DataProcessingUnitSpec{
					DpuProductName: detector.Name(),
					IsDpuSide:      true,
				},
				Status: v1.DataProcessingUnitStatus{
					Conditions: []metav1.Condition{
						{
							Type:               "Ready",
							Status:             metav1.ConditionFalse,
							Reason:             "Initializing",
							Message:            "DPU resource is being initialized.",
							LastTransitionTime: metav1.Now(),
						},
					},
				},
			}
			detectedDpus = append(detectedDpus, &DetectedDpuWithPlugin{
				DpuCR:  dpuCR,
				Plugin: vsp,
			})
			continue
		}

		devices, err := d.platform.PciDevices()
		if err != nil {
			return nil, errors.Errorf("Error getting PCI info: %v", err)
		}

		var dpuDevices []plugin.DpuIdentifier
		for _, pci := range devices {
			isDpu, err := detector.IsDPU(d.platform, *pci, dpuDevices)
			if err != nil {
				return nil, errors.Errorf("Error detecting if device is DPU with detector %v: %v", detector.Name(), err)
			}
			if isDpu {
				identifier, err := detector.GetDpuIdentifier(d.platform, pci)
				if err != nil {
					return nil, errors.Errorf("Error getting DPU identifier with detector %v: %v", detector.Name(), err)
				}
				dpuDevices = append(dpuDevices, identifier)
				vsp, err := detector.VspPlugin(false, imageManager, client, pm, identifier)
				if err != nil {
					return nil, err
				}

				dpuCR := &v1.DataProcessingUnit{
					ObjectMeta: metav1.ObjectMeta{
						Name: string(identifier),
					},
					Spec: v1.DataProcessingUnitSpec{
						DpuProductName: detector.Name(),
						IsDpuSide:      false,
					},
					Status: v1.DataProcessingUnitStatus{
						Conditions: []metav1.Condition{
							{
								Type:               "Ready",
								Status:             metav1.ConditionFalse,
								Reason:             "Initializing",
								Message:            "DPU resource is being initialized.",
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				}
				detectedDpus = append(detectedDpus, &DetectedDpuWithPlugin{
					DpuCR:  dpuCR,
					Plugin: vsp,
				})
			}
		}
	}
	return detectedDpus, nil
}
