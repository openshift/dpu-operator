package daemon

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/openshift/dpu-operator/internal/images"
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
	StartVsp() error
	SetupDevices() error
	Listen() (net.Listener, error)
	Serve(ctx context.Context, listen net.Listener) error
}

type Daemon struct {
	mode              string
	pm                *utils.PathManager
	log               logr.Logger
	imageManager      images.ImageManager
	config            *rest.Config
	managers          []SideManager
	client            client.Client
	fs                afero.Fs
	p                 platform.Platform
	dpuDetectorManger *platform.DpuDetectorManager
}

func NewDaemon(fs afero.Fs, p platform.Platform, mode string, config *rest.Config, imageManager images.ImageManager, pathManager *utils.PathManager) Daemon {
	log := ctrl.Log.WithName("Daemon")
	return Daemon{
		fs:                fs,
		mode:              mode,
		pm:                pathManager,
		log:               log,
		imageManager:      imageManager,
		config:            config,
		p:                 p,
		dpuDetectorManger: platform.NewDpuDetectorManager(p),
		managers:          make([]SideManager, 0),
	}
}

func (d *Daemon) PrepareAndServe(ctx context.Context) error {
	err := d.Prepare()

	if err != nil {
		d.log.Error(err, "Failed to listen")
		return err
	}

	return d.Serve(ctx)
}

func (d *Daemon) Prepare() error {
	var err error
	d.client, err = client.New(d.config, client.Options{
		Scheme: scheme.Scheme,
	})

	if err != nil {
		return fmt.Errorf("Failed to create client: %v", err)
	}

	err = d.prepareCni()
	if err != nil {
		return err
	}
	return nil
}

func (d *Daemon) Serve(ctx context.Context) error {
	d.log.Info("Starting detection loop")
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var managerDone []chan struct{}
	errChan := make(chan error)
	managerCtx, cancelManagers := context.WithCancel(ctx)
	defer cancelManagers()

	for {
		select {
		case <-ticker.C:
			if len(d.managers) >= 1 {
				continue
			}
			d.log.Info("Scanning for DPUs")
			sideManager, err := d.createDaemon()
			if err != nil {
				d.log.Error(err, "Got error while detecting DPUs")
				return err
			}
			if sideManager != nil {
				d.managers = append(d.managers, sideManager)
				done := make(chan struct{})
				managerDone = append(managerDone, done)
				go func(mgr SideManager) {
					defer close(done)
					err := mgr.StartVsp()
					if err != nil {
						d.log.Error(err, "Failed to start VSP in sideManager")
						select {
						case errChan <- err:
						default:
						}
						return
					}
					err = mgr.SetupDevices()
					if err != nil {
						d.log.Error(err, "Failed to setup devices in sideManager")
						select {
						case errChan <- err:
						default:
						}
						return
					}
					listener, err := mgr.Listen()
					if err != nil {
						d.log.Error(err, "Failed to listen on sideManager")
						select {
						case errChan <- err:
						default:
						}
						return
					}
					err = mgr.Serve(managerCtx, listener)
					if err != nil && managerCtx.Err() == nil {
						d.log.Error(err, "Failed to serve on sideManager")
						select {
						case errChan <- err:
						default:
						}
					}
				}(sideManager)
			}
		case err := <-errChan:
			d.log.Error(err, "Side manager failed, stopping all managers")
			cancelManagers()
			d.log.Info("Waiting for all side managers to stop")
			for _, done := range managerDone {
				<-done
			}
			d.log.Info("All side managers stopped")
			return err
		case <-ctx.Done():
			d.log.Info("Context cancelled, waiting for all side managers to stop")
			cancelManagers()
			for _, done := range managerDone {
				<-done
			}
			d.log.Info("All side managers stopped")
			return ctx.Err()
		}
	}
}

func (d *Daemon) createDaemon() (SideManager, error) {
	dpuMode, plugin, err := d.dpuDetectorManger.Detect(d.imageManager, d.client, *d.pm)
	if err != nil {
		return nil, fmt.Errorf("Failed to detect DPUs: %v", err)
	}
	if plugin != nil {
		if dpuMode {
			dsm, err := NewDpuSideManager(plugin, d.config, WithPathManager(*d.pm))
			if err != nil {
				return nil, fmt.Errorf("failed to create DpuSideManager: %v", err)
			}
			return dsm, nil
		} else {
			hsm, err := NewHostSideManager(plugin, WithPathManager2(d.pm))
			if err != nil {
				return nil, fmt.Errorf("failed to create HostSideManager: %v", err)
			}
			return hsm, nil
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
