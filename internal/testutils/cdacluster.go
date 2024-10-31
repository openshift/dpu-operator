package testutils

import (
	"os"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type CdaCluster struct {
	Name           string
	HostConfigPath string
	DpuConfigPath  string
}

func (t *CdaCluster) EnsureExists() (*rest.Config, *rest.Config, error) {
	hostConfig, err := t.createClient("/root/kubeconfig.ocpcluster")
	if err != nil {
		return nil, nil, err
	}
	dpuConfig, err := t.createClient("/root/kubeconfig.microshift")
	if err != nil {
		return nil, nil, err
	}
	return hostConfig, dpuConfig, nil
}

func (t *CdaCluster) createClient(kubeconfigPath string) (*rest.Config, error) {
	kubeconfig, err := readToByte(kubeconfigPath)
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

func readToByte(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	stats, err := f.Stat()
	if err != nil {
		return nil, err
	}

	cfg := make([]byte, stats.Size())
	_, err = f.Read(cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}
