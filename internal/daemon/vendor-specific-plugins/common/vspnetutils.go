package vspnetutils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/openshift/dpu-operator/internal/platform"
	"github.com/spf13/afero"
	"k8s.io/klog/v2"
)

// SetSriovNumVfs sets the number of virtual functions (VFs) for a given PCI address.
func SetSriovNumVfs(fs afero.Fs, pciAddr string, numVfs int) error {
	klog.Infof("SetSriovNumVfs(): set NumVfs device %s numvfs %d", pciAddr, numVfs)
	numVfsFilePath := filepath.Join("/sys/bus/pci/devices", pciAddr, "sriov_numvfs")
	bs := []byte(strconv.Itoa(numVfs))
	err := afero.WriteFile(fs, numVfsFilePath, []byte("0"), os.ModeAppend)
	if err != nil {
		klog.Errorf("SetSriovNumVfs(): fail to reset NumVfs file path %s, err %v", numVfsFilePath, err)
		return err
	}
	if numVfs == 0 {
		return nil
	}
	err = afero.WriteFile(fs, numVfsFilePath, bs, os.ModeAppend)
	if err != nil {
		klog.Errorf("SetSriovNumVfs(): fail to set NumVfs file path %s, err %v", numVfsFilePath, err)
		return err
	}
	return nil
}

// TODO: Tech Debt, commands that use exec could be abstrated to a testable interface.
// linkHasAddrgenmodeEui64 checks if the given net interface has addrgenmode set to eui64.
func linkHasAddrgenmodeEui64(interfaceName string) bool {
	out, err := exec.Command("ip", "-d", "link", "show", "dev", interfaceName).Output()
	return err == nil && strings.Contains(string(out), "addrgenmode eui64")
}

// enableOptomisticDuplicateAddressDetection enables optimistic duplicate address detection on the given net interface.
func enableOptomisticDuplicateAddressDetection(fs afero.Fs, interfaceName string) error {
	optimistic_dad_file := "/proc/sys/net/ipv6/conf/" + interfaceName + "/optimistic_dad"
	err := afero.WriteFile(fs, optimistic_dad_file, []byte("1"), os.ModeAppend)
	return err
}

// TODO: Tech Debt, commands that use exec could be abstrated to a testable interface.
// EnableIPV6LinkLocal enables IPv6 link local address on the given net interface with the provided interface nad ipv6 address.
func EnableIPV6LinkLocal(fs afero.Fs, interfaceName string, ipv6Addr string) error {
	// Tell NetworkManager to not manage our interface.
	err1 := exec.Command("nsenter", "-t", "1", "-m", "-u", "-n", "-i", "--", "nmcli", "device", "set", interfaceName, "managed", "no").Run()
	if err1 != nil {
		// This error may be fine. Maybe our host doesn't even run
		// NetworkManager. Ignore.
		klog.Infof("EnableIPV6LinkLocal() nmcli device set %s managed no failed with error %v", interfaceName, err1)
	}

	err1 = enableOptomisticDuplicateAddressDetection(fs, interfaceName)
	if err1 != nil {
		klog.Errorf("EnableIPV6LinkLocal() Error setting optimistic dad: %v", err1)
	}

	if linkHasAddrgenmodeEui64(interfaceName) {
		// Kernel may require that the SDP interfaces are up at all times (RHEL-90248).
		// If the addrgenmode is already eui64, assume we are fine and don't need to reset
		// it (and don't need to toggle the link state).
	} else {
		// Ensure to set addrgenmode and toggle link state (which can result in creating
		// the IPv6 link local address).
		err2 := exec.Command("ip", "link", "set", interfaceName, "addrgenmode", "eui64").Run()
		if err2 != nil {
			return fmt.Errorf("error setting link %s addrgenmode: %v", interfaceName, err2)
		}
		err2 = exec.Command("ip", "link", "set", interfaceName, "down").Run()
		if err2 != nil {
			return fmt.Errorf("error setting link %s down after setting addrgenmode: %v", interfaceName, err2)
		}
	}

	err := exec.Command("ip", "link", "set", interfaceName, "up").Run()
	if err != nil {
		return fmt.Errorf("error setting link %s up: %v", interfaceName, err)
	}

	err = exec.Command("ip", "addr", "replace", ipv6Addr+"/64", "dev", interfaceName, "optimistic").Run()
	if err != nil {
		return fmt.Errorf("error configuring IPv6 address %s/64 on link %s: %v", ipv6Addr, interfaceName, err)
	}
	return nil
}

// GetNetDevNameFromPCIeAddr retrieves the network device name associated with a given PCIe address.
// This can fail if the given PCIe address is not a NetDev or the driver is not loaded correctly.
func GetNetDevNameFromPCIeAddr(platform platform.Platform, pcieAddress string) (string, error) {
	nics, err := platform.NetDevs()
	if err != nil {
		return "", fmt.Errorf("failed to get network devices: %w", err)
	}

	for _, nic := range nics {
		if nic.PCIAddress != nil && *nic.PCIAddress == pcieAddress {
			klog.Infof("GetNetDevNameFromPCIeAddr(): found DPU network device %s %s", nic.Name, *nic.PCIAddress)
			return nic.Name, nil
		}
	}

	return "", fmt.Errorf("network device not found for PCI address %s", pcieAddress)
}
