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
	"github.com/openshift/dpu-operator/internal/daemon"
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
	PciAddr string
	Daemon *daemon.Daemon
}

func NewDataProcessingUnitConfigReconciler(client client.Client, scheme *runtime.Scheme) *DataProcessingUnitConfigReconciler {
	return &DataProcessingUnitConfigReconciler{
		Client:               client,
		Scheme:               scheme
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

    dpuConfigList := &configv1.DataProcessingUnitConfigList{}
    if err := r.List(ctx, dpuConfigList); err != nil {
        if errors.IsNotFound(err) {
            logger.Info("DataProcessingUnitConfig not found. Ignoring.")
            return ctrl.Result{}, nil
        }
        logger.Error(err, "Failed to get DataProcessingUnitConfig")
        return ctrl.Result{}, err
    }

    //only support 1 DPU
    dpuFound, matchedDPUsIdentifier, matchedDpuConfig := r.IsLabelsMatched(dpuConfigList)

	if dpuFound {
        vsp := r.Daemon.GetSpecificDpuPlugin(matchedDPUsIdentifier)
		if vsp ==nil {
			mu.lock
			return ctrl.Result{RequeueAfter: 5*time.Second}, nil
		}

		managedDpu := r.Daemon.GetManagedDpusByIdentifier(matchedDPUsIdentifier)
		if managedDpu == nil {
			logger.Error(nil, "Managed DPU not found", "identifier", matchedDPUsIdentifier)
			return r.updateHealthStatus(ctx, matchedDpuConfig, configv1.HealthStatusUnknown, 
				"Managed DPU not found")
		}
        
		if !vsp.CheckPing() {
			logger.Info("DPU became unhealthy before operation execution")
        
			// 更新健康状态
			r.updateHealthStatus(ctx, dpuConfig, configv1.HealthStatusUnhealthy, 
				"DPU became unhealthy before operation execution")

			 // 更新操作状态为失败
			matchedDpuConfig.Status.NodeStatus.Phase = configv1.DpuPhaseFailed
			matchedDpuConfig.Status.NodeStatus.ErrorMessage = "DPU became unhealthy before operation"
			r.Status().Update(ctx, matchedDpuConfig)
			
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		// 更新操作状态为 Running
		dpuConfig.Status.NodeStatus.Phase = configv1.DpuPhaseRunning
		dpuConfig.Status.NodeStatus.SubOperation = dpuConfig.Spec.DpuManagement.Operation
		dpuConfig.Status.NodeStatus.StartTime = &metav1.Time{Time: time.Now()}
		if err := r.Status().Update(ctx, dpuConfig); err != nil {
			logger.Error(err, "Failed to update status to Running")
		}

		if matchedDpuConfig.Spec.DpuManagement.Operation == DpuOpRestart {
			pciAddr := extractPCIAddress(matchedDPUsIdentifier)
			req := &pb.DPUManagementRequest{
				PciAddress:        pciAddr,
			}

			res,err := vsp.RebootDpu(req)
			// 此时 Ping 应该会失败，因为 DPU 正在重启
			logger.Info("Reboot command sent, waiting for DPU to go down and come back")
			
			// 4. 等待 DPU 重新上线
			timeout := time.After(120 * time.Second)  // 给足够长的重启时间
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			
			var rebootSuccessful bool
			
			for {
				select {
				case <-timeout:
					// 超时未恢复
					dpuConfig.Status.NodeStatus.Phase = configv1.DpuPhaseFailed
					dpuConfig.Status.NodeStatus.ErrorMessage = "Reboot timeout: DPU did not come back online"
					dpuConfig.Status.NodeStatus.CompletionTime = &metav1.Time{Time: time.Now()}
					r.Status().Update(ctx, dpuConfig)
					
					return ctrl.Result{RequeueAfter: 60 * time.Second}, 
						fmt.Errorf("reboot timeout for DPU %s", identifier)
					
				case <-ticker.C:
					// 检查 DPU 是否重新上线
					if vsp.CheckPing() {
						// Ping 成功，DPU 已恢复
						rebootSuccessful = true
						
						dpuConfig.Status.NodeStatus.Phase = configv1.DpuPhaseSucceeded
						dpuConfig.Status.NodeStatus.CompletionTime = &metav1.Time{Time: time.Now()}
						dpuConfig.Status.NodeStatus.Message = "DPU reboot completed successfully"
						r.Status().Update(ctx, dpuConfig)
						
						// 更新健康状态为健康
						r.updateHealthStatus(ctx, dpuConfig, configv1.HealthStatusHealthy, 
							"DPU successfully rebooted and responding to pings")
						
						logger.Info("DPU reboot completed and confirmed via ping")
						
						return ctrl.Result{}, nil
					}
					
					logger.V(2).Info("Still waiting for DPU to come online after reboot...")
				}
			}
		}
		else if matchedDpuConfig.Spec.DpuManagement.Operation == DpuOpFirmwareUpgrade {
			// 固件升级逻辑
			pciAddr := extractPCIAddress(matchedDPUsIdentifier)
			req := &pb.DPUManagementRequest{
				PciAddress:        pciAddr,
				FirmwareType:      string(matchedDpuConfig.Spec.DpuManagement.Firmware.Type),
				FirmwareImagePath: matchedDpuConfig.Spec.DpuManagement.Firmware.FirmwarePath,
			}
			vsp.UpgradeFirmware(req) 
		}

	}
    
    return ctrl.Result{}, nil
}

func (r *DataProcessingUnitConfigReconciler) IsLabelsMatched(dpuConfigList *configv1.DataProcessingUnitConfig) (bool, string, *configv1.DataProcessingUnitConfig) {
    // 获取本地 DPU CRs，并将名称作为标签
    localDpuCRs := make(map[string]*configv1.DataProcessingUnit)
    for identifier, managedDpu := range r.Daemon.GetManagedDpus() {
        // 创建 DPU CR 的深拷贝
        dpuCrCopy := managedDpu.DpuCR.DeepCopy()
        
        // 确保 DPU CR 有包含其名称的标签
        if dpuCrCopy.Labels == nil {
            dpuCrCopy.Labels = make(map[string]string)
        }

        if _, exists := dpuCrCopy.Labels["dpu-name"]; !exists {
            dpuCrCopy.Labels["dpu-name"] = identifier
            logger.Info("Added dpu-name label to DPU CR", "dpuName", identifier)
        }
        
        localDpuCRs[identifier] = dpuCrCopy
    }
    
    // 为每个 Config 找到匹配的 DPU
    for _, dpuConfig := range dpuConfigList.Items {
        if dpuConfig.Spec.DpuSelector == nil {
            logger.Info("DPU Config has no selector, skipping", "configName", dpuConfig.Name)
            continue
        }
        
        // 将 LabelSelector 转换为 Selector
        selector, err := metav1.LabelSelectorAsSelector(dpuConfig.Spec.DpuSelector)
        if err != nil {
            logger.Error(err, "Failed to parse label selector", 
                "configName", dpuConfig.Name)
            continue
        }
        
        // 找出匹配这个 selector 的本地 DPU
        for identifier, dpuCR := range localDpuCRs {
            // 检查 DPU 的标签是否匹配 selector
            if selector.Matches(labels.Set(dpuCR.Labels)) {
                logger.Info("Found matching DPU for config", 
                    "configName", dpuConfig.Name,
                    "identifier", identifier,
                    "dpuLabels", dpuCR.Labels)

                // 返回匹配的 DPU Config（注意这里返回的是指针）
                return true, identifier, &dpuConfig
            }
        }
    }

    return false, "", nil
}

func extractPCIAddress(identifier string) string {
    parts := strings.Split(identifier, "-")
    
    // 对于 "SynaXG-dpu-0000-65-00-0-host"
    // 索引: 0:SynaXG, 1:dpu, 2:0000, 3:65, 4:00, 5:0, 6:host
    
    if len(parts) == 7 {
        // 取索引 2-5 的部分
        pciParts := parts[2:6] // [0000, 65, 00, 0]
        
        return fmt.Sprintf("%s:%s:%s.%s", 
            pciParts[0], 
            pciParts[1], 
            pciParts[2], 
            pciParts[3])
    }
    return ""
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

 