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

// ServiceFunctionChainSpec defines the desired state of ServiceFunctionChain
type ServiceFunctionChainSpec struct {
	// NodeSelector specifies which nodes this ServiceFunctionChain CR should be able to create the Network Function pod.
	// If empty, the ServiceFunctionChain will try to deploy the Network Function pod on all nodes.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	NetworkFunctions []NetworkFunction `json:"networkFunctions"`
}

// +kubebuilder:validation:XValidation:rule="has(self.image) != has(self.chart)",message="exactly one of image or chart must be set"
type NetworkFunction struct {
	Name string `json:"name"`

	// Image is the raw container image (legacy/simple deployment)
	Image string `json:"image,omitempty"`

	// Chart defines a Helm chart for complex vendor deployments
	Chart *HelmChartSource `json:"chart,omitempty"`
}

// HelmChartSource defines the location and version of a Helm chart
type HelmChartSource struct {
	// Repository URL where to locate the requested chart
	Repository string `json:"repository"`
	// Name of the requested chart
	Name string `json:"name"`
	// Version of the chart
	Version string `json:"version"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:shortName=sfc

// ServiceFunctionChain is the Schema for the servicefunctionchains API
type ServiceFunctionChain struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceFunctionChainSpec   `json:"spec,omitempty"`
	Status ServiceFunctionChainStatus `json:"status,omitempty"`
}

type ServiceFunctionChainStatus struct {
}

//+kubebuilder:object:root=true

// ServiceFunctionChainList contains a list of ServiceFunctionChain
type ServiceFunctionChainList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceFunctionChain `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceFunctionChain{}, &ServiceFunctionChainList{})
}
