package mrvlutils

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaypipes/ghw"
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

const (
	VendorID         = "177d" // vendor ID for Marvell OCTEON
	deviceID         = "a0f7" // device ID for Marvell OCTEON(CN10K) SDP Interface
	SysBusPci string = "/sys/bus/pci/devices"
)

// GetAllVfsByDeviceID returns the list of all VFs associated with the given device ID
func GetAllVfsByDeviceID(deviceID string) ([]string, error) {
	pci, err := ghw.PCI()
	if err != nil {
		return nil, err
	}
	var targetAddresses []string
	for _, dev := range pci.Devices {
		if dev.Vendor.ID == VendorID && dev.Product.ID == deviceID {
			targetAddresses = append(targetAddresses, dev.Address)
		}
	}
	if len(targetAddresses) == 0 {
		return nil, errors.New("no devices found with given Vendor ID and Device ID")
	}
	return targetAddresses, nil
}

// Mapped_VF returns the PCI address of the VF mapped to the given PF
func Mapped_VF(pf_count int, pfid int, vfid int) (string, error) {
	pci, err := ghw.PCI()
	if err != nil {
		return "", err
	}

	devices := pci.Devices
	var list []string
	for _, device := range devices {
		if device.Vendor != nil && device.Product != nil {
			if device.Vendor.ID == VendorID &&
				device.Product.ID == deviceID {
				list = append(list, device.Address)
			}
		}
	}
	dpu_vfid := pf_count*vfid + pfid
	size := len(list) - 1
	if dpu_vfid >= size {
		return "", errors.New("mapped VF out of bounds")
	}

	vf_pci := strings.Split(list[dpu_vfid], " ")
	return vf_pci[0], nil
}

// getInterfaceName function to get the Interface Name of the given Device ID and vendor ID
// It will return the Interface Name and error
func GetNameByDeviceID(deviceID string) (string, error) {
	targetVendorID := VendorID
	targetDeviceID := deviceID
	pci, err := ghw.PCI()
	if err != nil {
		return "", err
	}
	var pciAddress string
	for _, device := range pci.Devices {
		if device.Vendor != nil && device.Product != nil {
			if device.Vendor.ID == targetVendorID && device.Product.ID == targetDeviceID {
				pciAddress = device.Address
				break
			}
		}
	}
	if pciAddress == "" {
		return "", fmt.Errorf("device not found with Vendor ID: %s and Device ID: %s", targetVendorID, targetDeviceID)
	}
	ifname, err := GetNameByPCI(pciAddress)
	if err != nil {
		return "", err
	}
	return ifname, nil
}

// GetInterfaceName returns the Name of Interface of  network device forthe given PCI address
func GetNameByPCI(pciAddress string) (string, error) {
	pfSymLink := filepath.Join(SysBusPci, pciAddress, "net")
	_, err := os.Lstat(pfSymLink)
	if err != nil {
		return "", err
	}

	files, err := os.ReadDir(pfSymLink)
	if err != nil {
		return "", err
	}

	if len(files) < 1 {
		return "", errors.New("PF network device not found")
	}

	return strings.TrimSpace(files[0].Name()), nil
}

// GetPCIByDeviceID returns the First Interface's PCI address of the device for the given device ID
func GetPCIByDeviceID(deviceID string) (string, error) {
	targetVendorID := VendorID
	targetDeviceID := deviceID
	pci, err := ghw.PCI()
	if err != nil {
		return "", err
	}
	var pciAddress string
	for _, device := range pci.Devices {
		if device.Vendor != nil && device.Product != nil {
			if device.Vendor.ID == targetVendorID && device.Product.ID == targetDeviceID {
				pciAddress = device.Address
				break
			}
		}
	}
	if pciAddress == "" {
		return "", fmt.Errorf("device not found with Vendor ID: %s and Device ID: %s", targetVendorID, targetDeviceID)
	}
	return pciAddress, nil
}

// Print DPDK port info prints information of dpdk port with given pci address
func PrintDPDKPortInfo(pciAddress string) error {
	// pciAddress := "0000:03:00.0"
	// Get PCI device information
	pci, err := ghw.PCI()
	if err != nil {
		return err
	}
	// Find the network interface associated with the PCI address
	var IfName string
	for _, device := range pci.Devices {
		if device.Address == pciAddress {
			IfName = device.Product.Name
			break
		}
	}
	if IfName == "" {
		klog.Errorf("No network interface found for PCI address %s", pciAddress)
		return errors.New("no network interface found for PCI address")
	}
	// Get network interface details
	IntfDetails, err := net.InterfaceByName(IfName)
	if err != nil {
		klog.Errorf("Error fetching interface %s: %v", IfName, err)
		return err
	}
	// Print interface details
	klog.Infof("Interface Name: %s\n", IntfDetails.Name)
	klog.Infof("MAC Address: %s\n", IntfDetails.HardwareAddr)
	klog.Infof("MTU: %d\n", IntfDetails.MTU)
	return nil
}

// PrintPortInfo prints the information of Interface with given portName visible to kernel
func PrintPortInfo(portName string) error {
	link, err := netlink.LinkByName(portName)
	if err != nil {
		klog.Errorf("Error fetching interface %s: %v", portName, err)
		return err
	}
	addrs, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		klog.Errorf("Error fetching addresses for interface %s: %v", portName, err)
		return err
	}
	klog.Infof("Interface Name: %s", link.Attrs().Name)
	klog.Infof("Hardware Address (MAC): %s", link.Attrs().HardwareAddr)
	klog.Infof("MTU: %d", link.Attrs().MTU)
	for _, addr := range addrs {
		klog.Infof("Address: %s", addr.IP.String())
	}
	return nil
}
func GetPCIByName(portName string) (string, error) {
	link, err := netlink.LinkByName(portName)
	if err != nil {
		klog.Errorf("Error fetching interface %s: %v", portName, err)
		return "", err
	}
	return link.Attrs().Name, nil
}
