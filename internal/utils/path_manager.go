package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"k8s.io/klog/v2"
)

type PathManager struct {
	rootDir string
}

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
	return p.wrap("/var/lib/kubelet/device-plugins/dpuNet.sock")
}

func (p *PathManager) PluginEndpointFilename() string {
	return filepath.Base(p.PluginEndpoint())
}

func (p *PathManager) CniPath(flavour Flavour) (string, error) {
	// Some k8s cluster flavours use /var/lib (in the case of RHCOS based)
	// and some use /opt (in the case of RHEL based)
	switch flavour {
	case MicroShiftFlavour:
		return p.wrap("/opt/cni/bin/dpu-cni"), nil
	case OpenShiftFlavour:
		return p.wrap("/var/lib/cni/bin/dpu-cni"), nil
	case KindFlavour:
		return p.wrap("/opt/cni/bin/dpu-cni"), nil
	default:
		return "", fmt.Errorf("unknown flavour")
	}
}

func (p *PathManager) VendorPluginSocket() string {
	return p.wrap("/var/run/dpu-daemon/vendor-plugin/vendor-plugin.sock")
}

func (p *PathManager) wrap(path string) string {
	return filepath.Join(p.rootDir, path)
}

// EnsureSocketDirExists makes sure that the socket being created is only accessible to root.
func (p *PathManager) EnsureSocketDirExists(serverSocketPath string) error {
	runDir := filepath.Dir(serverSocketPath)
	// Remove and re-create the socket directory with root-only permissions
	klog.Infof("Removing %v", runDir)
	if err := os.RemoveAll(runDir); err != nil && !os.IsNotExist(err) {
		info, err := os.Stat(runDir)
		if err != nil {
			return fmt.Errorf("failed to stat old socket directory %s: %v", runDir, err)
		}
		// Owner must be root
		tmp := info.Sys()
		statt, ok := tmp.(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("failed to read socket directory stat info: %T", tmp)
		}
		if statt.Uid != 0 {
			return fmt.Errorf("insecure owner of socket directory %s: %v", runDir, statt.Uid)
		}

		// Check permissions
		if info.Mode()&0o777 != 0o700 {
			return fmt.Errorf("insecure permissions on socket directory %s: %v", runDir, info.Mode())
		}
		// Finally remove the socket file so we can re-create it
		if err := os.Remove(serverSocketPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove old socket %s: %v", serverSocketPath, err)
		}
	}
	klog.Infof("Creating %v", runDir)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return fmt.Errorf("failed to create socket directory %s: %v", runDir, err)
	}
	return nil
}
