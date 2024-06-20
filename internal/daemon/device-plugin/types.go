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

type DeviceList map[string]pluginapi.Device

type DeviceHandler interface {
	SetupDevices() error
	GetDevices() (*DeviceList, error)
}
