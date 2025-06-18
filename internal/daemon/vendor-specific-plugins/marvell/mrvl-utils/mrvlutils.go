package mrvlutils

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jaypipes/ghw"
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

const (
	MrvlVendorID           = "177d" // vendor ID for Marvell OCTEON
	MrvlDPUSDPPFId         = "a0f7" // device ID for Marvell OCTEON(CN10K) SDP Interface
	MrvlHostSDPPFId        = "b900" // device ID for Host SDP Interface
	MrvlDPIPFId            = "a080" // device ID for Marvell OCTEON(CN10K) DPI PF
	MrvlPEMPFId            = "a06c" // device ID for Marvell OCTEON(CN10K) PEM PF
	SysBusPci       string = "/sys/bus/pci/devices"
)

// GetAllVfsByDeviceID returns the list of all VFs associated with the given device ID
func GetAllVfsByDeviceID(deviceID string) ([]string, error) {
	pci, err := ghw.PCI()
	if err != nil {
		return nil, err
	}
	var targetAddresses []string
	for _, dev := range pci.Devices {
		if dev.Vendor.ID == MrvlVendorID && dev.Product.ID == deviceID {
			targetAddresses = append(targetAddresses, dev.Address)
		}
	}
	if len(targetAddresses) == 0 {
		return nil, errors.New("no devices found with given Vendor ID and Device ID")
	}
	return targetAddresses, nil
}

func GetAllVfsNameByDeviceID(deviceID string) ([]string, error) {
	dpuVfsPCI, err := GetAllVfsByDeviceID(deviceID)
	if err != nil {
		return nil, err
	}
	dpuVfsName := make([]string, 0)
	for _, vfpci := range dpuVfsPCI {
		vfName, err := GetNameByPCI(vfpci)
		if err != nil {
			return nil, err
		}
		dpuVfsName = append(dpuVfsName, vfName)
	}
	return dpuVfsName, nil
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
			if device.Vendor.ID == MrvlVendorID &&
				device.Product.ID == MrvlDPUSDPPFId {
				list = append(list, device.Address)
			}
		}
	}
	dpu_vfid := pf_count*vfid + pfid
	size := len(list) - 1
	if dpu_vfid > size {
		return "", errors.New("mapped VF out of bounds")
	}

	vf_pci := strings.Split(list[dpu_vfid], " ")
	return vf_pci[0], nil
}

