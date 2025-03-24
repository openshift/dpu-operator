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
	"github.com/openshift/dpu-operator/pkgs/render"
	"github.com/openshift/dpu-operator/pkgs/vars"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//go:embed bindata/*
var binData embed.FS

// DpuOperatorConfigReconciler reconciles a DpuOperatorConfig object
type DpuOperatorConfigReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	dpuDaemonImage  string
	vspImages       map[string]string
	vspExtraData    map[string]string
	imagePullPolicy string
	nriWebhookImage string
}

func NewDpuOperatorConfigReconciler(client client.Client, scheme *runtime.Scheme, dpuDaemonImage string, vspImages map[string]string, vspExtraData map[string]string, nriWebhookImage string) *DpuOperatorConfigReconciler {
	return &DpuOperatorConfigReconciler{
		Client:          client,
		Scheme:          scheme,
		dpuDaemonImage:  dpuDaemonImage,
		vspImages:       vspImages,
		vspExtraData:    vspExtraData,
		imagePullPolicy: "IfNotPresent",
		nriWebhookImage: nriWebhookImage,
	}
}

func (r *DpuOperatorConfigReconciler) WithImagePullPolicy(policy string) *DpuOperatorConfigReconciler {
	r.imagePullPolicy = policy
	return r
}

//+kubebuilder:rbac:groups=config.openshift.io,resources=dpuoperatorconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=config.openshift.io,resources=dpuoperatorconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=config.openshift.io,resources=dpuoperatorconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=config.openshift.io,resources=servicefunctionchains/finalizers,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=roles,resources=*,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,resourceNames=anyuid;hostnetwork;privileged,verbs=use
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=k8s.cni.cncf.io,resources=network-attachment-definitions,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=create;get;update;patch;delete
//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=*
//+kubebuilder:rbac:groups="",resources=secrets,verbs=*
//+kubebuilder:rbac:groups="",resources=services,verbs=*
//+kubebuilder:rbac:groups="",resources=pods,verbs=*

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

	dpuOperatorConfig := &configv1.DpuOperatorConfig{}
	if err := r.Get(ctx, req.NamespacedName, dpuOperatorConfig); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("DpuOperatorConfig resource not found. Ignoring.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get DpuOperatorConfig resource")
		return ctrl.Result{}, err
	}

	err := r.ensureDpuDeamonSet(ctx, dpuOperatorConfig)
	if err != nil {
		logger.Error(err, "Failed to ensure Daemon is running")
		return ctrl.Result{}, err
	}

	err = r.ensureNetworkFunctioNAD(ctx, dpuOperatorConfig)
	if err != nil {
		logger.Error(err, "Failed to create Network Function NAD")
		return ctrl.Result{}, err
	}

	err = r.ensureNetworkResourcesInjector(ctx, dpuOperatorConfig)
	if err != nil {
		logger.Error(err, "Failed to ensure Network Resources Injector is running")
	}

	return ctrl.Result{}, nil
}

func (r *DpuOperatorConfigReconciler) createCommonData(cfg *configv1.DpuOperatorConfig) map[string]string {
	// All the CRs will be in the same namespace as the operator config
	data := map[string]string{
		"Namespace":              vars.Namespace,
		"ImagePullPolicy":        r.imagePullPolicy,
		"Mode":                   "auto",
		"DpuOperatorDaemonImage": r.dpuDaemonImage,
		"ResourceName":           "openshift.io/dpu", // FIXME: Hardcode for now
		"NRIWebhookImage":        r.nriWebhookImage,
	}

	for key, value := range r.vspImages {
		data[key] = value
	}
	for key, value := range r.vspExtraData {
		data[key] = value
	}

	return data
}

func (r *DpuOperatorConfigReconciler) createAndApplyAllFromBinData(logger logr.Logger, binDataPath string, cfg *configv1.DpuOperatorConfig) error {
	data := r.createCommonData(cfg)
	return render.ApplyAllFromBinData(logger, binDataPath, data, binData, r.Client, cfg, r.Scheme)
}

func (r *DpuOperatorConfigReconciler) ensureDpuDeamonSet(ctx context.Context, cfg *configv1.DpuOperatorConfig) error {
	logger := log.FromContext(ctx)
	logger.Info("Ensuring DPU DaemonSet", "image", r.dpuDaemonImage)
	return r.createAndApplyAllFromBinData(logger, "daemon", cfg)
}

func (r *DpuOperatorConfigReconciler) ensureNetworkResourcesInjector(ctx context.Context, cfg *configv1.DpuOperatorConfig) error {
	logger := log.FromContext(ctx)
	logger.Info("Create Network Resources Injector")
	return r.createAndApplyAllFromBinData(logger, "network-resources-injector", cfg)
}
func (r *DpuOperatorConfigReconciler) ensureNetworkFunctioNAD(ctx context.Context, cfg *configv1.DpuOperatorConfig) error {
	logger := log.FromContext(ctx)
	logger.Info("Create the Network Function NAD")
	nadFile := ""
	switch cfg.Spec.Mode {
	case "dpu":
		nadFile = "networkfn-nad-dpu"
	case "host":
		nadFile = "networkfn-nad-host"
	default:
		err := errors.NewBadRequest(fmt.Sprintf("Invalid Mode: %s", cfg.Spec.Mode))
		logger.Error(err, "Invalid mode specified")
		return err
	}
	return r.createAndApplyAllFromBinData(logger, nadFile, cfg)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DpuOperatorConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1.DpuOperatorConfig{}).
		Complete(r)
}
