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

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/internal/images"
	"github.com/openshift/dpu-operator/internal/platform"
	"github.com/openshift/dpu-operator/pkgs/render"
	"github.com/openshift/dpu-operator/pkgs/vars"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const dataProcessingUnitFinalizer = "config.openshift.io/dataprocessingunit-finalizer"

//go:embed bindata
var dpuBinData embed.FS

// DataProcessingUnitReconciler reconciles a DataProcessingUnit object
type DataProcessingUnitReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	imageManager    images.ImageManager
	imagePullPolicy string

	// Track created VSP resources per DataProcessingUnit for cleanup
	vspResourceRenderers map[string]*render.ResourceRenderer
}

func NewDataProcessingUnitReconciler(client client.Client, scheme *runtime.Scheme, imageManager images.ImageManager) *DataProcessingUnitReconciler {
	return &DataProcessingUnitReconciler{
		Client:               client,
		Scheme:               scheme,
		imageManager:         imageManager,
		imagePullPolicy:      "Always",
		vspResourceRenderers: make(map[string]*render.ResourceRenderer),
	}
}

func (r *DataProcessingUnitReconciler) WithImagePullPolicy(policy string) *DataProcessingUnitReconciler {
	r.imagePullPolicy = policy
	return r
}

// +kubebuilder:rbac:groups="",resources=pods,verbs=*
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=*
// +kubebuilder:rbac:groups="",resources=services,verbs=*
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=*
// +kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=*
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,resourceNames=anyuid;hostnetwork;privileged,verbs=use
func (r *DataProcessingUnitReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the DataProcessingUnit instance
	dpu := &configv1.DataProcessingUnit{}
	err := r.Get(ctx, req.NamespacedName, dpu)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("DataProcessingUnit not found. Ignoring.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get DataProcessingUnit")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling DataProcessingUnit", "name", dpu.Name, "nodeName", dpu.Spec.NodeName, "dpuProductName", dpu.Spec.DpuProductName)

	if dpu.GetDeletionTimestamp() != nil {
		return r.handleDeletion(ctx, dpu)
	}

	// Ensure VSP resources exist
	err = r.ensureVSPResources(ctx, dpu)
	if err != nil {
		logger.Error(err, "Failed to ensure VSP resources")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *DataProcessingUnitReconciler) handleDeletion(ctx context.Context, dpu *configv1.DataProcessingUnit) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling DataProcessingUnit deletion")

	if controllerutil.ContainsFinalizer(dpu, dataProcessingUnitFinalizer) {
		logger.Info("Performing cleanup for DataProcessingUnit")

		if err := r.cleanupVSPResources(ctx, dpu, logger); err != nil {
			logger.Error(err, "Failed to cleanup VSP resources")
			return ctrl.Result{}, err
		}

		logger.Info("Cleanup completed for DataProcessingUnit")

		controllerutil.RemoveFinalizer(dpu, dataProcessingUnitFinalizer)
		if err := r.Update(ctx, dpu); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
		logger.Info("Finalizer removed from DataProcessingUnit")
	}

	return ctrl.Result{}, nil
}

func (r *DataProcessingUnitReconciler) cleanupVSPResources(ctx context.Context, dpu *configv1.DataProcessingUnit, logger logr.Logger) error {
	renderer, exists := r.vspResourceRenderers[dpu.Name]
	if !exists {
		logger.Info("No VSP resource renderer found, nothing to clean up", "dpu", dpu.Name)
		return nil
	}
	return renderer.CleanupResourcesInReverseOrder(ctx, r.Client, logger)
}

