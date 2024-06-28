package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"net"
	"os"

	daemon "github.com/openshift/dpu-operator/internal/daemon"
	nfdevicehandler "github.com/openshift/dpu-operator/internal/daemon/device-handler/nf-device-handler"
	sriovdevicehandler "github.com/openshift/dpu-operator/internal/daemon/device-handler/sriov-device-handler"
	deviceplugin "github.com/openshift/dpu-operator/internal/daemon/device-plugin"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"github.com/openshift/dpu-operator/internal/utils"
	"go.uber.org/zap/zapcore"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func createDaemon(dpuMode bool, config *rest.Config) (Daemon, error) {
	plugin := plugin.NewGrpcPlugin(dpuMode)

	if dpuMode {
		deviceHandler := nfdevicehandler.NewNfDeviceHandler()
		dp := deviceplugin.NewDevicePlugin(deviceHandler)
		return daemon.NewDpuDaemon(plugin, dp, config), nil
	} else {
		deviceHandler := sriovdevicehandler.NewSriovDeviceHandler()
		dp := deviceplugin.NewDevicePlugin(deviceHandler)
		return daemon.NewHostDaemon(plugin, dp), nil
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

	v1.AddToScheme(scheme.Scheme)
	log := ctrl.Log.WithName("Daemon Init")
	log.Info("Daemon init")
	config := ctrl.GetConfigOrDie()
	client, err := client.New(config, client.Options{
		Scheme: scheme.Scheme,
	})

	ce := utils.NewClusterEnvironment(client)
	flavour, err := ce.Flavour(context.TODO())
	if err != nil {
		log.Error(err, "Failed to get cluster flavour")
		return
	}
	log.Info("Detected OpenShift", "flavour", flavour)
	pm := utils.NewPathManager("/")
	cniPath, err := pm.CniPath(flavour)
	if err != nil {
		log.Error(err, "Failed to get cni path")
		return
	}
	err = prepareCni(cniPath)
	if err != nil {
		log.Error(err, "Failed to prepare CNI binary", "path", cniPath)
		return
	}
	log.Info("Prepared CNI binary", "path", cniPath)

	dpuMode, err = isDpuMode(mode)
	if err != nil {
		log.Error(err, "Failed to parse mode")
		return
	}
	daemon, err := createDaemon(dpuMode, config)
	if err != nil {
		log.Error(err, "Failed to start daemon")
		return
	}
	daemon.ListenAndServe()
}
