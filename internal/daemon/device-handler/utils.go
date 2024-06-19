package devicehandler

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

const (
	sysBusPciDevices = "/sys/bus/pci/devices"
)

// GetDriverName returns current driver attached to a pci device from its pci address
func GetDriverName(pciAddr string) (string, error) {
	driverLink := filepath.Join(sysBusPciDevices, pciAddr, "driver")
	driverInfo, err := os.Readlink(driverLink)
	if err != nil {
		return "", fmt.Errorf("error getting driver info for device %s %v", pciAddr, err)
	}
	return filepath.Base(driverInfo), nil
}

// GetNumaNode returns current numa node that the pci device belong to from its pci address
func GetNumaNode(pciAddr string) int {
	devNodePath := filepath.Join(sysBusPciDevices, pciAddr, "numa_node")
	node, err := os.ReadFile(devNodePath)
	if err != nil {
		return -1
	}
	node = bytes.TrimSpace(node)
	numNode, err := strconv.Atoi(string(node))
	if err != nil {
		return -1
	}
	return numNode
}
