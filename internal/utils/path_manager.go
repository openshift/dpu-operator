package utils

import (
	"path/filepath"
)

type PathManager struct {
	rootDir string
}

func NewPathManager(rootDir string) *PathManager {
	return &PathManager{rootDir: rootDir}
}

func (p *PathManager) CNIServerPath() string {
	DaemonBaseDir := "/var/run/dpu-daemon/"
	ServerSocketPath := DaemonBaseDir + "dpu-cni/dpu-cni-server.sock"

	return filepath.Join(p.rootDir, ServerSocketPath)
}

func (p *PathManager) KubeletEndPoint() string {
	// KubeEndPoint is kubelet socket name
	KubeEndPoint := "kubelet.sock"
	// SockDir is the default Kubelet device plugin socket directory
	// SockDir = "/var/lib/kubelet/plugins_registry"
	// DeprecatedSockDir is the deprecated Kubelet device plugin socket directory
	DeprecatedSockDir := "/var/lib/kubelet/device-plugins"
	return filepath.Join(p.rootDir, DeprecatedSockDir, KubeEndPoint)
}

func (p *PathManager) PluginEndpoint() string {
	pluginEndpoint := "sriovNet.sock"
	pluginMountPath := "/var/lib/kubelet/device-plugins"
	return filepath.Join(p.rootDir, pluginMountPath, pluginEndpoint)
}

func (p *PathManager) DefaultCNIPATH() string {
	return filepath.Join(p.rootDir, "cni")
}
