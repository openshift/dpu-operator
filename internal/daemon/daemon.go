package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/internal/images"
	"github.com/openshift/dpu-operator/internal/platform"
	"github.com/openshift/dpu-operator/internal/scheme"
	"github.com/openshift/dpu-operator/internal/utils"

	"github.com/go-logr/logr"
	"github.com/spf13/afero"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var ()

type SideManager interface {
	StartVsp(ctx context.Context) error
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
	detectedDpus      map[string]platform.DetectedDpu
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
		detectedDpus:      make(map[string]platform.DetectedDpu),
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
			detectedDpusList, err := d.dpuDetectorManger.DetectAll(d.imageManager, d.client, *d.pm)
			currentDpus := d.detectedDpusToMap(detectedDpusList)
			if err != nil {
				d.log.Error(err, "Got error while detecting DPUs")
				return err
			}

			if len(currentDpus) > 1 {
				var keys []string
				for key := range currentDpus {
					keys = append(keys, key)
				}
				err := fmt.Errorf("Detected %d DPUs, but only one is currently supported", len(currentDpus))
				d.log.Error(err, "Got error while detecting DPUs", "dpuKeys", keys)
				return err
			}

			newDpus := d.findNewDpus(d.detectedDpus, currentDpus)

			for _, dpu := range newDpus {
				sideManager, err := d.createSideManager(dpu)
				if err != nil {
					d.log.Error(err, "Got error while creating side manager")
					return err
				}
				d.managers = append(d.managers, sideManager)

				done := make(chan struct{})
				managerDone = append(managerDone, done)
				go func(mgr SideManager, doneChannel chan struct{}) {
					defer close(doneChannel)
					err := d.runSideManager(mgr, managerCtx)
					if err != nil {
						select {
						case errChan <- err:
						default:
						}
					}
				}(sideManager, done)
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

func (d *Daemon) detectedDpusToMap(detectedDpusList []platform.DetectedDpu) map[string]platform.DetectedDpu {
	detectedDpus := make(map[string]platform.DetectedDpu)
	for _, detectedDpu := range detectedDpusList {
		key := string(detectedDpu.Identifier)
		detectedDpus[key] = detectedDpu
	}
	return detectedDpus
}

func (d *Daemon) findNewDpus(previousDpus map[string]platform.DetectedDpu, currentDpus map[string]platform.DetectedDpu) []platform.DetectedDpu {
	var newDpus []platform.DetectedDpu
	for key, detectedDpu := range currentDpus {
		if _, exists := previousDpus[key]; !exists {
			d.log.Info("New DPU detected", "identifier", key, "isDpuPlatform", detectedDpu.IsDpuPlatform)
			d.detectedDpus[key] = detectedDpu
			newDpus = append(newDpus, detectedDpu)
			
			// Create DPU CR when DPU is detected
			err := d.createDpuCR(detectedDpu.IsDpuPlatform)
			if err != nil {
				d.log.Error(err, "Failed to create DPU CR")
				// Don't fail the daemon if CR creation fails, just log and continue
			}
		} else {
		}
	}
	return newDpus
}

func (d *Daemon) createSideManager(detectedDpu platform.DetectedDpu) (SideManager, error) {
	if detectedDpu.IsDpuPlatform {
		dsm, err := NewDpuSideManager(detectedDpu.Plugin, d.config, WithPathManager(*d.pm))
		if err != nil {
			return nil, fmt.Errorf("failed to create DpuSideManager: %v", err)
		}
		return dsm, nil
	} else {
		hsm, err := NewHostSideManager(detectedDpu.Plugin, WithPathManager2(d.pm))
		if err != nil {
			return nil, fmt.Errorf("failed to create HostSideManager: %v", err)
		}
		return hsm, nil
	}
}

func (d *Daemon) createDpuCR(isDpuSide bool) error {
	// Get hostname to use as CR name
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("Failed to get hostname: %v", err)
	}

	// Get DPU type from detector
	dpuType := "unknown"
	vendorID, _, _, err := d.dpuDetectorManger.GetPcieDevFilter()
	if err == nil {
		// Map vendor/product IDs to DPU types
		switch vendorID {
		case "8086": // Intel
			dpuType = "Intel IPU"
		case "177d": // Marvell
			dpuType = "Marvell DPU"
		}
	}

	// Create DPU CR
	dpuCR := &v1.DataProcessingUnit{
		ObjectMeta: metav1.ObjectMeta{
			Name: hostname,
		},
		Spec: v1.DataProcessingUnitSpec{
			DpuType:   dpuType,
			IsDpuSide: isDpuSide,
		},
		Status: v1.DataProcessingUnitStatus{
			Status: "Detected",
		},
	}

	// Check if CR already exists
	existingCR := &v1.DataProcessingUnit{}
	err = d.client.Get(context.TODO(), client.ObjectKey{Name: hostname}, existingCR)
	if err == nil {
		// CR exists, update it if needed
		if existingCR.Spec.DpuType != dpuType || existingCR.Spec.IsDpuSide != isDpuSide || existingCR.Status.Status != "Detected" {
			existingCR.Spec = dpuCR.Spec
			existingCR.Status = dpuCR.Status
			err = d.client.Update(context.TODO(), existingCR)
			if err != nil {
				return fmt.Errorf("Failed to update DPU CR: %v", err)
			}
			d.log.Info("Updated DPU CR", "name", hostname, "dpuType", dpuType, "isDpuSide", isDpuSide)
		}
	} else if errors.IsNotFound(err) {
		// CR doesn't exist, create it
		err = d.client.Create(context.TODO(), dpuCR)
		if err != nil {
			return fmt.Errorf("Failed to create DPU CR: %v", err)
		}
		d.log.Info("Created DPU CR", "name", hostname, "dpuType", dpuType, "isDpuSide", isDpuSide)
	} else {
		return fmt.Errorf("Failed to check existing DPU CR: %v", err)
	}

	return nil
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

func (d *Daemon) runSideManager(mgr SideManager, managerCtx context.Context) error {
	err := mgr.StartVsp(managerCtx)
	if err != nil {
		d.log.Error(err, "Failed to start VSP in sideManager")
		return err
	}
	err = mgr.SetupDevices()
	if err != nil {
		d.log.Error(err, "Failed to setup devices in sideManager")
		return err
	}
	listener, err := mgr.Listen()
	if err != nil {
		d.log.Error(err, "Failed to listen on sideManager")
		return err
	}
	err = mgr.Serve(managerCtx, listener)
	if err != nil && managerCtx.Err() == nil {
		d.log.Error(err, "Failed to serve on sideManager")
		return err
	}
	return nil
}
