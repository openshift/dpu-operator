/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// ========== Add: Define supported DPU operation types ==========
type DpuOperationType string

const (
	// DpuOpNone No operation (default)
	DpuOpNone DpuOperationType = "None"
	// DpuOpFirmwareUpgrade Firmware upgrade operation
	DpuOpFirmwareUpgrade DpuOperationType = "FirmwareUpgrade"
	// DpuOpRestart DPU restart operation (mandatory after firmware upgrade)
	DpuOpRestart DpuOperationType = "Reboot"
)

// ========== Add: Define firmware types ==========
type DpuFirmwareType string

const (
	// DpuFirmwareTypeOAM OAM type firmware
	DpuFirmwareTypeOAM DpuFirmwareType = "OAM"
	// DpuFirmwareTypeSDK SDK type firmware
	DpuFirmwareTypeSDK DpuFirmwareType = "SDK"
)

type DpuOperationStatusPhase string

const (
	// DpuPhasePending Operation pending execution (default)
	DpuPhasePending DpuOperationStatusPhase = "Pending"
	// DpuPhaseRunning Operation is in progress
	DpuPhaseRunning DpuOperationStatusPhase = "Running"
	// DpuPhaseSucceeded Operation completed successfully
	DpuPhaseSucceeded DpuOperationStatusPhase = "Succeeded"
	// DpuPhaseFailed Operation execution failed
	DpuPhaseFailed DpuOperationStatusPhase = "Failed"
	// DpuPhaseCancelled Operation was cancelled
	DpuPhaseCancelled DpuOperationStatusPhase = "Cancelled"
)

// ========== Add: Define OAM firmware configuration ==========
type DpuFirmwareSpec struct {
	// Firmware type (OAM/SDK)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=OAM;SDK
	Type DpuFirmwareType `json:"type"`

	// Target firmware version number, required
	// +kubebuilder:validation:Required
	TargetVersion string `json:"targetVersion"`

	// Firmware image path/package path (e.g. /quay.io/openshift/firmware/dpu:v1.0.8)
	// +kubebuilder:validation:Required
	FirmwarePath string `json:"firmwarePath,omitempty"`

}

type DataProcessingUnitManagement struct{
	// DPU operation type to execute: None/FirmwareUpgrade/Restart
	// Modify this field to trigger the corresponding operation!!!
	Operation DpuOperationType `json:"operation,omitempty"`

	// Detailed configuration for firmware upgrade, required when Operation is upgrade type
	// +kubebuilder:validation:RequiredWhen=Operation,FirmwareUpgrade
	Firmware DpuFirmwareSpec `json:"firmware,omitempty"`

}
// DataProcessingUnitConfigSpec defines the desired state of DataProcessingUnitConfig.
type DataProcessingUnitConfigSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// DpuSelector specifies which DPUs this DpuConfig CR should target.
	// If empty, the DpuConfig will target all DPUs.
	//matchLabels: nodeName, pci-address
	//pci-address is required. 1 node might have multiple DPUs of the same vendor.
	//so the specify the target DPU, pci-address is necessary
	DpuSelector *metav1.LabelSelector `json:"dpuSelector,omitempty"`

	//each DPU has 1 specific CR
    DpuManagement DataProcessingUnitManagement `json:"dpuManagement,omitempty"`
}

// DpuNodeOperationStatus defines the observed state of DataProcessingUnitConfig.
type DpuNodeOperationStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	NodeName string `json:"nodeName"`

	// PciAddress of the target DPU device on this node.
	// +optional
	PciAddress string `json:"pciAddress,omitempty"`

	// Sub-operation type: distinguish between FirmwareUpgrade and Restart
	SubOperation DpuOperationType `json:"subOperation"`

	// Firmware type (valid only when SubOperation is FirmwareUpgrade): OAM/SDK
	FirmwareType DpuFirmwareType `json:"firmwareType,omitempty"`

	// Operation execution status: Pending/Running/Succeeded/Failed
	Phase cv  `json:"phase"`

	// Operation start time
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Operation completion time
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Upgrade-related versions (valid only when SubOperation is FirmwareUpgrade)
	OriginalVersion string `json:"originalVersion,omitempty"` // Version before upgrade
	TargetVersion   string `json:"targetVersion,omitempty"`   // Target version for upgrade
	
	// Error message (required when operation fails)
	ErrorMessage string `json:"errorMessage,omitempty"`
}

type DataProcessingUnitConfigStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	NodeStatus DpuNodeOperationStatus `json:"nodeStatuses,omitempty"`
}
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=dpuconfig

// DataProcessingUnitConfig is the Schema for the dataprocessingunitconfigs API.
type DataProcessingUnitConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DataProcessingUnitConfigSpec   `json:"spec,omitempty"`
	Status DataProcessingUnitConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DataProcessingUnitConfigList contains a list of DataProcessingUnitConfig.
type DataProcessingUnitConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataProcessingUnitConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DataProcessingUnitConfig{}, &DataProcessingUnitConfigList{})
}