// getInterfaceName function to get the Interface Name of the given Device ID and vendor ID
// It will return the Interface Name and error
func GetNameByDeviceID(deviceID string) (string, error) {
	targetVendorID := MrvlVendorID
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
	targetVendorID := MrvlVendorID
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

// GetPCIDriver returns the driver in use for the PCI address
func GetPCIDriver(pciAddress string) string {
	pci, err := ghw.PCI()
	if err != nil {
		klog.Errorf("Error getting ghw product info: %v", err)
		os.Exit(1)
	}

	deviceInfo := pci.GetDevice(pciAddress)
	return deviceInfo.Driver
}

// DetectPlatformMode returns detects platform mode
func DetectPlatformMode() string {
	pci, err := ghw.PCI()
	if err != nil {
		klog.Fatalf("Error getting ghw product info: %v", err)
	}

	for _, pci := range pci.Devices {
		if pci.Vendor.ID == MrvlVendorID &&
			pci.Product.ID == MrvlDPUSDPPFId {
			return "dpu"
		}
	}

	return "host"
}

func BindToVFIO(pciAddress string) error {
	driver := GetPCIDriver(pciAddress)
	if driver != "" {
		// Path to the device driver
		unbindPath := filepath.Join("/sys/bus/pci/drivers/", driver, "unbind")

		// Unbind the device from its current driver
		if err := os.WriteFile(unbindPath, []byte(pciAddress), 0644); err != nil {
			klog.Errorf("failed to unbind device: %v", err)
			return err
		}
	}

	// Bind the device to VFIO
	overridePath := filepath.Join("/sys/bus/pci/devices/", pciAddress, "driver_override")
	if err := os.WriteFile(overridePath, []byte("vfio-pci"), 0644); err != nil {
		klog.Errorf("failed to override device to VFIO: %v", err)
		return err
	}

	probePath := filepath.Join("/sys/bus/pci/drivers_probe")
	if err := os.WriteFile(probePath, []byte(pciAddress), 0644); err != nil {
		klog.Errorf("failed to probe device to VFIO: %v", err)
		return err
	}

	return nil
}

func SetupHugepages() error {
	cmd := "mkdir -p /dev/huge"
	err := exec.Command("bash", "-c", cmd).Run()
	if err != nil {
		klog.Errorf("Failed to create /dev/huge: %v", err)
		return err
	}

	cmd = "mount -t hugetlbfs none /dev/huge"
	err = exec.Command("bash", "-c", cmd).Run()
	if err != nil {
		klog.Errorf("Failed to mount /dev/huge: %v", err)
		return err
	}

	return nil
}

func loadHostDriver() error {
	cmd := "chroot /host modprobe octeon_ep"
	err := exec.Command("bash", "-c", cmd).Run()
	if err != nil {
		klog.Errorf("Failed to load driver octeon_ep: %v", err)
	}

	cmd = "chroot /host modprobe octeon_ep_vf"
	err = exec.Command("bash", "-c", cmd).Run()
	if err != nil {
		klog.Errorf("Failed to load driver octeon_ep_vf: %v", err)
	}

	return nil
}

func unloadHostDriver() {
	cmd := "chroot /host rmmod octeon_ep_vf"
	_ = exec.Command("bash", "-c", cmd).Run()

	cmd = "chroot /host rmmod octeon_ep"
	_ = exec.Command("bash", "-c", cmd).Run()
}

func SetupHostInterface() error {
	for i := 1; i < 10; i++ {
		ifName, err := GetNameByDeviceID(MrvlHostSDPPFId)
		if err == nil && ifName != "" {
			klog.Info("Host Interface found")
			return nil
		}

		unloadHostDriver()

		klog.Info("Waiting for host interface")
		time.Sleep(20 * time.Second)

		err = loadHostDriver()
		if err != nil {
			klog.Error("Failed to load host drivers")
			os.Exit(1)
		}

		time.Sleep(5 * time.Second)
	}

	return errors.New("Failed to set up Host Interface")
}

func SetupDpuService() error {
	cmd := "cp /cp-agent.service /host/etc/systemd/system/cp-agent.service"
	err := exec.Command("bash", "-c", cmd).Run()
	if err != nil {
		klog.Errorf("Failed to copy cp-agent.service: %v", err)
		return err
	}

	cmd = "chroot /host systemctl enable cp-agent"
	err = exec.Command("bash", "-c", cmd).Run()
	if err != nil {
		klog.Errorf("Failed to enable cp-agent.service: %v", err)
		return err
	}

	cmd = "chroot /host systemctl daemon-reload"
	err = exec.Command("bash", "-c", cmd).Run()
	if err != nil {
		klog.Errorf("Failed to reload daemon with cp-agent.service: %v", err)
		return err
	}

	cmd = "chroot /host systemctl start cp-agent"
	err = exec.Command("bash", "-c", cmd).Run()
	if err != nil {
		klog.Errorf("Failed to start cp-agent.service: %v", err)
		return err
	}

	klog.Info("cp-agent service enabled")
	return nil
}

func SetupPlatform() error {
	if DetectPlatformMode() == "host" {
		err := SetupHostInterface()
		if err != nil {
			klog.Errorf("Failed to set up PF: %v", err)
			return err
		}
	} else {
		// TODO: move away from using systemd file
		err := SetupDpuService()
		if err != nil {
			klog.Errorf("Failed to set up Control Plane Agent: %v", err)
			return err
		}
	}

	return nil
}
