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
	"github.com/openshift/dpu-operator/pkgs/vars"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var dpuoperatorconfiglog = logf.Log.WithName("dpuoperatorconfig-resource")

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *DpuOperatorConfig) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		WithValidator(r).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
//+kubebuilder:webhook:path=/validate-config-openshift-io-v1-dpuoperatorconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=config.openshift.io,resources=dpuoperatorconfigs,verbs=create;update,versions=v1,name=vdpuoperatorconfig.kb.io,admissionReviewVersions=v1

var _ webhook.CustomValidator = &DpuOperatorConfig{}

func (r *DpuOperatorConfig) validateDpuOperatorConfig() (admission.Warnings, error) {
	if r.Name != vars.DpuOperatorConfigName {
		return nil, fmt.Errorf("DpuOperatorConfig must have standard name \"" + vars.DpuOperatorConfigName + "\"")
	}

	mode := r.Spec.Mode
	if mode != "host" && mode != "dpu" && mode != "auto" {
		return nil, fmt.Errorf("Invalid mode")
	}

	return nil, nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *DpuOperatorConfig) ValidateCreate(tx context.Context, obj runtime.Object) (admission.Warnings, error) {
	r = obj.(*DpuOperatorConfig)
	dpuoperatorconfiglog.Info("validate create", "name", r.Name)
	return r.validateDpuOperatorConfig()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *DpuOperatorConfig) ValidateUpdate(ctx context.Context, oldObj runtime.Object, newObj runtime.Object) (admission.Warnings, error) {
	r = newObj.(*DpuOperatorConfig)
	dpuoperatorconfiglog.Info("validate update", "name", r.Name)
	return r.validateDpuOperatorConfig()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *DpuOperatorConfig) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	dpuoperatorconfiglog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}
