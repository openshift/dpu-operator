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
	"github.com/openshift/dpu-operator/internal/utils"
	"github.com/openshift/dpu-operator/pkgs/render"
	"github.com/openshift/dpu-operator/pkgs/vars"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//go:embed bindata/*
var binData embed.FS

const dpuOperatorConfigFinalizer = "config.openshift.io/dpuoperatorconfig-finalizer"

type componentError struct {
	component string
	err       error
}

// DpuOperatorConfigReconciler reconciles a DpuOperatorConfig object
type DpuOperatorConfigReconciler struct {
	client.Client
	imageManager    images.ImageManager
	imagePullPolicy string
	pathManager     utils.PathManager
}

func NewDpuOperatorConfigReconciler(client client.Client, imageManager images.ImageManager) *DpuOperatorConfigReconciler {
	return &DpuOperatorConfigReconciler{
		Client:          client,
		imageManager:    imageManager,
		imagePullPolicy: "IfNotPresent",
	}
}

func (r *DpuOperatorConfigReconciler) WithImagePullPolicy(policy string) *DpuOperatorConfigReconciler {
	r.imagePullPolicy = policy
	return r
}

//+kubebuilder:rbac:groups=config.openshift.io,resources=dataprocessingunits,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=config.openshift.io,resources=dataprocessingunits/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=config.openshift.io,resources=dataprocessingunits/finalizers,verbs=update
//+kubebuilder:rbac:groups=config.openshift.io,resources=dpuoperatorconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=config.openshift.io,resources=dpuoperatorconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=config.openshift.io,resources=dpuoperatorconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=config.openshift.io,resources=servicefunctionchains,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=config.openshift.io,resources=servicefunctionchains/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=config.openshift.io,resources=servicefunctionchains/finalizers,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups="",resources=*,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=*
//+kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=*
//+kubebuilder:rbac:groups="",resources=pods,verbs=*
//+kubebuilder:rbac:groups="",resources=secrets,verbs=*
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=*
//+kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=*
//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=k8s.cni.cncf.io,resources=network-attachment-definitions,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,resourceNames=anyuid;hostnetwork;privileged,verbs=use

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DpuOperatorConfig object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *DpuOperatorConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// req.NamespacedName always points to the DpuOperatorConfig (owner)
	// We reconcile the DpuOperatorConfig regardless of what triggered the reconcile
	dpuOperatorConfig := &configv1.DpuOperatorConfig{}
	if err := r.Get(ctx, req.NamespacedName, dpuOperatorConfig); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("DpuOperatorConfig resource not found. Ignoring.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get DpuOperatorConfig resource")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling DpuOperatorConfig", "name", dpuOperatorConfig.Name, "namespace", dpuOperatorConfig.Namespace)

	if !dpuOperatorConfig.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, dpuOperatorConfig)
	}

	if !controllerutil.ContainsFinalizer(dpuOperatorConfig, dpuOperatorConfigFinalizer) {
		logger.Info("Adding finalizer to DpuOperatorConfig")
		controllerutil.AddFinalizer(dpuOperatorConfig, dpuOperatorConfigFinalizer)
		if err := r.Update(ctx, dpuOperatorConfig); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Ensure Ready condition is set if finalizer is present
	if !meta.IsStatusConditionTrue(dpuOperatorConfig.Status.Conditions, "Ready") {
		r.setReadyCondition(dpuOperatorConfig, metav1.ConditionTrue, "FinalizerAdded", "Finalizer has been added and resource is ready")
		if err := r.Status().Update(ctx, dpuOperatorConfig); err != nil {
			logger.Error(err, "Failed to update Ready condition")
			return ctrl.Result{}, err
		}
	}

	// Initialize status if needed
	if err := r.initializeStatus(ctx, dpuOperatorConfig); err != nil {
		logger.Error(err, "Failed to initialize status")
		return ctrl.Result{}, err
	}

	// Track any errors during reconciliation with component information
	var reconcileErrors []componentError

	if err := r.ensureDpuDeamonSet(ctx, dpuOperatorConfig); err != nil {
		logger.Error(err, "Failed to ensure Daemon is running")
		reconcileErrors = append(reconcileErrors, componentError{component: "DpuDaemonSet", err: err})
	}

	if err := r.ensureNetworkFunctioNAD(ctx, dpuOperatorConfig); err != nil {
		logger.Error(err, "Failed to create Network Function NAD")
		reconcileErrors = append(reconcileErrors, componentError{component: "NetworkFunctionNAD", err: err})
	}

	if err := r.ensureNetworkResourcesInjector(ctx, dpuOperatorConfig); err != nil {
		logger.Error(err, "Failed to ensure Network Resources Injector is running")
		reconcileErrors = append(reconcileErrors, componentError{component: "NetworkResourcesInjector", err: err})
	}

	// Update status based on reconciliation results
	if err := r.updateStatus(ctx, dpuOperatorConfig, reconcileErrors); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *DpuOperatorConfigReconciler) handleDeletion(ctx context.Context, dpuOperatorConfig *configv1.DpuOperatorConfig) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling DpuOperatorConfig deletion")

	if controllerutil.ContainsFinalizer(dpuOperatorConfig, dpuOperatorConfigFinalizer) {
		logger.Info("Performing cleanup for DpuOperatorConfig")

		// Perform any cleanup operations here
		// For now, we'll just log that cleanup is complete
		logger.Info("Cleanup completed for DpuOperatorConfig")

		// Remove the finalizer to allow deletion
		controllerutil.RemoveFinalizer(dpuOperatorConfig, dpuOperatorConfigFinalizer)
		if err := r.Update(ctx, dpuOperatorConfig); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
		logger.Info("Finalizer removed from DpuOperatorConfig")
	}

	return ctrl.Result{}, nil
}

