package main

import (
	"errors"
	"flag"
	"io"
	"net"
	"os"

	deviceplugin "github.com/openshift/dpu-operator/daemon/device-plugin"
	"github.com/openshift/dpu-operator/daemon/plugin"
	"go.uber.org/zap/zapcore"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var ()

type Daemon interface {
	Listen() (net.Listener, error)
	ListenAndServe() error
	Serve(listen net.Listener) error
	Stop()
}

func isDpuMode(mode string) (bool, error) {
	if mode == "host" {
		return false, nil
	} else if mode == "dpu" {
		return true, nil
	} else {
		return false, errors.New("Invalid mode")
	}
}

func createDaemon(dpuMode bool) (Daemon, error) {
	plugin := plugin.NewGrpcPlugin(dpuMode)
	dp := deviceplugin.NewGrpcPlugin()
	if dpuMode {
		return NewDpuDaemon(plugin, dp), nil
	} else {
		return NewHostDaemon(plugin), nil
	}
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destinationFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destinationFile.Close()

	_, err = io.Copy(destinationFile, sourceFile)
	if err != nil {
		return err
	}

	return nil
}

func makeExecutable(file string) error {
	info, err := os.Stat(file)
	if err != nil {
		return err
	}

	newMode := info.Mode() | 0111

	if err := os.Chmod(file, newMode); err != nil {
		return err
	}

	return nil
}

func prepareCni(path string) error {
	err := copyFile("/dpu-cni", path)
	if err != nil {
		return err
	}
	return makeExecutable(path)
}

func main() {
	var mode string
	var err error
	var dpuMode bool
	flag.StringVar(&mode, "mode", "", "Mode for the daemon, can be either host or dpu")
	opts := zap.Options{
		Development: true,
		Level:       zapcore.DebugLevel,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	log := ctrl.Log.WithName("Daemon Init")
	log.Info("Daemon init")

	// Some k8s clusters use /var/lib (in the case of RHCOS based)
	// and some use /opt (in the case of RHEL based)
	// FIXME: In the future we should detect the cluster and move the
	// binaries to the expected locations only.
	err = prepareCni("/var/lib/cni/bin/dpu-cni")
	if err != nil {
		log.Error(err, "Failed to prepare CNI binary in /var/lib")
		/* Don't return on error for now */
	}
	err = prepareCni("/opt/cni/bin/dpu-cni")
	if err != nil {
		log.Error(err, "Failed to prepare CNI binary in /opt")
		/* Don't return on error for now */
	}

	dpuMode, err = isDpuMode(mode)
	if err != nil {
		log.Error(err, "Failed to parse mode")
		return
	}
	daemon, err := createDaemon(dpuMode)
	if err != nil {
		log.Error(err, "Failed to start daemon")
		return
	}
	daemon.ListenAndServe()
}
