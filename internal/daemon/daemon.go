package daemon

import (
	"context"
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
	mode              string
	pm                *utils.PathManager
	log               logr.Logger
	vspImages         map[string]string
	config            *rest.Config
	mgr               SideManager
	client            client.Client
	fs                afero.Fs
	p                 platform.Platform
	dpuDetectorManger *platform.DpuDetectorManager
}

func NewDaemon(fs afero.Fs, p platform.Platform, mode string, config *rest.Config, vspImages map[string]string, pathManager *utils.PathManager) Daemon {
	log := ctrl.Log.WithName("Daemon")
	return Daemon{
		fs:                fs,
		mode:              mode,
		pm:                pathManager,
		log:               log,
		vspImages:         vspImages,
		config:            config,
		p:                 p,
		dpuDetectorManger: platform.NewDpuDetectorManager(p),
	}
}

func (d *Daemon) Start(ctx context.Context) error {
	listener, err := d.Listen()

	if err != nil {
		d.log.Error(err, "Failed to listen")
		return err
	}

	errChan := make(chan error, 1)

	go func() {
		errChan <- d.Serve(listener)
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		d.mgr.Stop()
		return ctx.Err()
	}
}

func (d *Daemon) Listen() (net.Listener, error) {
	var err error
	d.client, err = client.New(d.config, client.Options{
		Scheme: scheme.Scheme,
	})

	if err != nil {
		return nil, fmt.Errorf("Failed to create client: %v", err)
	}

	err = d.prepareCni()
	if err != nil {
		return nil, err
	}
	d.mgr, err = d.createDaemon()
	if err != nil {
		d.log.Error(err, "Failed to start daemon")
		return nil, err
	}
	return d.mgr.Listen()
}

func (d *Daemon) Serve(listener net.Listener) error {
	return d.mgr.Serve(listener)
}

func (d *Daemon) createDaemon() (SideManager, error) {
	dpuMode, plugin, err := d.dpuDetectorManger.Detect(d.vspImages, d.client, *d.pm)
	if err != nil {
		return nil, fmt.Errorf("Failed to detect DPUs: %v", err)
	}
	if plugin != nil {
		if dpuMode {
			return NewDpuSideManger(plugin, d.config, WithPathManager(*d.pm)), nil
		} else {
			return NewHostSideManager(plugin, WithPathManager2(d.pm)), nil
		}
	}
	return nil, nil
}

func (d *Daemon) prepareCni() error {
	cniPath := d.pm.CniPath()
	d.log.Info("Copying dpu-cni to", "cniPath", cniPath)

	err := utils.CopyFile(d.fs, "/dpu-cni", cniPath)
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
