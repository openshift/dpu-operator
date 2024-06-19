//go:build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DpuOperatorConfig) DeepCopyInto(out *DpuOperatorConfig) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DpuOperatorConfig.
func (in *DpuOperatorConfig) DeepCopy() *DpuOperatorConfig {
	if in == nil {
		return nil
	}
	out := new(DpuOperatorConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DpuOperatorConfig) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DpuOperatorConfigList) DeepCopyInto(out *DpuOperatorConfigList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]DpuOperatorConfig, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DpuOperatorConfigList.
func (in *DpuOperatorConfigList) DeepCopy() *DpuOperatorConfigList {
	if in == nil {
		return nil
	}
	out := new(DpuOperatorConfigList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DpuOperatorConfigList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DpuOperatorConfigSpec) DeepCopyInto(out *DpuOperatorConfigSpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DpuOperatorConfigSpec.
func (in *DpuOperatorConfigSpec) DeepCopy() *DpuOperatorConfigSpec {
	if in == nil {
		return nil
	}
	out := new(DpuOperatorConfigSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DpuOperatorConfigStatus) DeepCopyInto(out *DpuOperatorConfigStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DpuOperatorConfigStatus.
func (in *DpuOperatorConfigStatus) DeepCopy() *DpuOperatorConfigStatus {
	if in == nil {
		return nil
	}
	out := new(DpuOperatorConfigStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NetworkFunction) DeepCopyInto(out *NetworkFunction) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NetworkFunction.
func (in *NetworkFunction) DeepCopy() *NetworkFunction {
	if in == nil {
		return nil
	}
	out := new(NetworkFunction)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceFunctionChain) DeepCopyInto(out *ServiceFunctionChain) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceFunctionChain.
func (in *ServiceFunctionChain) DeepCopy() *ServiceFunctionChain {
	if in == nil {
		return nil
	}
	out := new(ServiceFunctionChain)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ServiceFunctionChain) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceFunctionChainList) DeepCopyInto(out *ServiceFunctionChainList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ServiceFunctionChain, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceFunctionChainList.
func (in *ServiceFunctionChainList) DeepCopy() *ServiceFunctionChainList {
	if in == nil {
		return nil
	}
	out := new(ServiceFunctionChainList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceFunctionChainSpec) DeepCopyInto(out *ServiceFunctionChainSpec) {
	*out = *in
	if in.NetworkFunctions != nil {
		in, out := &in.NetworkFunctions, &out.NetworkFunctions
		*out = make([]NetworkFunction, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceFunctionChainSpec.
func (in *ServiceFunctionChainSpec) DeepCopy() *ServiceFunctionChainSpec {
	if in == nil {
		return nil
	}
	out := new(ServiceFunctionChainSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceFunctionChainStatus) DeepCopyInto(out *ServiceFunctionChainStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceFunctionChainStatus.
func (in *ServiceFunctionChainStatus) DeepCopy() *ServiceFunctionChainStatus {
	if in == nil {
		return nil
	}
	out := new(ServiceFunctionChainStatus)
	in.DeepCopyInto(out)
	return out
}
