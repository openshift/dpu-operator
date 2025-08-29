package daemon

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"time"

	"github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"github.com/openshift/dpu-operator/internal/images"
	"github.com/openshift/dpu-operator/internal/platform"
	"github.com/openshift/dpu-operator/internal/scheme"
	"github.com/openshift/dpu-operator/internal/utils"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/go-logr/logr"
	"github.com/spf13/afero"
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

// ManagedDpu represents a DPU with all its runtime state and management components
type ManagedDpu struct {
	DpuCR   *v1.DataProcessingUnit
	Plugin  *plugin.GrpcPlugin
	Manager SideManager
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
	managedDpus       map[string]*ManagedDpu
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
		managedDpus:       make(map[string]*ManagedDpu),
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
	d.log.Info("Starting daemon serve")
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	d.log.Info("Setting up manager channels")
	var managerDone []chan struct{}
	errChan := make(chan error)
	managerCtx, cancelManagers := context.WithCancel(ctx)
	defer cancelManagers()

	d.log.Info("Entering main daemon loop")

	for {
		select {
		case <-ticker.C:
			detectedDpusList, err := d.dpuDetectorManger.DetectAll(d.imageManager, d.client, *d.pm)
			if err != nil {
				d.log.Error(err, "Got error while detecting DPUs")
				return err
			}

			// Update managed DPUs with newly detected ones
			d.updateManagedDpus(detectedDpusList)

			if len(d.managedDpus) > 1 {
				var keys []string
				for key := range d.managedDpus {
					keys = append(keys, key)
				}
				err := fmt.Errorf("Detected %d DPUs, but only one is currently supported", len(d.managedDpus))
				d.log.Error(err, "Got error while detecting DPUs", "dpuKeys", keys)
				return err
			}

			// Create managers for DPUs that don't have them yet
			for identifier, managedDpu := range d.managedDpus {
				if managedDpu.Manager == nil {
					sideManager, err := d.createSideManager(managedDpu.DpuCR, managedDpu.Plugin)
					if err != nil {
						d.log.Error(err, "Got error while creating side manager")
						return err
					}

					// Store the manager in the ManagedDpu
					managedDpu.Manager = sideManager
					d.managers = append(d.managers, sideManager)

					done := make(chan struct{})
					managerDone = append(managerDone, done)
					go func(mgr SideManager, identifier string, doneChannel chan struct{}) {
						defer close(doneChannel)
						err := d.runSideManager(mgr, identifier, managerCtx)
						if err != nil {
							select {
							case errChan <- err:
							default:
							}
						}
					}(sideManager, identifier, done)
				}
			}

			for _, managedDpu := range d.managedDpus {
				var newCondition metav1.Condition
				if managedDpu.Plugin.IsInitialized() {
					newCondition = metav1.Condition{
						Type:    "Ready",
						Status:  metav1.ConditionTrue,
						Reason:  "Initialized",
						Message: "DPU plugin is initialized and ready.",
					}
				} else {
					newCondition = metav1.Condition{
						Type:    "Ready",
						Status:  metav1.ConditionFalse,
						Reason:  "NotInitialized",
						Message: "DPU plugin is not yet initialized.",
					}
				}
				// Always set a transition time
				newCondition.LastTransitionTime = metav1.Now()

				// Use the helper to add or update the condition
				meta.SetStatusCondition(&managedDpu.DpuCR.Status.Conditions, newCondition)
			}

			// Sync DPU CRs with the current state
			err = d.SyncDpuCRs()
			if err != nil {
				d.log.Error(err, "Failed to sync DPU CRs")
				return err
			}
		case err := <-errChan:
			d.log.Error(err, "Side manager failed, stopping all managers")
			d.shutdown(cancelManagers, managerDone)
			return err
		case <-ctx.Done():
			d.log.Info("Context cancelled, waiting for all side managers to stop")
			d.shutdown(cancelManagers, managerDone)
			return ctx.Err()
		}
	}
}

func (d *Daemon) shutdown(cancelManagers context.CancelFunc, managerDone []chan struct{}) {
	// Clean up all DPU CRs by clearing managed DPUs and syncing
	d.log.Info("Cleaning up DPU CRs before shutdown")
	d.managedDpus = make(map[string]*ManagedDpu)
	err := d.SyncDpuCRs()
	if err != nil {
		d.log.Error(err, "Failed to clean up DPU CRs during shutdown")
	}

	// Stop all side managers
	cancelManagers()
	d.log.Info("Waiting for all side managers to stop")
	for _, done := range managerDone {
		<-done
	}
	d.log.Info("All side managers stopped")
}

