package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	netattdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	configv1 "github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/pkgs/vars"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	dpuDevicePluginConfigMapName  = "dpu-device-plugin-config"
	defaultDpuNetworkNADNamespace = "default"
	dpuNetworkFinalizer           = "config.openshift.io/dpunetwork-cleanup"
)

type dpuDevicePluginConfig struct {
	Resources []dpuDevicePluginResource `json:"resources"`
}

type dpuDevicePluginResource struct {
	ResourceName   string                `json:"resourceName"`
	DpuNetworkName string                `json:"dpuNetworkName"`
	NodeSelector   *metav1.LabelSelector `json:"nodeSelector,omitempty"`
	VfRanges       []string              `json:"vfRanges,omitempty"`
	IsAccelerated  bool                  `json:"isAccelerated,omitempty"`
}

// DpuNetworkReconciler reconciles a DpuNetwork object
type DpuNetworkReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=config.openshift.io,resources=dpunetworks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=config.openshift.io,resources=dpunetworks/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=config.openshift.io,resources=dpunetworks/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=k8s.cni.cncf.io,resources=network-attachment-definitions,verbs=get;list;watch;create;update;patch;delete

func (r *DpuNetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	net := &configv1.DpuNetwork{}
	if err := r.Get(ctx, req.NamespacedName, net); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion: remove this network's entry from the shared ConfigMap,
	// then remove the finalizer so Kubernetes can delete the CR.
	if !net.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(net, dpuNetworkFinalizer) {
			if err := r.removeFromDevicePluginConfigMap(ctx, net.Name); err != nil {
				logger.Error(err, "Failed to clean up ConfigMap entry on deletion")
				return ctrl.Result{}, err
			}
			if err := r.deleteNAD(ctx, net.Name); err != nil {
				logger.Error(err, "Failed to delete NAD on deletion")
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(net, dpuNetworkFinalizer)
			if err := r.Update(ctx, net); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Ensure finalizer is present for cleanup on deletion.
	if !controllerutil.ContainsFinalizer(net, dpuNetworkFinalizer) {
		controllerutil.AddFinalizer(net, dpuNetworkFinalizer)
		if err := r.Update(ctx, net); err != nil {
			return ctrl.Result{}, err
		}
	}

	resourceName := fmt.Sprintf("openshift.io/dpunetwork-%s", net.Name)
	selectedVFs, vfRanges := parseVfRangesFromSelector(net.Spec.DpuSelector)

	if meta.FindStatusCondition(net.Status.Conditions, "Ready") == nil {
		meta.SetStatusCondition(&net.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "Reconciling",
			Message: "Reconciling DpuNetwork",
		})
	}

	// Ensure ConfigMap used by per-node daemons/device plugins.
	if err := r.ensureDevicePluginConfigMap(ctx, net, resourceName, vfRanges); err != nil {
		meta.SetStatusCondition(&net.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "ConfigMapError",
			Message: err.Error(),
		})
		_ = r.Status().Update(ctx, net)
		return ctrl.Result{}, err
	}

	//  Ensure NetworkAttachmentDefinition for this network.
	if err := r.ensureNAD(ctx, net, resourceName); err != nil {
		meta.SetStatusCondition(&net.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "NADError",
			Message: err.Error(),
		})
		_ = r.Status().Update(ctx, net)
		return ctrl.Result{}, err
	}

	net.Status.ResourceName = resourceName
	net.Status.SelectedVFs = selectedVFs
	meta.SetStatusCondition(&net.Status.Conditions, metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionTrue,
		Reason:  "ComponentsReady",
		Message: "ConfigMap and NAD ensured",
	})

	if err := r.Status().Update(ctx, net); err != nil {
		logger.Error(err, "Failed to update DpuNetwork status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *DpuNetworkReconciler) ensureDevicePluginConfigMap(ctx context.Context, net *configv1.DpuNetwork, resourceName string, vfRanges []string) error {
	cm := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: dpuDevicePluginConfigMapName, Namespace: vars.Namespace}
	err := r.Get(ctx, key, cm)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if apierrors.IsNotFound(err) {
		cm = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}}
	}

	// Build config.json payload. For now, we append one resource entry per DpuNetwork.
	cfg := dpuDevicePluginConfig{}
	if cm.Data != nil {
		if raw := cm.Data["config.json"]; raw != "" {
			if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
				return fmt.Errorf("failed to parse existing ConfigMap config.json: %w", err)
			}
		}
	}

	// Upsert entry for this DpuNetwork.
	newEntry := dpuDevicePluginResource{
		ResourceName:   resourceName,
		DpuNetworkName: net.Name,
		NodeSelector:   net.Spec.NodeSelector,
		VfRanges:       vfRanges,
		IsAccelerated:  net.Spec.IsAccelerated,
	}
	var out []dpuDevicePluginResource
	for _, e := range cfg.Resources {
		if e.DpuNetworkName == net.Name {
			continue
		}
		out = append(out, e)
	}
	out = append(out, newEntry)
	// stable ordering
	sort.Slice(out, func(i, j int) bool { return out[i].DpuNetworkName < out[j].DpuNetworkName })
	cfg.Resources = out

	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data["config.json"] = string(payload)

	// Owner reference: the ConfigMap is shared across all DpuNetwork CRs, so we do NOT set controller reference.
	// (Multiple controller references would be invalid.)

	if cm.CreationTimestamp.IsZero() {
		return r.Create(ctx, cm)
	}
	return r.Update(ctx, cm)
}

