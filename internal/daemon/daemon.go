package daemon

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/internal/images"
	"github.com/openshift/dpu-operator/internal/platform"
	"github.com/openshift/dpu-operator/internal/scheme"
	"github.com/openshift/dpu-operator/internal/utils"

	"github.com/go-logr/logr"
	"github.com/spf13/afero"
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

			// Sync DPU CRs with the current state
			err = d.SyncDpuCRs(d.detectedDpus)
			if err != nil {
				d.log.Error(err, "Failed to sync DPU CRs")
				// Don't fail the daemon if CR sync fails, just log and continue
			}

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

func (d *Daemon) SyncDpuCRs(detectedDpus map[string]platform.DetectedDpu) error {
	// Get all existing DPU CRs from K8s
	existingCRs := &v1.DataProcessingUnitList{}
	err := d.client.List(context.TODO(), existingCRs)
	if err != nil {
		return fmt.Errorf("Failed to list existing DPU CRs: %v", err)
	}

	// Create a map of existing CRs by name for quick lookup
	existingCRMap := make(map[string]*v1.DataProcessingUnit)
	for i := range existingCRs.Items {
		cr := &existingCRs.Items[i]
		existingCRMap[cr.Name] = cr
	}

	// Sync each detected DPU to K8s
	for identifier, detectedDpu := range detectedDpus {
		err := d.syncSingleDpuCR(identifier, detectedDpu, existingCRMap)
		if err != nil {
			d.log.Error(err, "Failed to sync DPU CR", "identifier", identifier)
			// Continue with other DPUs instead of failing completely
		}
	}

	// TODO: Consider removing CRs that no longer have corresponding in-memory DPUs
	// This might be done in a separate cleanup phase

	return nil
}

func (d *Daemon) syncSingleDpuCR(identifier string, detectedDpu platform.DetectedDpu, existingCRMap map[string]*v1.DataProcessingUnit) error {
	// Use identifier as CR name, but ensure it's a valid K8s name
	crName := d.sanitizeCRName(identifier)

	// Determine DPU type from the detected DPU's plugin vendor
	dpuType := d.getDpuTypeFromDetectedDpu(detectedDpu)

	// Create desired CR spec
	desiredCR := &v1.DataProcessingUnit{
		ObjectMeta: metav1.ObjectMeta{
			Name: crName,
		},
		Spec: v1.DataProcessingUnitSpec{
			DpuType:   dpuType,
			IsDpuSide: detectedDpu.IsDpuPlatform,
		},
		Status: v1.DataProcessingUnitStatus{
			Status: "Detected",
		},
	}

	// Check if CR already exists
	if existingCR, exists := existingCRMap[crName]; exists {
		// CR exists, update it if needed
		if existingCR.Spec.DpuType != dpuType ||
			existingCR.Spec.IsDpuSide != detectedDpu.IsDpuPlatform ||
			existingCR.Status.Status != "Detected" {
			existingCR.Spec = desiredCR.Spec
			existingCR.Status = desiredCR.Status
			err := d.client.Update(context.TODO(), existingCR)
			if err != nil {
				return fmt.Errorf("Failed to update DPU CR %s: %v", crName, err)
			}
			d.log.Info("Updated DPU CR", "name", crName, "dpuType", dpuType, "isDpuSide", detectedDpu.IsDpuPlatform)
		}
	} else {
		// CR doesn't exist, create it
		err := d.client.Create(context.TODO(), desiredCR)
		if err != nil {
			return fmt.Errorf("Failed to create DPU CR %s: %v", crName, err)
		}
		d.log.Info("Created DPU CR", "name", crName, "dpuType", dpuType, "isDpuSide", detectedDpu.IsDpuPlatform)
	}

	return nil
}

func (d *Daemon) sanitizeCRName(identifier string) string {
	// Convert identifier to valid K8s resource name
	// K8s names must be lowercase alphanumeric with dashes
	name := strings.ToLower(string(identifier))
	name = strings.ReplaceAll(name, ":", "-")
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")
	return name
}

func (d *Daemon) getDpuTypeFromDetectedDpu(detectedDpu platform.DetectedDpu) string {
	if detectedDpu.Plugin == nil {
		return "unknown"
	}

	// The plugin should contain vendor information
	// For now, we'll use a simple heuristic based on the identifier or plugin type
	identifier := string(detectedDpu.Identifier)
	if strings.Contains(identifier, "intel") {
		return "Intel IPU"
	} else if strings.Contains(identifier, "marvell") {
		return "Marvell DPU"
	}

	return "unknown"
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
