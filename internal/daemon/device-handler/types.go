package devicehandler

import (
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

type DeviceList map[string]pluginapi.Device

type DeviceHandler interface {
	SetupDevices() error
	GetDevices() (*DeviceList, error)
}
