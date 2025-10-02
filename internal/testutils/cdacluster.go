package testutils

import (
	"fmt"
	"os"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type CdaCluster struct {
}

func (t *CdaCluster) EnsureExists(kubeconfigPath string) (*rest.Config, error) {
	if _, err := os.Stat(kubeconfigPath); err == nil {
		return t.createClient(kubeconfigPath)
	}
	return nil, fmt.Errorf("kubeconfig %s not found", kubeconfigPath)
}

func (t *CdaCluster) createClient(kubeconfigPath string) (*rest.Config, error) {
	kubeconfig, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	config, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
	if err != nil {
		return nil, err
	}
	restCfg, err := config.ClientConfig()
	if err != nil {
		return nil, err
	}
	return restCfg, nil
}
