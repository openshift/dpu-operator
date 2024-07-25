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
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/apply"
	configv1 "github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/pkgs/render"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
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
	vspImage        string
	imagePullPolicy string
}

func NewDpuOperatorConfigReconciler(client client.Client, scheme *runtime.Scheme, dpuDaemonImage string, vspImage string) *DpuOperatorConfigReconciler {
	return &DpuOperatorConfigReconciler{
		Client:          client,
		Scheme:          scheme,
		dpuDaemonImage:  dpuDaemonImage,
		vspImage:        vspImage,
		imagePullPolicy: "IfNotPresent",
	}
}

func (r *DpuOperatorConfigReconciler) WithImagePullPolicy(policy string) *DpuOperatorConfigReconciler {
	r.imagePullPolicy = policy
	return r
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
//+kubebuilder:rbac:groups=k8s.cni.cncf.io,resources=network-attachment-definitions,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=create;get
//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create

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

	err := r.ensureVendorSpecificPlugin(ctx, dpuOperatorConfig)
	if err != nil {
		logger.Error(err, "Failed to ensure Vendor Specific Plugin is running")
		return ctrl.Result{}, err
	}

	err = r.ensureDpuDeamonSet(ctx, dpuOperatorConfig)
	if err != nil {
		logger.Error(err, "Failed to ensure Daemon is running")
		return ctrl.Result{}, err
	}

	if dpuOperatorConfig.Spec.Mode == "dpu" {
		err = r.ensureNetworkFunctioNAD(ctx, dpuOperatorConfig)
		if err != nil {
			logger.Error(err, "Failed to create Network Function NAD")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *DpuOperatorConfigReconciler) createCommonData(cfg *configv1.DpuOperatorConfig) map[string]string {
	// All the CRs will be in the same namespace as the operator config
	data := map[string]string{
		"Namespace":                 "openshift-dpu-operator",
		"ImagePullPolicy":           r.imagePullPolicy,
		"Mode":                      "auto",
		"DpuOperatorDaemonImage":    r.dpuDaemonImage,
		"VendorSpecificPluginImage": r.vspImage,
		"ResourceName":              "openshift.io/dpu", // FIXME: Hardcode for now
	}
	return data
}

func binDataYamlFiles(dirPath string) ([]string, error) {
	var yamlFileDescriptors []string

	dir, err := binData.ReadDir(filepath.Join("bindata", dirPath))
	if err != nil {
		return nil, err
	}

	for _, f := range dir {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".yaml") {
			yamlFileDescriptors = append(yamlFileDescriptors, filepath.Join(dirPath, f.Name()))
		}
	}

	sort.Strings(yamlFileDescriptors)
	return yamlFileDescriptors, nil
}

func (r *DpuOperatorConfigReconciler) applyFromBinData(logger logr.Logger, cfg *configv1.DpuOperatorConfig, filePath string, data map[string]string) error {
	file, err := binData.Open(filepath.Join("bindata", filePath))
	if err != nil {
		return fmt.Errorf("Failed to read file '%s': %v", filePath, err)
	}
	applied, err := render.ApplyTemplate(file, data)
	if err != nil {
		return fmt.Errorf("Failed to apply template on '%s': %v", filePath, err)
	}
	var obj *unstructured.Unstructured
	err = yaml.NewYAMLOrJSONDecoder(applied, 1024).Decode(&obj)
	if err != nil {
		return err
	}
	if err := ctrl.SetControllerReference(cfg, obj, r.Scheme); err != nil {
		return err
	}
	logger.Info("Preparing CR", "kind", obj.GetKind())
	if err := apply.ApplyObject(context.TODO(), r.Client, obj); err != nil {
		return fmt.Errorf("failed to apply object %v with err: %v", obj, err)
	}
	return nil
}

func (r *DpuOperatorConfigReconciler) applyAllFromBinData(logger logr.Logger, binDataPath string, cfg *configv1.DpuOperatorConfig) error {
	data := r.createCommonData(cfg)
	filePaths, err := binDataYamlFiles(binDataPath)
	if err != nil {
		return err
	}
	for _, f := range filePaths {
		err = r.applyFromBinData(logger, cfg, f, data)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *DpuOperatorConfigReconciler) ensureVendorSpecificPlugin(ctx context.Context, cfg *configv1.DpuOperatorConfig) error {
	logger := log.FromContext(ctx)
	logger.Info("Ensuring VSP DaemonSet", "image", r.vspImage)
	return r.applyAllFromBinData(logger, "vsp-ds", cfg)
}

func (r *DpuOperatorConfigReconciler) ensureDpuDeamonSet(ctx context.Context, cfg *configv1.DpuOperatorConfig) error {
	logger := log.FromContext(ctx)
	logger.Info("Ensuring DPU DaemonSet", "image", r.dpuDaemonImage)
	return r.applyAllFromBinData(logger, "daemon", cfg)
}

func (r *DpuOperatorConfigReconciler) ensureNetworkFunctioNAD(ctx context.Context, cfg *configv1.DpuOperatorConfig) error {
	logger := log.FromContext(ctx)
	logger.Info("Create the Network Function NAD")
	return r.applyAllFromBinData(logger, "networkfn-nad", cfg)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DpuOperatorConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1.DpuOperatorConfig{}).
		Complete(r)
}
