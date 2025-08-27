package sfcreconciler

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/dpu-operator/api/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// SfcReconciler reconciles a Service Function Chain object
type SfcReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	log      logr.Logger
	nodeName string
}

func networkFunctionPod(name string, image string, nodeSelector map[string]string) *corev1.Pod {
	trueVar := true
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Annotations: map[string]string{
				"k8s.v1.cni.cncf.io/networks": "dpunfcni-conf, dpunfcni-conf",
			},
		},
		Spec: corev1.PodSpec{
			NodeSelector: nodeSelector,
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

func (r *SfcReconciler) createOrUpdatePod(ctx context.Context, pod *corev1.Pod) error {
	err := r.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pod)
	if err != nil && errors.IsNotFound(err) {
		r.log.Info("Creating Pod", "name", pod.Name)
		if err := r.Create(ctx, pod); err != nil {
			r.log.Error(err, "Failed to create Pod", "pod", pod.Name, "namespace", pod.Namespace)
			return err
		}
		r.log.Info("Pod created successfully", "pod", pod.Name)
	} else if err == nil {
		r.log.Info("Updating Pod", "name", pod.Name)
		if err := r.Update(ctx, pod); err != nil {
			r.log.Error(err, "Failed to update Pod", "pod", pod.Name, "namespace", pod.Namespace)
			return err
		}
		r.log.Info("Pod updated successfully", "pod", pod.Name)
	} else {
		r.log.Error(err, "Failed to get Pod", "pod", pod.Name, "namespace", pod.Namespace)
		return err
	}
	return nil
}

func (r *SfcReconciler) ensureNetworkFunctionExists(ctx context.Context, sfc *configv1.ServiceFunctionChain, nf configv1.NetworkFunction) error {
	logger := r.log.WithValues("networkFunction", nf.Name)
	pod := networkFunctionPod(nf.Name, nf.Image, sfc.Spec.NodeSelector)

	if err := r.createOrUpdatePod(ctx, pod); err != nil {
		logger.Error(err, "Failed to ensure that pod exists")
		return err
	}

	return nil
}

// NewSfcReconciler creates a new SfcReconciler with the current node name
func NewSfcReconciler(client client.Client, scheme *runtime.Scheme) *SfcReconciler {
	nodeName := os.Getenv("K8S_NODE")
	if nodeName == "" {
		// Fallback to hostname if K8S_NODE is not set
		hostname, err := os.Hostname()
		if err != nil {
			nodeName = "unknown"
		} else {
			nodeName = hostname
		}
	}

	return &SfcReconciler{
		Client:   client,
		Scheme:   scheme,
		nodeName: nodeName,
	}
}

// matchesNodeSelector checks if the current node matches the ServiceFunctionChain's nodeSelector
func (r *SfcReconciler) matchesNodeSelector(ctx context.Context, nodeSelector map[string]string) (bool, error) {
	if len(nodeSelector) == 0 {
		// No node selector means match all nodes
		return true, nil
	}

	// Get the current node object
	node := &corev1.Node{}
	err := r.Get(ctx, types.NamespacedName{Name: r.nodeName}, node)
	if err != nil {
		r.log.Error(err, "Failed to get node", "nodeName", r.nodeName)
		return false, err
	}

	// Check if all nodeSelector labels match the node's labels
	for key, value := range nodeSelector {
		nodeValue, exists := node.Labels[key]
		if !exists || nodeValue != value {
			r.log.Info("Node selector does not match", "key", key, "expectedValue", value, "nodeValue", nodeValue, "exists", exists)
			return false, nil
		}
	}

	r.log.Info("Node selector matches current node", "nodeSelector", nodeSelector, "nodeName", r.nodeName)
	return true, nil
}

func (r *SfcReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.log = log.FromContext(ctx)
	r.log.Info("SfcReconciler", "nodeName", r.nodeName)

	sfc := &configv1.ServiceFunctionChain{}
	err := r.Get(ctx, req.NamespacedName, sfc)
	if err != nil {
		if errors.IsNotFound(err) {
			r.log.Info("ServiceFunctionChain not found, ignoring")
			return ctrl.Result{Requeue: true}, nil
		}
		r.log.Error(err, "Failed to get ServiceFunctionChain")
		return ctrl.Result{Requeue: true}, nil
	}

	// Check if this SFC should be reconciled on this node
	matches, err := r.matchesNodeSelector(ctx, sfc.Spec.NodeSelector)
	if err != nil {
		r.log.Error(err, "Failed to check node selector")
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	if !matches {
		r.log.Info("ServiceFunctionChain node selector does not match current node, skipping reconciliation",
			"nodeSelector", sfc.Spec.NodeSelector, "currentNode", r.nodeName)
		return ctrl.Result{}, nil
	}

	r.log.Info("ServiceFunctionChain matches current node, proceeding with reconciliation",
		"nodeSelector", sfc.Spec.NodeSelector, "currentNode", r.nodeName)

	for _, nf := range sfc.Spec.NetworkFunctions {
		err := r.ensureNetworkFunctionExists(ctx, sfc, nf)
		if err != nil {
			r.log.Error(err, "Failed to ensure network function exists", "networkFunction", nf.Name)
			return ctrl.Result{RequeueAfter: time.Minute}, err
		}
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
