package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"

	dpudevicehandler "github.com/openshift/dpu-operator/internal/daemon/device-handler/dpu-device-handler"
	deviceplugin "github.com/openshift/dpu-operator/internal/daemon/device-plugin"
	"github.com/openshift/dpu-operator/internal/platform"
	"github.com/openshift/dpu-operator/internal/utils"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var ()

type SideManager interface {
	Listen() (net.Listener, error)
	ListenAndServe() error
	Serve(listen net.Listener) error
	Stop()
}

func createDaemon(dpuMode bool, config *rest.Config, vspImages map[string]string, client client.Client) (SideManager, error) {
	platform := platform.NewPlatformInfo()
	plugin, err := platform.VspPlugin(dpuMode, vspImages, client)
	if err != nil {
		return nil, err
	}

	deviceHandler := dpudevicehandler.NewDpuDeviceHandler(dpudevicehandler.WithDpuMode(dpuMode))
	dp := deviceplugin.NewDevicePlugin(deviceHandler)

	if dpuMode {
		return NewDpuSideManger(plugin, dp, config), nil
	} else {
		return NewHostSideManager(plugin, dp), nil
	}
}

type Daemon struct {
	client    client.Client
	mode      string
	pm        *utils.PathManager
	log       logr.Logger
	vspImages map[string]string
	config    *rest.Config
}

func NewDaemon(mode string, client client.Client, scheme *runtime.Scheme, vspImages map[string]string, config *rest.Config) Daemon {
	log := ctrl.Log.WithName("Daemon")
	return Daemon{
		client:    client,
		mode:      mode,
		pm:        utils.NewPathManager("/"),
		log:       log,
		vspImages: vspImages,
		config:    config,
	}
}

func (d *Daemon) Run() error {
	ce := utils.NewClusterEnvironment(d.client)
	flavours, err := ce.Flavours(context.TODO())
	if err != nil {
		return err
	}
	d.log.Info("Detected Flavours", "flavours", flavours)
	err = d.prepareCni(flavours)
	if err != nil {
		return err
	}
	dpuMode, err := d.isDpuMode()
	if err != nil {
		return err
	}
	daemon, err := createDaemon(dpuMode, d.config, d.vspImages, d.client)
	if err != nil {
		d.log.Error(err, "Failed to start daemon")
		return err
	}
	return daemon.ListenAndServe()
}

func (d *Daemon) prepareCni(flavours utils.FlavourSet) error {
	cniPath, err := d.pm.CniPath(flavours)
	if err != nil {
		d.log.Error(err, "Failed to get cni path")
		return err
	}
	err = utils.CopyFile("/dpu-cni", cniPath)
	if err != nil {
		d.log.Error(err, "Failed to prepare CNI binary", "path", cniPath)
		return err
	}
	err = utils.MakeExecutable(cniPath)
	if err != nil {
		return err
	}
	d.log.Info("Prepared CNI binary", "path", cniPath)
	return nil
}

func (d *Daemon) isDpuMode() (bool, error) {
	if d.mode == "host" {
		return false, nil
	} else if d.mode == "dpu" {
		return true, nil
	} else if d.mode == "auto" {
		platform := platform.NewPlatformInfo()
		detectedDpuMode, err := platform.IsDpu()
		if err != nil {
			return false, fmt.Errorf("Failed to query platform info: %v", err)
		}
		d.log.Info("Autodetected mode", "isDPU", detectedDpuMode)
		return detectedDpuMode, nil
	} else {
		return false, errors.New("Invalid mode")
	}
}
