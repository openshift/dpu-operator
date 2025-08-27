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

// DataProcessingUnitSpec defines the desired state of DataProcessingUnit
type DataProcessingUnitSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// DpuProductName is the vendor and model name of the DPU
	DpuProductName string `json:"dpuProductName,omitempty"`
	// IsDpuSide indicates if this DPU is on the DPU side
	IsDpuSide bool `json:"isDpuSide"`
}

// DataProcessingUnitStatus defines the observed state of DataProcessingUnit
type DataProcessingUnitStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Status is the status of the DPU
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:shortName=dpu
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="DPU Product",type="string",JSONPath=".spec.dpuProductName"
//+kubebuilder:printcolumn:name="DPU Side",type="boolean",JSONPath=".spec.isDpuSide"
//+kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"

// DataProcessingUnit is the Schema for the dataprocessingunits API
type DataProcessingUnit struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DataProcessingUnitSpec   `json:"spec,omitempty"`
	Status DataProcessingUnitStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DataProcessingUnitList contains a list of DataProcessingUnit
type DataProcessingUnitList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataProcessingUnit `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DataProcessingUnit{}, &DataProcessingUnitList{})
}
