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
type ManualOperationReqObj struct {
	NodeName []string `json:"nodeName,omitempty"`
	PciList  []PciObj `json:"pciList,omitempty" patchStrategy:"merge" patchMergeKey:"PciAddr" protobuf:"bytes,1,rep,name=pciList"`
}

type PciObj struct {
	ManualReboot bool `json:"manualReboot,omitempty"`
	ManualUpgradeSdk bool  `json:"manualUpgrade,omitempty"`
	PciAddr string `json:"pciAddr,omitempty"`
	Enable  string `json:"enable,omitempty"`
	SdkImagePath string  `json:"sdkImagePath,omitempty"`
}

type ManualOperationStatObj struct {
	NodeName    []string       `json:"nodeName,omitempty"`
	PciStatList []PciStatObj `json:"pciStatList,omitempty" patchStrategy:"merge" patchMergeKey:"PciAddr" protobuf:"bytes,1,rep,name=pciStatList"`
}

type PciStatObj struct {
	PciAddr string `json:"pciAddr,omitempty"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

// ServiceFunctionChainSpec defines the desired state of ServiceFunctionChain
type ManualOperationSpec struct {
	ManualOperationReq ManualOperationReqObj `json:"manualOperationReq,omitempty"`
}


//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:shortName=sfc

// ServiceFunctionChain is the Schema for the servicefunctionchains API
type ManualOperation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ManualOperationSpec   `json:"spec,omitempty"`
	Status ManualOperationStatus `json:"status,omitempty"`
}

type ManualOperationStatus struct {

	ManualOperationState ManualOperationStatObj `json:"manualOperationState,omitempty"`
}

//+kubebuilder:object:root=true

// ServiceFunctionChainList contains a list of ServiceFunctionChain
type ManualOperationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManualOperation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManualOperation{}, &ManualOperationList{})
}