// setCondition sets the specified condition on the DpuOperatorConfig status
func (r *DpuOperatorConfigReconciler) setCondition(dpuOperatorConfig *configv1.DpuOperatorConfig, condition metav1.Condition) {
	meta.SetStatusCondition(&dpuOperatorConfig.Status.Conditions, condition)
}

// setReadyCondition sets the Ready condition with the given status, reason and message
func (r *DpuOperatorConfigReconciler) setReadyCondition(dpuOperatorConfig *configv1.DpuOperatorConfig, status metav1.ConditionStatus, reason, message string) {
	r.setCondition(dpuOperatorConfig, metav1.Condition{
		Type:    "Ready",
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}

// initializeStatus sets initial NotReady condition if no conditions exist
func (r *DpuOperatorConfigReconciler) initializeStatus(ctx context.Context, dpuOperatorConfig *configv1.DpuOperatorConfig) error {
	if len(dpuOperatorConfig.Status.Conditions) == 0 {
		r.setReadyCondition(dpuOperatorConfig, metav1.ConditionFalse, "WaitingForReconcile", "Waiting for reconcile to start DPU Daemons")
		return r.Status().Update(ctx, dpuOperatorConfig)
	}
	return nil
}

// updateStatus updates the status based on reconciliation results
func (r *DpuOperatorConfigReconciler) updateStatus(ctx context.Context, dpuOperatorConfig *configv1.DpuOperatorConfig, reconcileErrors []componentError) error {
	logger := log.FromContext(ctx)

	if len(reconcileErrors) > 0 {
		// Set NotReady condition with the first error, including component info in reason
		firstError := reconcileErrors[0]
		reason := fmt.Sprintf("%sError", firstError.component)
		r.setReadyCondition(dpuOperatorConfig, metav1.ConditionFalse, reason, firstError.err.Error())
		if updateErr := r.Status().Update(ctx, dpuOperatorConfig); updateErr != nil {
			logger.Error(updateErr, "Failed to update status with error condition")
			return updateErr
		}
		return firstError.err
	}

	// All components reconciled successfully
	logger.Info("All components reconciled successfully, setting Ready condition to True")
	r.setReadyCondition(dpuOperatorConfig, metav1.ConditionTrue, "ComponentsReady", "All DPU operator components deployed successfully")
	if updateErr := r.Status().Update(ctx, dpuOperatorConfig); updateErr != nil {
		logger.Error(updateErr, "Failed to update status with ready condition")
		return updateErr
	}

	return nil
}

func (r *DpuOperatorConfigReconciler) yamlVars() map[string]string {
	logger := log.FromContext(context.TODO())

	logger.Info("Detecting Kuberentes flavour")
	ce := utils.NewClusterEnvironment(r.Client)
	flavour, err := ce.Flavour(context.TODO())
	if err != nil {
		return nil
	}
	logger.Info("Detected Kuberentes flavour", "flavour", flavour)

	logger.Info("Detecting filesystem deployment mode")
	fmd := utils.NewFilesystemModeDetector()
	filesystemMode, err := fmd.DetectMode()
	if err != nil {
		logger.Error(err, "Failed to detect filesystem mode")
		return nil
	}
	logger.Info("Detected filesystem deployment mode", "mode", filesystemMode)

	p, err := r.pathManager.CniHostDir(flavour, filesystemMode)
	if err != nil {
		logger.Error(err, "Failed to determine CNI host directory", "flavour", flavour, "filesystemMode", filesystemMode)
		return nil
	}

	// All the CRs will be in the same namespace as the operator config
	data := map[string]string{
		"Namespace":       vars.Namespace,
		"ImagePullPolicy": r.imagePullPolicy,
		"Mode":            "auto",
		"ResourceName":    "openshift.io/dpu", // FIXME: Hardcode for now
		"CniDir":          p,
	}

	return data
}

func (r *DpuOperatorConfigReconciler) createAndApplyAllFromBinData(logger logr.Logger, binDataPath string, cfg *configv1.DpuOperatorConfig) error {
	mergedData := images.MergeVarsWithImages(r.imageManager, r.yamlVars())
	return render.ApplyAllFromBinData(logger, binDataPath, mergedData, binData, r.Client, cfg)
}

func (r *DpuOperatorConfigReconciler) ensureDpuDeamonSet(ctx context.Context, cfg *configv1.DpuOperatorConfig) error {
	logger := log.FromContext(ctx)
	daemonImage, err := r.imageManager.GetImage(images.DpuOperatorDaemonImage)
	if err != nil {
		return err
	}
	logger.Info("Ensuring DPU DaemonSet", "image", daemonImage)
	return r.createAndApplyAllFromBinData(logger, "daemon", cfg)
}

func (r *DpuOperatorConfigReconciler) ensureNetworkResourcesInjector(ctx context.Context, cfg *configv1.DpuOperatorConfig) error {
	logger := log.FromContext(ctx)
	logger.Info("Create Network Resources Injector")
	return r.createAndApplyAllFromBinData(logger, "network-resources-injector", cfg)
}
func (r *DpuOperatorConfigReconciler) ensureNetworkFunctioNAD(ctx context.Context, cfg *configv1.DpuOperatorConfig) error {
	logger := log.FromContext(ctx)

	// Both Host and DPU NADs are created here even though this operator can run exclusively on
	// either Host or DPU. The pod definition must choose the correct NAD for the node that the
	// pod will be running on (dpu would be networkfn-nad-dpu and host would be networkfn-nad-host).
	logger.Info("Create the Network Function DPU NAD")
	err := r.createAndApplyAllFromBinData(logger, "networkfn-nad-dpu", cfg)
	if err != nil {
		logger.Error(err, "Failed to create Network Function DPU NAD")
		return err
	}

	logger.Info("Create the Network Function Host NAD")
	err = r.createAndApplyAllFromBinData(logger, "networkfn-nad-host", cfg)
	if err != nil {
		logger.Error(err, "Failed to create Network Function Host NAD")
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DpuOperatorConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1.DpuOperatorConfig{}).
		Complete(r)
}
