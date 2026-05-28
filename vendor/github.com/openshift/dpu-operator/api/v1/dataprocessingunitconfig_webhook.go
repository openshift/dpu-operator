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
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var dpuconfiglog = logf.Log.WithName("dataprocessingunitconfig-resource")

// SetupWebhookWithManager registers the validating webhook for DataProcessingUnitConfig.
// Vendor-specific defaults (e.g. default firmware image) and allowlist enforcement are
// handled inside each VSP process, which is the only component that knows the vendor.
func (r *DataProcessingUnitConfig) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		WithValidator(r).
		Complete()
}

// +kubebuilder:webhook:path=/validate-config-openshift-io-v1-dataprocessingunitconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=config.openshift.io,resources=dataprocessingunitconfigs,verbs=create;update,versions=v1,name=vdataprocessingunitconfig.kb.io,admissionReviewVersions=v1

var _ webhook.CustomValidator = &DataProcessingUnitConfig{}

// validateDataProcessingUnitConfig performs vendor-neutral structural validation.
func validateDataProcessingUnitConfig(cfg *DataProcessingUnitConfig) (admission.Warnings, error) {
	if cfg.Spec.DpuManagement.Operation == DpuOpFirmwareUpgrade &&
		cfg.Spec.DpuManagement.Firmware == nil {
		return nil, fmt.Errorf("spec.dpuManagement.firmware is required when operation is FirmwareUpgrade")
	}
	return nil, nil
}

// ValidateCreate validates a new DataProcessingUnitConfig on creation.
func (r *DataProcessingUnitConfig) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	cfg := obj.(*DataProcessingUnitConfig)
	dpuconfiglog.Info("validate create", "name", cfg.Name)
	return validateDataProcessingUnitConfig(cfg)
}

// ValidateUpdate validates changes to an existing DataProcessingUnitConfig.
func (r *DataProcessingUnitConfig) ValidateUpdate(ctx context.Context, oldObj runtime.Object, newObj runtime.Object) (admission.Warnings, error) {
	cfg := newObj.(*DataProcessingUnitConfig)
	dpuconfiglog.Info("validate update", "name", cfg.Name)
	return validateDataProcessingUnitConfig(cfg)
}

// ValidateDelete allows deletion without restriction.
func (r *DataProcessingUnitConfig) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
