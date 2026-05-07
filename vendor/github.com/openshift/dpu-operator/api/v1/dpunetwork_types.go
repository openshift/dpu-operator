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

// DpuNetworkSpec defines the desired state of DpuNetwork.
type DpuNetworkSpec struct {
	// NodeSelector specifies which nodes this DpuNetwork should apply to.
	// If empty, the DpuNetwork will apply to all nodes.
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// DpuSelector specifies which DPUs (and their VFs) this DpuNetwork targets.
	//
	// Note: Today this is treated as an opaque selector definition; the controller
	// parses vfId ranges from matchExpressions (if present) but does not yet
	// validate against a per-VF inventory.
	// +optional
	DpuSelector *metav1.LabelSelector `json:"dpuSelector,omitempty"`

	// IsAccelerated indicates whether the network should be treated as accelerated
	// by downstream components.
	// +optional
	IsAccelerated bool `json:"isAccelerated,omitempty"`
}

// DpuNetworkStatus defines the observed state of DpuNetwork.
type DpuNetworkStatus struct {
	// Conditions is the status of the DpuNetwork.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ResourceName is the Kubernetes extended resource name generated for this network.
	// +optional
	ResourceName string `json:"resourceName,omitempty"`

	// SelectedVFs is the list of VF IDs parsed from vfId ranges.
	// +optional
	SelectedVFs []int32 `json:"selectedVFs,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster,shortName=dpunet
//+kubebuilder:printcolumn:name="Resource",type="string",JSONPath=".status.resourceName"
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"

// DpuNetwork is the Schema for the dpunetworks API.
type DpuNetwork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DpuNetworkSpec   `json:"spec,omitempty"`
	Status DpuNetworkStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DpuNetworkList contains a list of DpuNetwork.
type DpuNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DpuNetwork `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DpuNetwork{}, &DpuNetworkList{})
}
