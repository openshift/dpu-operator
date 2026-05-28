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
	"time"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/dpu-operator/api/v1"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/openshift/dpu-operator/internal/dpuprovider"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// Requeue intervals for different states.
	requeueWaitingForPlugin = 5 * time.Second
	requeueWaitingForReboot = 10 * time.Second
	rebootTimeoutDuration   = 300 * time.Second
	firmwareUpgradeTimeout  = 1200 * time.Second // 20 minutes for firmware flashing
)

// DataProcessingUnitConfigReconciler reconciles a DataProcessingUnitConfig object.
// NOTE: This reconciler runs inside the DPU daemon pod (DaemonSet), not in the
// central operator pod, because it needs direct access to the local Daemon and
// its vendor-specific gRPC plugins.
type DataProcessingUnitConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Provider dpuprovider.DpuProvider
	log      logr.Logger
}

func NewDataProcessingUnitConfigReconciler(client client.Client, scheme *runtime.Scheme, provider dpuprovider.DpuProvider) *DataProcessingUnitConfigReconciler {
	return &DataProcessingUnitConfigReconciler{
		Client:   client,
		Scheme:   scheme,
		Provider: provider,
	}
}

// +kubebuilder:rbac:groups=config.openshift.io,resources=dataprocessingunitconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.openshift.io,resources=dataprocessingunitconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=config.openshift.io,resources=dataprocessingunitconfigs/finalizers,verbs=update

