package daemon

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

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
	managers          []SideManager
	client            client.Client
	fs                afero.Fs
	dpuDetectorManger *platform.DpuDetectorManager
	running           sync.WaitGroup
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
		dpuDetectorManger: platform.NewDpuDetectorManager(p),
		running:           sync.WaitGroup{},
		managers:          make([]SideManager, 0),
	}
}

func (d *Daemon) Start(ctx context.Context) error {
	var ret error
	d.running.Add(1)
	d.log.Info("Preparing CNI binary")
	err := d.Prepare()
	if err != nil {
		return err
	}
	errChan := make(chan error, 1)
	go func() {
		d.detectLoop(errChan, ctx)
	}()

	d.log.Info("Entering wait")
	select {
	case err := <-errChan:
		ret = err
	case <-ctx.Done():
		ret = ctx.Err()
	}
	d.log.Info("Stopping all managers")
	for _, mgr := range d.managers {
		mgr.Stop()
	}
	d.running.Done()
	return ret
}

func (d *Daemon) Prepare() error {
	var err error
	d.client, err = client.New(d.config, client.Options{
		Scheme: scheme.Scheme,
	})

	if err != nil {
		return fmt.Errorf("Failed to create client: %v", err)
	}

	ce := utils.NewClusterEnvironment(d.client)
	flavour, err := ce.Flavour(context.TODO())
	if err != nil {
		return err
	}
	d.log.Info("Detected Kuberentes flavour", "flavour", flavour)
	return d.prepareCni(flavour)
}

func (d *Daemon) detectLoop(errChan chan error, ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if len(d.managers) == 1 {
				continue
			}
			d.log.Info("Scanning for DPUs")
			sideManager, err := d.createDaemon()
			if err != nil {
				d.log.Error(err, "Got error while detecting DPUs")
				errChan <- err
			}
			if sideManager != nil {
				d.managers = append(d.managers, sideManager)
				go func() {
					err := sideManager.ListenAndServe()
					if err != nil {
						d.log.Error(err, "Failed to listen on sideManager")
					}
				}()
			}
		case <-ctx.Done():
			return
		}
	}
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

func (d *Daemon) Wait() {
	d.running.Wait()
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
