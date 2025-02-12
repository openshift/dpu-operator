package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/openshift/dpu-operator/internal/platform"
	"github.com/openshift/dpu-operator/internal/scheme"
	"github.com/openshift/dpu-operator/internal/utils"

	"github.com/go-logr/logr"
	"github.com/spf13/afero"
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

type Daemon struct {
	mode      string
	pm        *utils.PathManager
	log       logr.Logger
	vspImages map[string]string
	config    *rest.Config
	mgr       SideManager
	client    client.Client
	fs        afero.Fs
}

func NewDaemon(fs afero.Fs, mode string, config *rest.Config, vspImages map[string]string, pathManager *utils.PathManager) Daemon {
	log := ctrl.Log.WithName("Daemon")
	return Daemon{
		fs:        fs,
		mode:      mode,
		pm:        pathManager,
		log:       log,
		vspImages: vspImages,
		config:    config,
	}
}

func (d *Daemon) ListenAndServe() error {
	listener, err := d.Listen()

	if err != nil {
		d.log.Error(err, "Failed to listen")
		return err
	}

	return d.Serve(listener)
}

func (d *Daemon) Listen() (net.Listener, error) {
	var err error
	d.client, err = client.New(d.config, client.Options{
		Scheme: scheme.Scheme,
	})

	if err != nil {
		return nil, fmt.Errorf("Failed to create client: %v", err)
	}

	ce := utils.NewClusterEnvironment(d.client)
	flavour, err := ce.Flavour(context.TODO())
	if err != nil {
		return nil, err
	}
	d.log.Info("Detected Kuberentes flavour", "flavour", flavour)
	err = d.prepareCni(flavour)
	if err != nil {
		return nil, err
	}
	dpuMode, err := d.isDpuMode()
	if err != nil {
		return nil, err
	}
	d.mgr, err = d.createDaemon(dpuMode, d.config, d.vspImages, d.client)
	if err != nil {
		d.log.Error(err, "Failed to start daemon")
		return nil, err
	}
	return d.mgr.Listen()
}

func (d *Daemon) Serve(listener net.Listener) error {
	return d.mgr.Serve(listener)
}

func (d *Daemon) Stop() {
	d.mgr.Stop()
}

func (d *Daemon) createDaemon(dpuMode bool, config *rest.Config, vspImages map[string]string, client client.Client) (SideManager, error) {
	platform := platform.NewPlatformInfo()
	plugin, err := platform.VspPlugin(dpuMode, vspImages, client)
	if err != nil {
		return nil, err
	}

	if dpuMode {
		return NewDpuSideManger(plugin, config, WithPathManager(*d.pm)), nil
	} else {
		return NewHostSideManager(plugin, WithPathManager2(d.pm)), nil
	}
}

func (d *Daemon) prepareCni(flavour utils.Flavour) error {
	cniPath, err := d.pm.CniPath(flavour)
	if err != nil {
		d.log.Error(err, "Failed to get cni path")
		return err
	}

	err = utils.CopyFile(d.fs, "/dpu-cni", cniPath)
	if err != nil {
		return fmt.Errorf("Failed to prepare CNI binary from /dpu-cni to %v", cniPath)
	}
	err = utils.MakeExecutable(d.fs, cniPath)
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
