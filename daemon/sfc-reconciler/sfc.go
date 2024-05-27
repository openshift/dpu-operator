package sfcreconciler

import (
	"context"

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
	"k8s.io/apimachinery/pkg/api/resource"
)

// SfcReconciler reconciles a Service Function Chain object
type SfcReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	log    logr.Logger
}

func networkFunctionPod(name string, image string) *corev1.Pod {
	falseVar := false
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "dpu-operator-system",
			Annotations: map[string]string{
				"k8s.v1.cni.cncf.io/networks": "dpunfcni-conf, dpunfcni-conf",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  name,
					Image: image,
					Ports: []corev1.ContainerPort{
						{
							Name:          "web",
							ContainerPort: 8080,
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"openshift.io/dpu": resource.MustParse("2"),
						},
						Limits: corev1.ResourceList{
							"openshift.io/dpu": resource.MustParse("2"),
						},
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &falseVar,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
							Add:  []corev1.Capability{"NET_RAW"},
						},
					},
				},
			},
		},
	}
}

func (r *SfcReconciler) createOrUpdatePod(ctx context.Context, pod *corev1.Pod) error {
	err := r.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)
	if err != nil && errors.IsNotFound(err) {
		r.log.Info("Creating Pod", "name", pod.Name)
		if err := r.Create(ctx, pod); err != nil {
			r.log.Error(err, "Failed to create Pod", "pod", pod.Name)
			return err
		}
		r.log.Info("Pod created successfully", "pod", pod.Name)
	} else if err == nil {
		r.log.Info("Updating Pod", "name", pod.Name)
		if err := r.Update(ctx, pod); err != nil {
			r.log.Error(err, "Failed to update Pod", "pod", pod.Name)
			return err
		}
		r.log.Info("Pod updated successfully", "pod", pod.Name)
	} else {
		r.log.Error(err, "Failed to get Pod", "pod", pod.Name)
		return err
	}
	return nil
}

func (r *SfcReconciler) ensureNetworkFunctionExists(ctx context.Context, sfc *configv1.ServiceFunctionChain, nf configv1.NetworkFunction) error {
	logger := r.log.WithValues("networkFunction", nf.Name)
	pod := networkFunctionPod(nf.Name, nf.Image)

	if err := controllerutil.SetControllerReference(sfc, pod, r.Scheme); err != nil {
		logger.Error(err, "Failed to set owner reference on Pod")
		return err
	}

	if err := r.createOrUpdatePod(ctx, pod); err != nil {
		logger.Error(err, "Failed to ensure that pod exists")
		return err
	}

	return nil
}

func (r *SfcReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.log = log.FromContext(ctx)
	r.log.Info("SfcReconciler")

	sfc := &configv1.ServiceFunctionChain{}
	err := r.Get(ctx, req.NamespacedName, sfc)
	if err != nil {
		return ctrl.Result{Requeue: true}, nil
	}

	for _, nf := range sfc.Spec.NetworkFunctions {
		r.ensureNetworkFunctionExists(ctx, sfc, nf)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SfcReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1.ServiceFunctionChain{}).
		Complete(r)
}
