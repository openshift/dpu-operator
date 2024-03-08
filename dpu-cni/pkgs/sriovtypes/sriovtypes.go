package sriovtypes

import (
	"regexp"
)

var (
	rePciDeviceName = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{2}:[01][0-9a-f]\.[0-7]$`)
	reAuxDeviceName = regexp.MustCompile(`^\w+.\w+.\d+$`)
)

// IsPCIDeviceName check if passed device id is a PCI device name
func IsPCIDeviceName(deviceID string) bool {
	return rePciDeviceName.MatchString(deviceID)
}

// IsAuxDeviceName check if passed device id is a Auxiliary device name
func IsAuxDeviceName(deviceID string) bool {
	return reAuxDeviceName.MatchString(deviceID)
}