func (r *DataProcessingUnitReconciler) ensureVSPResources(ctx context.Context, dpu *configv1.DataProcessingUnit) error {
	logger := log.FromContext(ctx)

	// Add finalizer if not present, just before creating VSP resources
	if !controllerutil.ContainsFinalizer(dpu, dataProcessingUnitFinalizer) {
		logger.Info("Adding finalizer to DataProcessingUnit before creating VSP resources")
		controllerutil.AddFinalizer(dpu, dataProcessingUnitFinalizer)
		err := r.Update(ctx, dpu)
		if err != nil {
			return fmt.Errorf("failed to add finalizer: %v", err)
		}
	}

	// Create VSP template variables
	vspImage, err := r.getVSPImageForDPU(dpu)
	if err != nil {
		return fmt.Errorf("failed to get VSP image for DPU type %s: %v", dpu.Spec.DpuProductName, err)
	}

	additionalVars := map[string]string{
		"Namespace":                 vars.Namespace,
		"VspName":                   r.getVSPName(dpu),
		"DpuName":                   dpu.Name,
		"NodeName":                  dpu.Spec.NodeName,
		"VendorSpecificPluginImage": vspImage,
		"ImagePullPolicy":           r.imagePullPolicy,
		"Command":                   "[]",
		"Args":                      "[]",
	}
	templateVars := images.MergeVarsWithImages(r.imageManager, additionalVars)

	// Apply shared VSP resources (ServiceAccount, Roles, etc.) - owned by DataProcessingUnit
	// TODO: refcount so that we clean up when the last one is removed
	err = r.applyVSPResourcesWithTracking(logger, "vsp/shared", templateVars, dpu)
	if err != nil {
		return fmt.Errorf("failed to apply shared VSP resources: %v", err)
	}

	// Apply vendor-specific VSP resources (Pod) - owned by DataProcessingUnit
	vendorDir, err := r.getVendorDirectory(dpu)
	if err != nil {
		return fmt.Errorf("failed to get vendor directory for DPU type %s: %v", dpu.Spec.DpuProductName, err)
	}
	err = r.applyVSPResourcesWithTracking(logger, vendorDir, templateVars, dpu)
	if err != nil {
		return fmt.Errorf("failed to apply vendor-specific VSP resources: %v", err)
	}

	logger.Info("Successfully ensured all VSP resources", "dpu", dpu.Name, "vspName", r.getVSPName(dpu))
	return nil
}

func (r *DataProcessingUnitReconciler) applyVSPResourcesWithTracking(logger logr.Logger, binDataPath string, data map[string]string, dpu *configv1.DataProcessingUnit) error {
	// Get or create VSP resource renderer for this DataProcessingUnit
	renderer, exists := r.vspResourceRenderers[dpu.Name]
	if !exists {
		dpuKey := dpu.Name
		renderer = render.NewResourceRenderer(dpuKey)
		r.vspResourceRenderers[dpu.Name] = renderer
	}

	return renderer.ApplyAllFromBinData(logger, binDataPath, data, dpuBinData, r.Client, dpu)
}

func (r *DataProcessingUnitReconciler) getVSPName(dpu *configv1.DataProcessingUnit) string {
	return fmt.Sprintf("vsp-%s", dpu.Name)
}

func (r *DataProcessingUnitReconciler) getVendorDirectory(dpu *configv1.DataProcessingUnit) (string, error) {
	detectorManager := platform.NewDpuDetectorManager(platform.NewHardwarePlatform())
	vendorDir, err := detectorManager.GetVendorDirectory(dpu.Spec.DpuProductName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("vsp/%s", vendorDir), nil
}

func (r *DataProcessingUnitReconciler) getVSPImageForDPU(dpu *configv1.DataProcessingUnit) (string, error) {
	// Get the detector manager to find the platform identifier
	detectorManager := platform.NewDpuDetectorManager(platform.NewHardwarePlatform())

	// Find the detector for this DPU product name and get its platform identifier
	for _, detector := range detectorManager.GetDetectors() {
		if detector.Name() == dpu.Spec.DpuProductName {
			identifier := detector.DpuPlatformIdentifier()
			templateKey := platform.SanitizeForTemplate(identifier)
			return r.imageManager.GetImage(templateKey)
		}
	}

	return "", fmt.Errorf("unknown DPU product name: %s", dpu.Spec.DpuProductName)
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
