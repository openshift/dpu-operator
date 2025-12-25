// Package dpuprovider defines the interfaces and shared types used by the
// controller package to interact with the daemon without creating an import cycle.
// Both internal/controller and internal/daemon import this package; neither
// imports the other.
package dpuprovider

import (
	"context"

	pb "github.com/openshift/dpu-operator/dpu-api/gen"
)

// ManagedDpuInfo is a minimal view of a managed DPU exposed to the controller.
type ManagedDpuInfo struct {
	// Labels of the DPU CR, used for selector matching.
	Labels map[string]string
}

// DpuVSP is the minimal vendor-specific-plugin interface the reconciler needs.
type DpuVSP interface {
	Ping(ctx context.Context) (*pb.PingResponse, error)
	RebootDpu(ctx context.Context, req *pb.DPURebootRequest) (*pb.DPUManagementResponse, error)
	UpgradeFirmware(ctx context.Context, req *pb.DPUFirmwareUpgradeRequest) (*pb.DPUManagementResponse, error)
}

// DpuProvider is the interface the controller uses to locate DPU plugins.
// It is satisfied by *daemon.Daemon.
type DpuProvider interface {
	// GetSpecificDpuPlugin returns the plugin for the named DPU, or nil.
	GetSpecificDpuPlugin(name string) DpuVSP
	// GetManagedDpus returns a snapshot of all DPUs managed by this daemon.
	GetManagedDpus() map[string]ManagedDpuInfo
}
