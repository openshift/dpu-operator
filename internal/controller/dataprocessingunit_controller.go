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

package controller

import (
	"context"
	"embed"
	"fmt"

	configv1 "github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/internal/images"
	"github.com/openshift/dpu-operator/pkgs/render"
	"github.com/openshift/dpu-operator/pkgs/vars"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//go:embed bindata
var dpuBinData embed.FS

// DataProcessingUnitReconciler reconciles a DataProcessingUnit object
type DataProcessingUnitReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	imageManager    images.ImageManager
	imagePullPolicy string
}

func NewDataProcessingUnitReconciler(client client.Client, scheme *runtime.Scheme, imageManager images.ImageManager) *DataProcessingUnitReconciler {
	return &DataProcessingUnitReconciler{
		Client:          client,
		Scheme:          scheme,
		imageManager:    imageManager,
		imagePullPolicy: "Always",
	}
}

func (r *DataProcessingUnitReconciler) WithImagePullPolicy(policy string) *DataProcessingUnitReconciler {
	r.imagePullPolicy = policy
	return r
}

//+kubebuilder:rbac:groups=config.openshift.io,resources=dataprocessingunits,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=config.openshift.io,resources=dataprocessingunits/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=config.openshift.io,resources=dataprocessingunits/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *DataProcessingUnitReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the DataProcessingUnit instance
	dpu := &configv1.DataProcessingUnit{}
	err := r.Get(ctx, req.NamespacedName, dpu)
	if err != nil {
		if errors.IsNotFound(err) {
			// DPU was deleted, VSP resources will be cleaned up by owner references
			logger.Info("DataProcessingUnit deleted", "name", req.Name)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get DataProcessingUnit")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling DataProcessingUnit", "name", dpu.Name, "nodeName", dpu.Spec.NodeName, "dpuProductName", dpu.Spec.DpuProductName)

	// Ensure VSP resources exist
	err = r.ensureVSPResources(ctx, dpu)
	if err != nil {
		logger.Error(err, "Failed to ensure VSP resources")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *DataProcessingUnitReconciler) ensureVSPResources(ctx context.Context, dpu *configv1.DataProcessingUnit) error {
	logger := log.FromContext(ctx)

	// Create VSP template variables
	vspImage, err := r.getVSPImageForDPU(dpu)
	if err != nil {
		return fmt.Errorf("failed to get VSP image for DPU type %s: %v", dpu.Spec.DpuProductName, err)
	}

	templateVars := map[string]string{
		"Namespace":                 vars.Namespace,
		"VspName":                   r.getVSPName(dpu),
		"DpuName":                   dpu.Name,
		"NodeName":                  dpu.Spec.NodeName,
		"VendorSpecificPluginImage": vspImage,
		"ImagePullPolicy":           r.imagePullPolicy,
		"Command":                   "[]",
		"Args":                      "[]",
	}

	// Apply all VSP resources using the existing bindata
	// Pass nil for owner reference since render function expects DpuOperatorConfig
	// TODO: Consider creating a generic version that accepts any owner type
	err = render.ApplyAllFromBinData(logger, "vsp", templateVars, dpuBinData, r.Client, nil, r.Scheme)
	if err != nil {
		return fmt.Errorf("failed to apply VSP resources: %v", err)
	}

	logger.Info("Successfully ensured all VSP resources", "dpu", dpu.Name, "vspName", r.getVSPName(dpu))
	return nil
}

func (r *DataProcessingUnitReconciler) getVSPName(dpu *configv1.DataProcessingUnit) string {
	return fmt.Sprintf("vsp-%s", dpu.Name)
}

// dpuProductToImageKey maps DPU product names to their corresponding VSP image keys
var dpuProductToImageKey = map[string]string{
	"Intel IPU E2100":    images.VspImageIntel,
	"Marvell OCTEON":     images.VspImageMarvell,
	"NetSec Accelerator": images.VspImageIntelNetSec,
}

func (r *DataProcessingUnitReconciler) getVSPImageForDPU(dpu *configv1.DataProcessingUnit) (string, error) {
	// Look up the image key for the DPU product name
	imageKey, exists := dpuProductToImageKey[dpu.Spec.DpuProductName]
	if !exists {
		return "", fmt.Errorf("unknown DPU product name: %s", dpu.Spec.DpuProductName)
	}

	return r.imageManager.GetImage(imageKey)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataProcessingUnitReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1.DataProcessingUnit{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Complete(r)
}
