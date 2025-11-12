package main

import (
	"fmt"
	"os"

	"github.com/openshift/dpu-operator/internal/testutils"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	setupLog = ctrl.Log.WithName("basic_setup")
)

func main() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	setupLog.Info("DPU Operator Basic Setup Tool")

	var validKubeconfigs []struct {
		name string
		path string
	}

	hostKubeconfig := os.Getenv("KUBECONFIG_HOST")
	if hostKubeconfig == "" {
		setupLog.Error(fmt.Errorf("KUBECONFIG_HOST not set"), "must specify at least KUBECONFIG_HOST")
		os.Exit(1)
	}

	if _, err := os.Stat(hostKubeconfig); err != nil {
		setupLog.Error(err, "KUBECONFIG_HOST specified but file not found", "path", hostKubeconfig)
		os.Exit(1)
	}
	validKubeconfigs = append(validKubeconfigs, struct {
		name string
		path string
	}{"host", hostKubeconfig})

	if dpuKubeconfig := os.Getenv("KUBECONFIG_DPU"); dpuKubeconfig != "" {
		if _, err := os.Stat(dpuKubeconfig); err != nil {
			setupLog.Error(err, "KUBECONFIG_DPU specified but file not found", "path", dpuKubeconfig)
			os.Exit(1)
		}
		validKubeconfigs = append(validKubeconfigs, struct {
			name string
			path string
		}{"dpu", dpuKubeconfig})
	}

	setupLog.Info(fmt.Sprintf("Running in %d cluster mode", len(validKubeconfigs)))

	type clusterInfo struct {
		name     string
		crClient client.Client
	}
	var clusters []clusterInfo

	setupLog.Info("Configuring DpuOperator on all clusters")
	for _, kubeconfig := range validKubeconfigs {
		setupLog.Info("Configuring DpuOperator", "cluster", kubeconfig.name, "path", kubeconfig.path)

		cluster := &testutils.CdaCluster{
			Name:           kubeconfig.name,
			KubeconfigPath: kubeconfig.path,
		}

		restConfig, err := cluster.EnsureExists(kubeconfig.path)
		if err != nil {
			setupLog.Error(err, "Failed to connect to cluster", "cluster", kubeconfig.name)
			os.Exit(1)
		}

		crClient, _, err := testutils.CreateClientsFromConfig(restConfig)
		if err != nil {
			setupLog.Error(err, "Failed to create clients", "cluster", kubeconfig.name)
			os.Exit(1)
		}

		if err := testutils.ConfigureDpuOperator(crClient, ""); err != nil {
			setupLog.Error(err, "Failed to configure DpuOperator", "cluster", kubeconfig.name)
			os.Exit(1)
		}

		clusters = append(clusters, clusterInfo{
			name:     kubeconfig.name,
			crClient: crClient,
		})

		setupLog.Info("DpuOperator configuration completed", "cluster", kubeconfig.name)
	}

	setupLog.Info("Waiting for DPU Ready on all clusters")
	for _, cluster := range clusters {
		setupLog.Info("Waiting for DPU Ready", "cluster", cluster.name)

		if err := testutils.WaitForDPUReady(cluster.crClient); err != nil {
			setupLog.Error(err, "Failed waiting for DPU Ready", "cluster", cluster.name)
			os.Exit(1)
		}

		setupLog.Info("DPU Ready achieved", "cluster", cluster.name)
	}

	setupLog.Info("Setup completed successfully!")
}