// Reconcile implements the main reconciliation loop. It is designed to be
// short-lived and idempotent — long-running operations like waiting for a
// reboot are handled by re-queuing with a delay rather than blocking.
func (r *DataProcessingUnitConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling DataProcessingUnitConfig", "name", req.Name)

	// 1. Fetch the DataProcessingUnitConfig
	dpuConfig := &configv1.DataProcessingUnitConfig{}
	if err := r.Get(ctx, req.NamespacedName, dpuConfig); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("DataProcessingUnitConfig not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// 2. Nothing to do if no operation is requested
	if dpuConfig.Spec.DpuManagement.Operation == configv1.DpuOpNone ||
		dpuConfig.Spec.DpuManagement.Operation == "" {
		return ctrl.Result{}, nil
	}

	// 3. If the operation already succeeded or was cancelled, do nothing
	phase := dpuConfig.Status.NodeStatus.Phase
	if phase == configv1.DpuPhaseSucceeded || phase == configv1.DpuPhaseCancelled {
		return ctrl.Result{}, nil
	}

	// 4. Match this config to a local DPU
	dpuIdentifier, err := r.matchDpuForConfig(dpuConfig)
	if err != nil {
		logger.V(1).Info("No matching local DPU found for config", "config", dpuConfig.Name, "reason", err)
		return ctrl.Result{}, nil // Not for this node
	}

	// 5. Get the gRPC plugin for this DPU
	vsp := r.Provider.GetSpecificDpuPlugin(dpuIdentifier)
	if vsp == nil {
		logger.Info("VSP plugin not ready yet, requeuing", "identifier", dpuIdentifier)
		return ctrl.Result{RequeueAfter: requeueWaitingForPlugin}, nil
	}
	// 6. Dispatch based on the current phase (state machine)
	//
	// Reboot only:            Pending → Rebooting → Succeeded
	// Firmware upgrade:       Pending → Running (flashing) → Rebooting → Succeeded
	//
	switch phase {
	case "", configv1.DpuPhasePending:
		return r.handlePending(ctx, logger, dpuConfig, dpuIdentifier, vsp)

	case configv1.DpuPhaseRunning:
		// Running means firmware upgrade gRPC was sent and we are waiting
		// for the VSP to confirm flashing is done, then auto-reboot.
		return r.handleRunning(ctx, logger, dpuConfig, dpuIdentifier, vsp)

	case configv1.DpuPhaseRebooting:
		return r.handleRebooting(ctx, logger, dpuConfig, dpuIdentifier, vsp)

	case configv1.DpuPhaseFailed:
		// Stay failed — user must reset operation to "None" and then set a new one
		logger.Info("Operation previously failed, no action taken", "config", dpuConfig.Name)
		return ctrl.Result{}, nil

	default:
		logger.Info("Unknown phase, ignoring", "phase", phase)
		return ctrl.Result{}, nil
	}
}

// handlePending starts the requested operation.
//   - Reboot:          send RebootDpu gRPC → phase = Rebooting
//   - FirmwareUpgrade: send UpgradeFirmware gRPC (synchronous, OAM returns
//     success when flashing is done) → send RebootDpu → phase = Rebooting
func (r *DataProcessingUnitConfigReconciler) handlePending(
	ctx context.Context,
	logger logr.Logger,
	dpuConfig *configv1.DataProcessingUnitConfig,
	dpuIdentifier string,
	vsp dpuprovider.DpuVSP,
) (ctrl.Result, error) {

	// Pre-flight health check via Ping
	_, err := vsp.Ping(ctx)
	if err != nil {
		logger.Info("DPU not reachable before starting operation", "error", err)
		r.setHealthStatus(dpuConfig, configv1.HealthStatusUnhealthy, "DPU not reachable before operation: "+err.Error())
		dpuConfig.Status.NodeStatus.Phase = configv1.DpuPhaseFailed
		dpuConfig.Status.NodeStatus.ErrorMessage = "DPU unhealthy before operation"
		dpuConfig.Status.NodeStatus.CompletionTime = timeNow()
		if updateErr := r.Status().Update(ctx, dpuConfig); updateErr != nil {
			logger.Error(updateErr, "Failed to update status")
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil
	}

	op := dpuConfig.Spec.DpuManagement.Operation
	now := timeNow()

	switch op {
	case configv1.DpuOpReboot:
		// Send reboot command and go straight to Rebooting phase.
		req := &pb.DPURebootRequest{}
		_, err := vsp.RebootDpu(ctx, req)
		if err != nil {
			return r.failOperation(ctx, logger, dpuConfig, "Failed to send reboot command: "+err.Error())
		}
		logger.Info("Reboot command sent successfully", "dpuIdentifier", dpuIdentifier)

		dpuConfig.Status.NodeStatus.Phase = configv1.DpuPhaseRebooting
		dpuConfig.Status.NodeStatus.SubOperation = configv1.DpuOpReboot
		dpuConfig.Status.NodeStatus.StartTime = now
		dpuConfig.Status.NodeStatus.CompletionTime = nil
		dpuConfig.Status.NodeStatus.ErrorMessage = ""
		dpuConfig.Status.NodeStatus.Message = "Reboot command sent, waiting for DPU to come back online"
		meta.SetStatusCondition(&dpuConfig.Status.Conditions, metav1.Condition{
			Type:               configv1.ConditionTypeReady,
			Status:             metav1.ConditionUnknown,
			Reason:             "Rebooting",
			Message:            "Reboot in progress",
			ObservedGeneration: dpuConfig.Generation,
		})

	case configv1.DpuOpFirmwareUpgrade:
		if dpuConfig.Spec.DpuManagement.Firmware == nil {
			return r.failOperation(ctx, logger, dpuConfig, "FirmwareUpgrade requested but firmware spec is nil")
		}

		// Step 1: Send firmware upgrade gRPC.
		// This call is synchronous — OAM returns success when flashing is done.
		fwReq := &pb.DPUFirmwareUpgradeRequest{
			FirmwareType:      string(dpuConfig.Spec.DpuManagement.Firmware.Type),
			FirmwareImagePath: dpuConfig.Spec.DpuManagement.Firmware.FirmwarePath,
		}
		fwCtx, fwCancel := context.WithTimeout(ctx, firmwareUpgradeTimeout)
		defer fwCancel()
		resp, err := vsp.UpgradeFirmware(fwCtx, fwReq)
		if err != nil {
			return r.failOperation(ctx, logger, dpuConfig, "Firmware upgrade failed: "+err.Error())
		}
		if resp != nil && !resp.Success {
			return r.failOperation(ctx, logger, dpuConfig, "Firmware upgrade rejected by OAM: "+resp.GetMessage())
		}
		logger.Info("Firmware upgrade completed (flashing done), triggering reboot for activation",
			"dpuIdentifier", dpuIdentifier, "response", resp.GetMessage())

		// Step 2: Firmware flashing done — reboot to activate.
		rebootReq := &pb.DPURebootRequest{}
		_, err = vsp.RebootDpu(ctx, rebootReq)
		if err != nil {
			return r.failOperation(ctx, logger, dpuConfig,
				"Firmware flashing succeeded but failed to send reboot command: "+err.Error())
		}
		logger.Info("Post-upgrade reboot command sent", "dpuIdentifier", dpuIdentifier)

		dpuConfig.Status.NodeStatus.Phase = configv1.DpuPhaseRebooting
		dpuConfig.Status.NodeStatus.SubOperation = configv1.DpuOpFirmwareUpgrade
		dpuConfig.Status.NodeStatus.FirmwareType = dpuConfig.Spec.DpuManagement.Firmware.Type
		dpuConfig.Status.NodeStatus.TargetVersion = dpuConfig.Spec.DpuManagement.Firmware.TargetVersion
		dpuConfig.Status.NodeStatus.StartTime = now
		dpuConfig.Status.NodeStatus.CompletionTime = nil
		dpuConfig.Status.NodeStatus.ErrorMessage = ""
		dpuConfig.Status.NodeStatus.Message = "Firmware flashing done, rebooting to activate new firmware"
		meta.SetStatusCondition(&dpuConfig.Status.Conditions, metav1.Condition{
			Type:               configv1.ConditionTypeReady,
			Status:             metav1.ConditionUnknown,
			Reason:             "Rebooting",
			Message:            "Firmware upgrade rebooting to activate",
			ObservedGeneration: dpuConfig.Generation,
		})

	default:
		logger.Info("Unknown operation type, ignoring", "operation", op)
		return ctrl.Result{}, nil
	}

	if err := r.Status().Update(ctx, dpuConfig); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: requeueWaitingForReboot}, nil
}

// handleRunning is currently unused because both Reboot and FirmwareUpgrade
// go directly to Rebooting from Pending. It is kept as a placeholder in case
// a future operation needs an intermediate "in-progress" polling phase
// (e.g., async firmware upload with progress reporting).
func (r *DataProcessingUnitConfigReconciler) handleRunning(
	ctx context.Context,
	logger logr.Logger,
	dpuConfig *configv1.DataProcessingUnitConfig,
	dpuIdentifier string,
	vsp dpuprovider.DpuVSP,
) (ctrl.Result, error) {
	logger.Info("Unexpected Running phase — current design skips Running", "config", dpuConfig.Name)
	return r.failOperation(ctx, logger, dpuConfig, "Unexpected Running phase; expected Rebooting")
}

// handleRebooting polls until the DPU comes back online after a reboot.
// This phase is entered after:
//   - a standalone Reboot operation, or
//   - a FirmwareUpgrade where flashing succeeded and a reboot was auto-triggered.
func (r *DataProcessingUnitConfigReconciler) handleRebooting(
	ctx context.Context,
	logger logr.Logger,
	dpuConfig *configv1.DataProcessingUnitConfig,
	dpuIdentifier string,
	vsp dpuprovider.DpuVSP,
) (ctrl.Result, error) {

	// Check for timeout
	if dpuConfig.Status.NodeStatus.StartTime != nil {
		elapsed := time.Since(dpuConfig.Status.NodeStatus.StartTime.Time)
		if elapsed > rebootTimeoutDuration {
			msg := fmt.Sprintf("Reboot timed out after %v for DPU %s", elapsed, dpuIdentifier)
			return r.failOperation(ctx, logger, dpuConfig, msg)
		}
	}

	// Poll: is the DPU back online?
	_, err := vsp.Ping(ctx)
	if err != nil {
		logger.V(2).Info("DPU not yet online after reboot, will requeue",
			"identifier", dpuIdentifier, "error", err)
		return ctrl.Result{RequeueAfter: requeueWaitingForReboot}, nil
	}

	// DPU is back online — build the success message based on what was requested.
	op := dpuConfig.Status.NodeStatus.SubOperation
	var message string
	switch op {
	case configv1.DpuOpFirmwareUpgrade:
		message = fmt.Sprintf("Firmware upgrade completed and DPU rebooted successfully (type=%s, target=%s)",
			dpuConfig.Status.NodeStatus.FirmwareType,
			dpuConfig.Status.NodeStatus.TargetVersion)
	case configv1.DpuOpReboot:
		message = "DPU reboot completed successfully"
	default:
		message = "DPU is back online"
	}

	return r.succeedOperation(ctx, logger, dpuConfig, message)
}

// succeedOperation marks the operation as succeeded and updates health.
func (r *DataProcessingUnitConfigReconciler) succeedOperation(
	ctx context.Context, logger logr.Logger,
	dpuConfig *configv1.DataProcessingUnitConfig, message string,
) (ctrl.Result, error) {
	dpuConfig.Status.NodeStatus.Phase = configv1.DpuPhaseSucceeded
	dpuConfig.Status.NodeStatus.CompletionTime = timeNow()
	dpuConfig.Status.NodeStatus.Message = message
	dpuConfig.Status.NodeStatus.ErrorMessage = ""
	r.setHealthStatus(dpuConfig, configv1.HealthStatusHealthy, message)
	meta.SetStatusCondition(&dpuConfig.Status.Conditions, metav1.Condition{
		Type:               configv1.ConditionTypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Succeeded",
		Message:            message,
		ObservedGeneration: dpuConfig.Generation,
	})

	if err := r.Status().Update(ctx, dpuConfig); err != nil {
		logger.Error(err, "Failed to update status to Succeeded")
		return ctrl.Result{}, err
	}
	logger.Info("Operation succeeded", "config", dpuConfig.Name, "message", message)
	return ctrl.Result{}, nil
}

// failOperation marks the operation as failed and updates health.
func (r *DataProcessingUnitConfigReconciler) failOperation(
	ctx context.Context, logger logr.Logger,
	dpuConfig *configv1.DataProcessingUnitConfig, errMsg string,
) (ctrl.Result, error) {
	dpuConfig.Status.NodeStatus.Phase = configv1.DpuPhaseFailed
	dpuConfig.Status.NodeStatus.CompletionTime = timeNow()
	dpuConfig.Status.NodeStatus.ErrorMessage = errMsg
	r.setHealthStatus(dpuConfig, configv1.HealthStatusUnhealthy, errMsg)
	meta.SetStatusCondition(&dpuConfig.Status.Conditions, metav1.Condition{
		Type:               configv1.ConditionTypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             "Failed",
		Message:            errMsg,
		ObservedGeneration: dpuConfig.Generation,
	})

	if err := r.Status().Update(ctx, dpuConfig); err != nil {
		logger.Error(err, "Failed to update status to Failed")
		return ctrl.Result{}, err
	}
	logger.Info("Operation failed", "config", dpuConfig.Name, "error", errMsg)
	return ctrl.Result{}, fmt.Errorf("%s", errMsg)
}

// setHealthStatus is a helper to update the health sub-status on the config.
func (r *DataProcessingUnitConfigReconciler) setHealthStatus(
	dpuConfig *configv1.DataProcessingUnitConfig,
	status configv1.DpuHealthStatus,
	message string,
) {
	dpuConfig.Status.Health.Status = status
	dpuConfig.Status.Health.Message = message
	dpuConfig.Status.Health.LastProbeTime = timeNow()
}

// matchDpuForConfig finds the local ManagedDpu whose labels match the
// config's DpuSelector. Returns the DPU identifier string.
func (r *DataProcessingUnitConfigReconciler) matchDpuForConfig(dpuConfig *configv1.DataProcessingUnitConfig) (string, error) {
	if dpuConfig.Spec.DpuSelector == nil {
		return "", fmt.Errorf("config %s has no DpuSelector", dpuConfig.Name)
	}

	selector, err := metav1.LabelSelectorAsSelector(dpuConfig.Spec.DpuSelector)
	if err != nil {
		return "", fmt.Errorf("failed to parse label selector for config %s: %v", dpuConfig.Name, err)
	}

	managedDpus := r.Provider.GetManagedDpus()
	for identifier, info := range managedDpus {
		dpuLabels := info.Labels
		if dpuLabels == nil {
			dpuLabels = map[string]string{}
		}
		// Ensure the DPU name label is present for matching
		if _, exists := dpuLabels["dpu-name"]; !exists {
			dpuLabels["dpu-name"] = identifier
		}
		if selector.Matches(labels.Set(dpuLabels)) {
			return identifier, nil
		}
	}

	return "", fmt.Errorf("no local DPU matches selector for config %s", dpuConfig.Name)
}

// timeNow returns a pointer to the current time as *metav1.Time.
func timeNow() *metav1.Time {
	t := metav1.Now()
	return &t
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataProcessingUnitConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.log = mgr.GetLogger().WithName("DataProcessingUnitConfigReconciler")

	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1.DataProcessingUnitConfig{}).
		Complete(r)
}
