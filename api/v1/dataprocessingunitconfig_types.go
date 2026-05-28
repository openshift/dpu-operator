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

// ========== DPU operation types ==========

type DpuOperationType string

const (
	// DpuOpNone No operation (default)
	DpuOpNone DpuOperationType = "None"
	// DpuOpFirmwareUpgrade Firmware upgrade operation
	DpuOpFirmwareUpgrade DpuOperationType = "FirmwareUpgrade"
	// DpuOpReboot DPU reboot operation (mandatory after firmware upgrade)
	DpuOpReboot DpuOperationType = "Reboot"
)

// ========== Firmware types ==========

type DpuFirmwareType string

const (
	// DpuFirmwareTypeOAM OAM type firmware
	DpuFirmwareTypeOAM DpuFirmwareType = "OAM"
	// DpuFirmwareTypeSDK SDK type firmware
	DpuFirmwareTypeSDK DpuFirmwareType = "SDK"
)

// ========== Operation status phases ==========

type DpuOperationStatusPhase string

const (
	// DpuPhasePending Operation pending execution (default)
	DpuPhasePending DpuOperationStatusPhase = "Pending"
	// DpuPhaseRunning Operation is in progress
	DpuPhaseRunning DpuOperationStatusPhase = "Running"
	// DpuPhaseRebooting DPU is rebooting (waiting for it to come back online)
	DpuPhaseRebooting DpuOperationStatusPhase = "Rebooting"
	// DpuPhaseSucceeded Operation completed successfully
	DpuPhaseSucceeded DpuOperationStatusPhase = "Succeeded"
	// DpuPhaseFailed Operation execution failed
	DpuPhaseFailed DpuOperationStatusPhase = "Failed"
	// DpuPhaseCancelled Operation was cancelled
	DpuPhaseCancelled DpuOperationStatusPhase = "Cancelled"
)

// ========== Health status ==========

type DpuHealthStatus string

const (
	// HealthStatusHealthy DPU is healthy and responding
	HealthStatusHealthy DpuHealthStatus = "Healthy"
	// HealthStatusUnhealthy DPU is not responding
	HealthStatusUnhealthy DpuHealthStatus = "Unhealthy"
	// HealthStatusUnknown DPU health is unknown
	HealthStatusUnknown DpuHealthStatus = "Unknown"
)

// ========== Firmware specification ==========

// DpuFirmwareSpec defines the firmware upgrade parameters.
type DpuFirmwareSpec struct {
	// Firmware type (OAM/SDK)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=OAM;SDK
	Type DpuFirmwareType `json:"type"`

	// Target firmware version number
	// +kubebuilder:validation:Required
	TargetVersion string `json:"targetVersion"`

	// Firmware image path or URI (e.g. quay.io/openshift/firmware/dpu:v1.0.8)
	// +optional
	FirmwarePath string `json:"firmwarePath,omitempty"`
}

// ========== DPU management (spec) ==========

// DataProcessingUnitManagement defines the desired management operation on a DPU.
type DataProcessingUnitManagement struct {
	// DPU operation type to execute: None, FirmwareUpgrade, or Reboot.
	// +kubebuilder:validation:Enum=None;FirmwareUpgrade;Reboot
	// +kubebuilder:default=None
	Operation DpuOperationType `json:"operation,omitempty"`

	// Detailed configuration for firmware upgrade.
	// Required when Operation is FirmwareUpgrade.
	// +optional
	Firmware *DpuFirmwareSpec `json:"firmware,omitempty"`
}

// ========== Spec ==========

// DataProcessingUnitConfigSpec defines the desired state of DataProcessingUnitConfig.
type DataProcessingUnitConfigSpec struct {
	// DpuSelector specifies which DPUs this config should target.
	// Must include a pci-address label to uniquely identify the DPU on
	// nodes that have more than one DPU of the same vendor.
	// +optional
	DpuSelector *metav1.LabelSelector `json:"dpuSelector,omitempty"`

	// DpuManagement specifies the management operation to perform.
	DpuManagement DataProcessingUnitManagement `json:"dpuManagement,omitempty"`
}

// ========== Status ==========

// DpuNodeOperationStatus tracks the status of the current management operation.
type DpuNodeOperationStatus struct {
	// SubOperation type: distinguishes FirmwareUpgrade from Reboot.
	SubOperation DpuOperationType `json:"subOperation,omitempty"`

	// FirmwareType (valid only when SubOperation is FirmwareUpgrade): OAM/SDK
	FirmwareType DpuFirmwareType `json:"firmwareType,omitempty"`

	// Phase is the current status of the operation: Pending/Running/Succeeded/Failed.
	Phase DpuOperationStatusPhase `json:"phase,omitempty"`

	// StartTime is when the operation started.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the operation completed (success or failure).
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// PreviousVersion is the firmware version before upgrade.
	// +optional
	PreviousVersion string `json:"previousVersion,omitempty"`

	// TargetVersion is the desired firmware version for upgrade.
	// +optional
	TargetVersion string `json:"targetVersion,omitempty"`

	// Message is a human-readable summary of the operation result.
	// +optional
	Message string `json:"message,omitempty"`

	// ErrorMessage contains error details when the operation fails.
	// +optional
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// DpuHealthInfo tracks the health / liveness of the DPU.
type DpuHealthInfo struct {
	// Status is the current health of the DPU: Healthy/Unhealthy/Unknown.
	Status DpuHealthStatus `json:"status,omitempty"`

	// Message is a human-readable health description.
	// +optional
	Message string `json:"message,omitempty"`

	// LastProbeTime is the last time the health was checked.
	// +optional
	LastProbeTime *metav1.Time `json:"lastProbeTime,omitempty"`
}

// ConditionTypeReady is the standard K8s condition used for kubectl-wait support.
//
//	kubectl wait dpuconfig/<name> --for=condition=Ready --timeout=600s
//
// Condition values:
//   - True:    phase=Succeeded and DPU is healthy
//   - False:   phase=Failed or DPU is unreachable
//   - Unknown: operation in progress (Rebooting/Running)
const ConditionTypeReady = "Ready"

// DataProcessingUnitConfigStatus defines the observed state of DataProcessingUnitConfig.
type DataProcessingUnitConfigStatus struct {
	// NodeStatus tracks the current management-operation status.
	NodeStatus DpuNodeOperationStatus `json:"nodeStatus,omitempty"`

	// Health tracks the DPU liveness information.
	Health DpuHealthInfo `json:"health,omitempty"`

	// Conditions holds standard Kubernetes status conditions.
	// The "Ready" condition supports `kubectl wait --for=condition=Ready`.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
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
