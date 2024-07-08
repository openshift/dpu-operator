package utils

import (
	"fmt"
	"path/filepath"
)

type PathManager struct {
	rootDir string
}

const (
	PluginEndpointSocket = "sriovNet.sock"
)

func NewPathManager(rootDir string) *PathManager {
	return &PathManager{rootDir: rootDir}
}

func (p *PathManager) CNIServerPath() string {
	return p.wrap("/var/run/dpu-daemon/dpu-cni/dpu-cni-server.sock")
}

func (p *PathManager) KubeletEndPoint() string {
	// The following path uses the deprecated Kubelet device plugin socket directory
	return p.wrap("/var/lib/kubelet/device-plugins/kubelet.sock")
}

func (p *PathManager) PluginEndpoint() string {
	return p.wrap(filepath.Join("/var/lib/kubelet/device-plugins", PluginEndpointSocket))
}

func (p *PathManager) CniPath(flavour Flavour) (string, error) {
	// Some k8s cluster flavours use /var/lib (in the case of RHCOS based)
	// and some use /opt (in the case of RHEL based)
	switch flavour {
	case MicroShiftFlavour:
		return p.wrap("/opt/cni/bin/dpu-cni"), nil
	case OpenShiftFlavour:
		return p.wrap("/var/lib/cni/bin/dpu-cni"), nil
	default:
		return "", fmt.Errorf("unknown flavour")
	}
}

func (p *PathManager) wrap(path string) string {
	return filepath.Join(p.rootDir, path)
}
