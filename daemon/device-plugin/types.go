package deviceplugin

import (
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

var (
	// SockDir is the default Kubelet device plugin socket directory
	SockDir = "/var/lib/kubelet/plugins_registry"
	// DeprecatedSockDir is the deprecated Kubelet device plugin socket directory
	DeprecatedSockDir = "/var/lib/kubelet/device-plugins"
)

const (
	// KubeEndPoint is kubelet socket name
	KubeEndPoint = "kubelet.sock"
)

// ExcludeConfig contains excluded list of devices
type ExcludeConfig struct {
	ExcludeDevices []string `json:"exclude"`
}

type DeviceList map[string]pluginapi.Device

type DeviceHandler interface {
	GetDevices() (*DeviceList, error)
}

// ResourceFactory is an interface to get instances of ResourcePool and ResourceServer
type ResourceFactory interface {
	GetResourceServer(ResourcePool) (ResourceServer, error)
	GetResourcePool(rc *ExcludeConfig) (ResourcePool, error)
}

// ResourcePool represents a generic resource entity
type ResourcePool interface {
	// extended API for internal use
	GetResourceName() string
	GetResourcePrefix() string
	GetDevices() map[string]*pluginapi.Device // for ListAndWatch
	Probe() bool
	GetDeviceSpecs(deviceIDs []string) []*pluginapi.DeviceSpec
	GetEnvs(prefix string, deviceIDs []string) (map[string]string, error)
	GetMounts(deviceIDs []string) []*pluginapi.Mount
	StoreDeviceInfoFile(resourceNamePrefix string) error
	CleanDeviceInfoFile(resourceNamePrefix string) error
	GetCDIName() string
}

// ResourceServer is gRPC server implements K8s device plugin api
type ResourceServer interface {
	// Device manager API
	pluginapi.DevicePluginServer
	// grpc server related
	Start() error
	Stop() error
	// Init initializes resourcePool
	Init() error
	// Watch watches for socket file deletion and restart server if needed
	Watch()
}

// NadUtils is an interface for Network-Attachment-Definition utilities
type NadUtils interface {
}
