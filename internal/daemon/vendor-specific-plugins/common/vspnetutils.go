package vspnetutils

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/openshift/dpu-operator/internal/platform"
	"github.com/spf13/afero"
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

const (
	NetSysDir            = "/sys/class/net"
	pcidevPrefix         = "device"
	netDevVfDevicePrefix = "virtfn"
)

type VEthPairDeviceInfo struct {
	IfName    string // Interface Name of the veth
	PeerName  string // Interface Name of the peer veth
	IfMac     string // MAC address of the veth interface
	PeerIfMAC string // MAC address of the Peer veth interface
}

type VfDeviceInfo struct {
	PfInterfaceName string // Name of the PF interface
	Id              int    // VF ID starting from 0
	PciAddress      string // PCI address of the VF
	Vlan            int    // VLAN ID of the VF (0 is not vlan tagged)
	Allocated       bool   // Indicates if the VF is allocated or not
}

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

func CreateNfVethPair(idx int) (*VEthPairDeviceInfo, error) {
	//nfInterfaceName is the name of the interface on the Network Function attached to the container
	//dpInterfaceName is the name of the interface on the Data Plane OvS side
	nfInterfaceName := fmt.Sprintf("nf_if%d", idx)
	dpInterfaceName := fmt.Sprintf("dp_if%d", idx)

	return CreateVethPair(nfInterfaceName, dpInterfaceName)
}

// CreateVethPair function to create a veth pair with the given index and InterfaceInfo
func CreateVethPair(ifname string, peername string) (*VEthPairDeviceInfo, error) {
	deviceInfo := VEthPairDeviceInfo{
		IfName:    ifname,
		PeerName:  peername,
		IfMac:     "",
		PeerIfMAC: "",
	}

	// Destroy existing veth pair if it exists
	// This is to ensure that we do not have any stale veth pairs lying around.
	// Error is fine if there is no existing veth pair.
	_ = DestroyVethPair(&deviceInfo)

	vethLink := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{Name: deviceInfo.IfName},
		PeerName:  deviceInfo.PeerName,
	}
	if err := netlink.LinkAdd(vethLink); err != nil {
		klog.Errorf("Error occurred in creating veth pair: %v", err)
		return nil, err
	}

	ifLink, err := netlink.LinkByName(deviceInfo.IfName)
	if err != nil {
		klog.Errorf("Error occurred in getting Link By Name of VEth Link: %v", err)
		return nil, err
	}
	peerLink, err := netlink.LinkByName(deviceInfo.PeerName)
	if err != nil {
		klog.Errorf("Error occurred in getting Link By Name of VEth Peer Link: %v", err)
		return nil, err
	}

	if err := netlink.LinkSetUp(ifLink); err != nil {
		klog.Errorf("Error occurred in setting up NF link: %v", err)
		errOnDestroy := DestroyVethPair(&deviceInfo)
		if errOnDestroy != nil {
			klog.Errorf("Error occurred in destroying existing veth pair: %v", err)
			err = errors.Join(errOnDestroy, err)
		}
		return nil, err
	}

	if err := netlink.LinkSetUp(peerLink); err != nil {
		klog.Errorf("Error occurred in setting up DP link: %v", err)
		errOnDestroy := DestroyVethPair(&deviceInfo)
		if errOnDestroy != nil {
			klog.Errorf("Error occurred in destroying existing veth pair: %v", err)
			err = errors.Join(errOnDestroy, err)
		}
		return nil, err
	}

	deviceInfo.IfMac = ifLink.Attrs().HardwareAddr.String()
	deviceInfo.PeerIfMAC = peerLink.Attrs().HardwareAddr.String()

	return &deviceInfo, nil
}

// DestroyVethPair function to clean all the veth pairs created
// This function shall not log errors, but return it since it can
// be called to opportunistically clean up veth pairs.
func DestroyVethPair(dev *VEthPairDeviceInfo) error {
	nfLink, err := netlink.LinkByName(dev.IfName)
	if err != nil {
		return err
	}
	if err := netlink.LinkDel(nfLink); err != nil {
		return err
	}

	return nil
}

// SetSriovVlanId sets the VLAN ID for a specific virtual function (VF)
// As per ip-link (8) VLAN is special where it disable VLAN tagging and filtering
// Also incoming traffic will be filtered for the specific VLAN ID and will
// have all VLAN tags stripped before being passed to the VF.
func LinkSetVfVlanByName(ifname string, vfId int, vlanId int) error {
	link, err := netlink.LinkByName(ifname)
	if err != nil {
		klog.Errorf("Failed to get link by name '%s': %v", ifname, err)
		return err
	}

	// This is the equivalent of "ip link set dev <PF> vf <ID> vlan <VLAN>"
	if err := netlink.LinkSetVfVlan(link, vfId, vlanId); err != nil {
		klog.Errorf("Failed to set VLAN %d on VF %d of device '%s': %v", vlanId, vfId, ifname, err)
		return err
	}

	return nil
}

// netDevDeviceDir returns the device directory for a given network device name.
func readPCIsymbolicLink(fs afero.Fs, symbolicLink string) (string, error) {
	pciDevDir, err := fs.(afero.Symlinker).ReadlinkIfPossible(symbolicLink)
	if len(pciDevDir) <= 3 {
		return "", fmt.Errorf("could not find PCI Address")
	}
	return pciDevDir[3:], err
}

// VfPCIAddressFromVfIndex retrieves the PCI address for a virtual function (VF) based on its the PF and VF index.
func VfPCIAddressFromVfIndex(fs afero.Fs, pfName string, vfId int) (string, error) {
	symbolicLink := filepath.Join(NetSysDir, pfName, pcidevPrefix, fmt.Sprintf("%s%v",
		netDevVfDevicePrefix, vfId))
	pciAddress, err := readPCIsymbolicLink(fs, symbolicLink)
	if err != nil {
		err = fmt.Errorf("%v for VF %s%v of PF %s", err, netDevVfDevicePrefix, vfId, pfName)
		return "", err
	}
	return pciAddress, err
}
