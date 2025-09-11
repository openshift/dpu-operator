package sfcreconciler

import (
	"context"
	"fmt"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/pkgs/vars"
	"k8s.io/apimachinery/pkg/api/resource"
)

// SfcReconciler reconciles a Service Function Chain object
type SfcReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	log    logr.Logger
}

func networkFunctionPod(name string, image string) *corev1.Pod {
	trueVar := true
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: vars.Namespace,
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
						Privileged: &trueVar,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
							Add:  []corev1.Capability{"NET_RAW", "NET_ADMIN"},
						},
					},
				},
			},
		},
	}
}

func (r *SfcReconciler) createOrUpdatePod(ctx context.Context, desiredPod *corev1.Pod) error {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desiredPod.Name,
			Namespace: desiredPod.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, pod, func() error {
		// Set the annotation fields, metadata can change when a pod is created. We shouldn't override the metadata by using the desired pod
		// definition directly. Metadata fields shouldn't be removed. Instead we use use the mutation pattern (get actual object from API
		// server then modify it in place)
		if pod.Annotations == nil {
			pod.Annotations = make(map[string]string)
		}
		for key, value := range desiredPod.Annotations {
			pod.Annotations[key] = value
		}

		// Set the complete PodSpec from the template. The pod spec should not be changed directly when using the sfc CR.
		pod.Spec = desiredPod.Spec

		return nil
	})

	if err != nil {
		r.log.Error(err, "Failed to create or update Pod", "pod", pod.Name, "namespace", pod.Namespace)
		return err
	}

	r.log.Info("Pod created or updated successfully", "pod", pod.Name, "namespace", pod.Namespace)
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

var uniqueCounter int64 = 0

func (r *SfcReconciler) uniqueName() string {
	val := atomic.AddInt64(&uniqueCounter, 1)
	return fmt.Sprintf("%s%d", "servicefunctionchain", val)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SfcReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1.ServiceFunctionChain{}).
		Named(r.uniqueName()).
		Complete(r)
}
