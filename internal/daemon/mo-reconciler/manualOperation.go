package MoReconciler

import (
	"context"
	"fmt"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/pkgs/vars"
	"k8s.io/apimachinery/pkg/api/resource"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
)

// MoReconciler reconciles a Service Function Chain object
type MoReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	log    logr.Logger
	vsp    plugin.VendorPlugin
}

func UpdateStatus(status *configv1.ManualOperationStatObj, nodeName, pciAddr, updStatus, message string, index int) {
	var nodeItem *configv1.ManualOperationStatObj

	(*nodeItem).PciStatList[index] = configv1.PciStatObj{PciAddr: pciAddr, Status: updStatus, Message: message}
}

func (r *MoReconciler) manualOperationImplement(mo *configv1.ManualOperation) error {
    
	var status *pb.ManualOperationResponse
	var err error

	for index, _ := range mo.Spec.ManualOperationReq.PciList{
        if mo.Spec.ManualOperationReq.PciList[index].ManualReboot == true {
            status, err = r.vsp.RebootDpu(mo.Spec.ManualOperationReq.NodeName[index], mo.Spec.ManualOperationReq.PciList[index].PciAddr)
		}else if mo.Spec.ManualOperationReq.PciList[index].ManualUpgradeSdk == true {
			status, err = r.vsp.UpgradeSdk(mo.Spec.ManualOperationReq.NodeName[index], mo.Spec.ManualOperationReq.PciList[index].PciAddr, mo.Spec.ManualOperationReq.PciList[index].SdkImagePath)
		}else{
			//do nothing
		}

		UpdateStatus(&mo.Status.ManualOperationState, mo.Spec.ManualOperationReq.NodeName[index], mo.Spec.ManualOperationReq.PciList[index].PciAddr,status.Status, status.Message, index)
	}

	
	return err
}

func (r *MoReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.log = log.FromContext(ctx)
	r.log.Info("MoReconciler")

	mo := &configv1.ManualOperation{}
	err := r.Get(ctx, req.NamespacedName, mo)
	if err != nil {
		return ctrl.Result{Requeue: true}, nil
	}

	r.manualOperationImplement(mo) //SynaXG added, FIXME
	return ctrl.Result{}, nil
}

var uniqueCounter int64 = 0

func (r *MoReconciler) uniqueName() string {
	val := atomic.AddInt64(&uniqueCounter, 1)
	return fmt.Sprintf("%s%d", "manualOperationFunction", val)
}

// SetupWithManager sets up the controller with the Manager.
func (r *MoReconciler) SetupWithManager(mgr ctrl.Manager, vsp plugin.VendorPlugin, ) error {
	r.vsp = vsp
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1.ServiceFunctionChain{}).
		Named(r.uniqueName()).
		Complete(r)
}