func (r *DpuNetworkReconciler) removeFromDevicePluginConfigMap(ctx context.Context, networkName string) error {
	cm := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: dpuDevicePluginConfigMapName, Namespace: vars.Namespace}
	if err := r.Get(ctx, key, cm); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	cfg := dpuDevicePluginConfig{}
	if cm.Data != nil {
		if raw := cm.Data["config.json"]; raw != "" {
			if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
				return fmt.Errorf("failed to parse existing ConfigMap config.json: %w", err)
			}
		}
	}

	var out []dpuDevicePluginResource
	for _, e := range cfg.Resources {
		if e.DpuNetworkName == networkName {
			continue
		}
		out = append(out, e)
	}

	if len(out) == len(cfg.Resources) {
		return nil
	}

	cfg.Resources = out
	if len(out) == 0 {
		// All networks removed; delete the ConfigMap so the daemon
		// transitions back to the default device plugin.
		return r.Delete(ctx, cm)
	}

	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	cm.Data["config.json"] = string(payload)
	return r.Update(ctx, cm)
}

func (r *DpuNetworkReconciler) ensureNAD(ctx context.Context, net *configv1.DpuNetwork, resourceName string) error {
	bridgeID := stableBridgeID(net.Name)
	nadName := fmt.Sprintf("%s-nad", net.Name)
	key := types.NamespacedName{Name: nadName, Namespace: defaultDpuNetworkNADNamespace}

	nad := &netattdefv1.NetworkAttachmentDefinition{}
	err := r.Get(ctx, key, nad)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if apierrors.IsNotFound(err) {
		nad = &netattdefv1.NetworkAttachmentDefinition{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}}
	}

	if nad.Annotations == nil {
		nad.Annotations = map[string]string{}
	}
	nad.Annotations["dpu.config.openshift.io/dpu-network"] = net.Name
	nad.Annotations["k8s.v1.cni.cncf.io/resourceName"] = resourceName

	// Keep this JSON minimal for now; the dpu-cni and NRI integration can evolve.
	nad.Spec.Config = fmt.Sprintf(`{"type":"dpu-cni","cniVersion":"0.4.0","name":"dpu-cni","BridgeID":"%s"}`, bridgeID)

	if nad.CreationTimestamp.IsZero() {
		return r.Create(ctx, nad)
	}
	return r.Update(ctx, nad)
}

func (r *DpuNetworkReconciler) deleteNAD(ctx context.Context, networkName string) error {
	nadName := fmt.Sprintf("%s-nad", networkName)
	nad := &netattdefv1.NetworkAttachmentDefinition{}
	key := types.NamespacedName{Name: nadName, Namespace: defaultDpuNetworkNADNamespace}
	if err := r.Get(ctx, key, nad); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return r.Delete(ctx, nad)
}

func stableBridgeID(name string) string {
	sum := sha256.Sum256([]byte(name))
	// keep short but collision-resistant enough for demo purposes
	return hex.EncodeToString(sum[:])[:8]
}

func parseVfRangesFromSelector(sel *metav1.LabelSelector) ([]int32, []string) {
	if sel == nil {
		return nil, nil
	}

	var ranges []string
	for _, expr := range sel.MatchExpressions {
		if expr.Key != "vfId" {
			continue
		}
		if strings.ToLower(string(expr.Operator)) != "in" {
			// only handle In for now
			continue
		}
		for _, v := range expr.Values {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			ranges = append(ranges, v)
		}
	}

	vfSet := map[int32]struct{}{}
	for _, r := range ranges {
		for _, id := range expandRange(r) {
			vfSet[id] = struct{}{}
		}
	}

	var vfs []int32
	for id := range vfSet {
		vfs = append(vfs, id)
	}
	sort.Slice(vfs, func(i, j int) bool { return vfs[i] < vfs[j] })
	return vfs, ranges
}

func expandRange(s string) []int32 {
	// supports "N" or "A-B"
	if !strings.Contains(s, "-") {
		v, err := strconv.Atoi(s)
		if err != nil {
			return nil
		}
		return []int32{int32(v)}
	}
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return nil
	}
	start, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	end, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return nil
	}
	if end < start {
		start, end = end, start
	}
	out := make([]int32, 0, end-start+1)
	for i := start; i <= end; i++ {
		out = append(out, int32(i))
	}
	return out
}

// SetupWithManager sets up the controller with the Manager.
func (r *DpuNetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1.DpuNetwork{}).
		Complete(r)
}