func (d *Daemon) createSideManager(dpuCR *v1.DataProcessingUnit, dpuPlugin *plugin.GrpcPlugin) (SideManager, error) {
	if dpuCR.Spec.IsDpuSide {
		dsm, err := NewDpuSideManager(dpuPlugin, d.config, WithPathManager(*d.pm))
		if err != nil {
			return nil, fmt.Errorf("failed to create DpuSideManager: %v", err)
		}
		return dsm, nil
	} else {
		hsm, err := NewHostSideManager(dpuPlugin, WithPathManager2(d.pm))
		if err != nil {
			return nil, fmt.Errorf("failed to create HostSideManager: %v", err)
		}
		return hsm, nil
	}
}

func (d *Daemon) SyncDpuCRs() error {
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

	// Sync each managed DPU to K8s
	for identifier, managedDpu := range d.managedDpus {
		err := d.syncSingleDpuCR(managedDpu.DpuCR, existingCRMap)
		if err != nil {
			d.log.Error(err, "Failed to sync DPU CR", "identifier", identifier)
		}
	}

	// Check for orphaned CRs that no longer have corresponding in-memory DPUs
	for crName, orphanedCR := range existingCRMap {
		if _, exists := d.managedDpus[crName]; !exists {
			d.log.Info("Found orphaned DPU CR, removing it", "crName", crName)
			err := d.client.Delete(context.TODO(), orphanedCR)
			if err != nil {
				d.log.Error(err, "Failed to delete orphaned DPU CR", "crName", crName)
			} else {
				d.log.Info("Successfully deleted orphaned DPU CR", "crName", crName)
			}
		}
	}

	return nil
}

func (d *Daemon) syncSingleDpuCR(dpuCR *v1.DataProcessingUnit, existingCRMap map[string]*v1.DataProcessingUnit) error {
	identifier := dpuCR.Name

	// Check if CR exists in the cluster
	existingCR := &v1.DataProcessingUnit{}
	err := d.client.Get(context.TODO(), client.ObjectKey{
		Name:      identifier,
		Namespace: dpuCR.Namespace,
	}, existingCR)

	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// CR doesn't exist, create it
			err := d.client.Create(context.TODO(), dpuCR)
			if err != nil {
				return fmt.Errorf("Failed to create DPU CR %s: %v", identifier, err)
			}

			// Update status after creation
			err = d.client.Status().Update(context.TODO(), dpuCR)
			if err != nil {
				return fmt.Errorf("Failed to update DPU CR status %s: %v", identifier, err)
			}

			d.log.Info("Created DPU CR", "name", identifier, "dpuProductName", dpuCR.Spec.DpuProductName, "isDpuSide", dpuCR.Spec.IsDpuSide)
			return nil
		}
		return fmt.Errorf("Failed to get DPU CR %s: %v", identifier, err)
	}

	// CR exists, update it to reflect all fields
	needsSpecUpdate := !reflect.DeepEqual(existingCR.Spec, dpuCR.Spec)
	needsStatusUpdate := !reflect.DeepEqual(existingCR.Status, dpuCR.Status)

	if needsSpecUpdate {
		existingCR.Spec = dpuCR.Spec
		err := d.client.Update(context.TODO(), existingCR)
		if err != nil {
			return fmt.Errorf("Failed to update DPU CR spec %s: %v", identifier, err)
		}
	}

	if needsStatusUpdate {
		existingCR.Status = dpuCR.Status
		err := d.client.Status().Update(context.TODO(), existingCR)
		if err != nil {
			return fmt.Errorf("Failed to update DPU CR status %s: %v", identifier, err)
		}
	}

	if needsSpecUpdate || needsStatusUpdate {
		d.log.Info("Updated DPU CR", "name", identifier, "dpuProductName", dpuCR.Spec.DpuProductName, "isDpuSide", dpuCR.Spec.IsDpuSide)
	}

	return nil
}

func (d *Daemon) updateManagedDpus(detectedDpusList []*platform.DetectedDpuWithPlugin) {
	// Create a map of currently detected DPUs for easy lookup
	currentlyDetected := make(map[string]*platform.DetectedDpuWithPlugin)
	for _, detected := range detectedDpusList {
		identifier := detected.DpuCR.Name
		currentlyDetected[identifier] = detected
	}

	// Create new managed DPUs. Each identifier is unique for each DPU, so it's not
	// possible to have the identifier point to a DPU that morphed into another DPU
	for identifier, detected := range currentlyDetected {
		if _, exists := d.managedDpus[identifier]; !exists {
			// Create new ManagedDpu entry
			d.managedDpus[identifier] = &ManagedDpu{
				DpuCR:   detected.DpuCR,
				Plugin:  detected.Plugin,
				Manager: nil, // Will be created later
			}
			d.log.Info("Created new ManagedDpu entry", "identifier", identifier)
		}
	}

	// Remove managed DPUs that are no longer detected
	for identifier := range d.managedDpus {
		if _, stillDetected := currentlyDetected[identifier]; !stillDetected {
			d.log.Info("Removing no longer detected DPU", "identifier", identifier)
			delete(d.managedDpus, identifier)
			// TODO: properly clean up managers
		}
	}
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

func (d *Daemon) runSideManager(mgr SideManager, identifier string, managerCtx context.Context) error {
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
