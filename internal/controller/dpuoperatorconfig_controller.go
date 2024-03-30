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
	"fmt"
	"os"

	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/apply"
	"github.com/openshift/cluster-network-operator/pkg/render"
	configv1 "github.com/openshift/dpu-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// DpuOperatorConfigReconciler reconciles a DpuOperatorConfig object
type DpuOperatorConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=config.openshift.io,resources=dpuoperatorconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=config.openshift.io,resources=dpuoperatorconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=config.openshift.io,resources=dpuoperatorconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=roles,resources=*,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,resourceNames=anyuid;hostnetwork;privileged,verbs=use
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete

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
	err := r.ensureDpuDeamonSetRunning(ctx, dpuOperatorConfig)
	if err != nil {
		logger.Error(err, "Failed to ensure Daemon is running")
	}

	return ctrl.Result{}, nil
}

func getImagePullPolicy() string {
	if value, ok := os.LookupEnv("IMAGE_PULL_POLICIES"); ok {
		return value
	}
	return "IfNotPresent"
}

func (r *DpuOperatorConfigReconciler) ensureDpuDeamonSetRunning(ctx context.Context, cfg *configv1.DpuOperatorConfig) error {
	var err error

	logger := log.FromContext(ctx)
	data := render.MakeRenderData()
	// All the CRs will be in the same namespace as the operator config
	data.Data["Namespace"] = cfg.Namespace
	data.Data["Mode"] = cfg.Spec.Mode
	data.Data["ImagePullPolicy"] = getImagePullPolicy()
	dpuDaemonImage := os.Getenv("DPU_DAEMON_IMAGE")
	if dpuDaemonImage == "" {
		return fmt.Errorf("DPU_DAEMON_IMAGE not set")
	}
	data.Data["DpuOperatorDaemonImage"] = dpuDaemonImage

	logger.Info("Ensuring that DPU DaemonSet is running", "image", dpuDaemonImage)
	objs, err := render.RenderDir("./bindata/daemon", &data)
	if err != nil {
		logger.Error(err, "Failed to render dpu daemon manifests")
		return err
	}

	for _, obj := range objs {
		if err := ctrl.SetControllerReference(cfg, obj, r.Scheme); err != nil {
			return err
		}
	}

	for _, obj := range objs {
		logger.Info("Preparing CR", "kind", obj.GetKind())
		if obj.GetKind() == "DaemonSet" {
			scheme := r.Scheme
			ds := &appsv1.DaemonSet{}
			err = scheme.Convert(obj, ds, nil)
			if err != nil {
				logger.Error(err, "Fail to convert to DaemonSet")
				return err
			}
			ds.Spec.Template.Spec.NodeSelector["dpu"] = "true"
			err = scheme.Convert(ds, obj, nil)
			if err != nil {
				logger.Error(err, "Fail to convert to Unstructured")
				return err
			}
		}
		if err := apply.ApplyObject(context.TODO(), r.Client, obj); err != nil {
			return fmt.Errorf("failed to apply object %v with err: %v", obj, err)
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DpuOperatorConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1.DpuOperatorConfig{}).
		Complete(r)
}
