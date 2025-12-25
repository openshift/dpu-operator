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
	"time"

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
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//go:embed bindata
var dpuBinData embed.FS

// DataProcessingUnitConfigReconciler reconciles a DataProcessingUnitConfig object
type DataProcessingUnitConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	vsp    *plugin.VendorPlugin
	PciAddr string
}

func NewDataProcessingUnitConfigReconciler(client client.Client, scheme *runtime.Scheme, vsp *plugin.VendorPlugin) *DataProcessingUnitConfigReconciler {
	return &DataProcessingUnitConfigReconciler{
		Client:               client,
		Scheme:               scheme,
		vsp:                  vsp
	}
}

// +kubebuilder:rbac:groups=config.openshift.io,resources=dataprocessingunitconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.openshift.io,resources=dataprocessingunitconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=config.openshift.io,resources=dataprocessingunitconfigs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DataProcessingUnitConfig object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *DataProcessingUnitConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("DataProcessingUnitConfigReconciler")

	dpuConfig := &configv1.DataProcessingUnitConfig{}
	err := r.Get(ctx, req.NamespacedName, dpuConfig)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("DataProcessingUnitConfig not found. Ignoring.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get DataProcessingUnitConfig")
		return ctrl.Result{}, err
	}
    
	//DataprocessinUnitConfigReconciler is called by hostsidemanager, which means all the worker nodes within the cluster have a replica hostsidemanager locally
	// DpuSelector contains nodeName and pciAddr, here is to judge is the CR is targeting to this dpu on the node 
	labelMatched, err := IsLabelsMatched(dpuConfig)
	if labelMatched {
        log.Info("Labels are matched, continue processing")
	} else {
		//if label not matched, skip processing
		log.Info("Labels are not matched, skip processing")
		return ctrl.Result{}, nil
	}
    
	//If the CR is targeted at DPU firmware upgrading
	if dpuConfig.Spec.DpuManagement.Operation == "FirmwareUpgrade" {
		targetVersion := dpuConfig.Spec.DpuManagement.TargetVersion
		firmwareImagePath := dpuConfig.Spec.DpuManagement.FirmwarePath
		firmwareType := dpuConfig.Spec.DpuManagement.Type
        
		//Invoke the firmware upgrade operation via the gRPC method provided by the VSP gRPC server
		r.vsp.UpgradeFirmware(r.pciAddr, firmwareType, targetVersion, firmwareImagePath)
		//The return here is intended to prevent the DPU from being upgraded and rebooted simultaneously.
		return ctrl.Result{}, nil

	}
    
	//f the CR is targeted at DPU rebooting
	if dpuConfig.Spec.DpuManagement.Operation == "Reboot" {
		r.vsp.RebootDpu(r.pciAddr)
		return ctrl.Result{}, nil
	}
	
    return ctrl.Result{}, nil
}

func (r *DataProcessingUnitConfigReconciler) IsLabelsMatched(dpuConfig *configv1.DataProcessingUnitConfig) (bool, error) {
	// get current nodeName
	currentNodeName := getCurrentNodeName()
	if currentNodeName == "" {
		klog.Error("Failed to get current node name")
		return false, fmt.Errorf("current node name is empty")
	}
	klog.Info("Current node info", "currentNodeName", currentNodeName)
    
	var selector labels.Selector
	var nodeName, pciAddr string
	var err error

	// parse DpuSelector
	if dpuConfig.Spec.DpuSelector != nil {
		selector, err = metav1.LabelSelectorAsSelector(dpuConfig.Spec.DpuSelector)
		if err != nil {
			klog.Error(err, "Invalid DpuSelector")
			return false, fmt.Errorf("invalid DpuSelector: %v", err) 
		}
	}

	// Extract noodeName and pci-address from selector
	if dpuConfig.Spec.DpuSelector != nil {
		nodeName, pciAddr, err = GetNodenameAndPCIAddressFromSelector(dpuConfig.Spec.DpuSelector)
		if err != nil {
			klog.Errorf("Failed to get PCI address from selector: %v", err)
			return false, err
		}
		klog.Infof("Extracted nodeName: %s, PCI address: %s", nodeName, pciAddr)
	}

	//check the exsitence of pci-address
	currentPciAddr, err = CheckPCIAddressExists(pciAddr)
    r.PciAddr = currentPciAddr
	// construct current label
	currentLabels := labels.Set{
		"nodename":    currentNodeName,
		"pci-address": currentPciAddr,
	}

	// match labels
	matched := selector.Matches(currentLabels)
	klog.Infof("Label match result: %t (selector: %s, current labels: %v)", matched, selector.String(), currentLabels)
	return matched, nil
}

func (r *DataProcessingUnitConfigReconciler) CheckPCIAddressExists(pciAddr string) (string, error) {
	// check /sys/bus/pci/devices/pciAddr
	cmd := exec.Command("ls", fmt.Sprintf("/sys/bus/pci/devices/%s", pciAddr))
	// exec command and obtain err
	err := cmd.Run()
	if err == nil {
		//pciAddr exsits
		return pciAddr, nil
	}
	else {
		return "", nil
	}
}

func (r *DataProcessingUnitConfigReconciler) GetNodenameAndPCIAddressFromSelector(selector *metav1.LabelSelector) (string, string, error) {
	if selector == nil {
		return "", "", fmt.Errorf("DpuSelector is nil")
	}

	var nodeName, pciAddress string
	var nodeNameFound, pciAddressFound bool

	if selector.MatchLabels != nil {
		if pciAddr, ok := selector.MatchLabels["pci-address"]; ok {
			pciAddress = pciAddr
			pciAddressFound = true
		}

		if nn, ok := selector.MatchLabels["nodename"]; ok {
			nodeName = nn
			nodeNameFound = true
		}
	}

	if !nodeNameFound && !pciAddressFound {
		return "", "", fmt.Errorf("both nodename and pci-address labels not found in DpuSelector")
	} else if !nodenameFound {
		return "", pciAddress, fmt.Errorf("nodename label not found in DpuSelector (pci-address: %s)", pciAddress)
	} else if !pciAddressFound {
		return nodename, "", fmt.Errorf("pci-address label not found in DpuSelector (nodename: %s)", nodename)
	}

	return nodename, pciAddress, nil
}

func (r *DataProcessingUnitConfigReconciler) getCurrentNodeName() string {
	return os.Getenv("MY_NODE_NAME")
}

func (r *DataProcessingUnitConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	//init log
	r.log = mgr.GetLogger().WithName("DataProcessingUnitConfigReconciler")

	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1.DataProcessingUnitConfig{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Complete(r)
}

 