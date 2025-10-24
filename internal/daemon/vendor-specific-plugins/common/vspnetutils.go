package vspnetutils

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/openshift/dpu-operator/internal/platform"
	"github.com/spf13/afero"
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

const (
	NetSysDir            = "/sys/class/net"
	pcidevPrefix         = "device"
	netDevVfDevicePrefix = "virtfn"
	retryInterval        = 500 * time.Millisecond
)

type VethPairKey struct {
	IfMac string // MAC address of the veth interface
}

type VEthPairDeviceInfo struct {
	VethKey   VethPairKey
	IfName    string // Interface Name of the veth
	PeerName  string // Interface Name of the peer veth
	PeerIfMAC string // MAC address of the Peer veth interface
}

type VfDeviceKey struct {
	PfInterfaceName string // Name of the PF interface
	Id              int    // VF ID starting from 0
}

type VfDeviceInfo struct {
	VfKey      VfDeviceKey
	PciAddress string // PCI address of the VF
	Vlan       int    // VLAN ID of the VF (0 is not vlan tagged)
	Allocated  bool   // Indicates if the VF is allocated or not
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

func CreateNfVethPair(idx int) (*VEthPairDeviceInfo, error) {
	//nfInterfaceName is the name of the interface on the Network Function attached to the container
	//dpInterfaceName is the name of the interface on the Data Plane OvS side
	nfInterfaceName := fmt.Sprintf("nf_if%d", idx)
	dpInterfaceName := fmt.Sprintf("dp_if%d", idx)

	return CreateVethPair(nfInterfaceName, dpInterfaceName)
}

func LinkSetUpDown(ifName string, up bool) error {
	link, err := netlink.LinkByName(ifName)
	if err != nil {
		klog.Error(err, "LinkSetUpDown: Error getting link by name", "ifName", ifName)
		return err
	}

	klog.Info("LinkSetUpDown: set interface", "ifName", ifName, "up", up)

	if up {
		err = netlink.LinkSetUp(link)
	} else {
		err = netlink.LinkSetDown(link)
	}

	if err != nil {
		klog.Error(err, "LinkSetUpDown: Error setting link for interface", "ifName", ifName, "up", up)
		return err
	}

	return nil
}

// CreateVethPair function to create a veth pair with the given index and InterfaceInfo
func CreateVethPair(ifname string, peername string) (*VEthPairDeviceInfo, error) {
	deviceInfo := VEthPairDeviceInfo{
		VethKey:   VethPairKey{IfMac: ""},
		IfName:    ifname,
		PeerName:  peername,
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

	deviceInfo.VethKey.IfMac = ifLink.Attrs().HardwareAddr.String()
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

// SetHwModeVepa sets the hardware mode of the specified interface to "vepa".
// In vepa mode, Data sent between HW ports is sent on the wire to the external switch.
// No bridging happens in hardware.
func SetPfHwModeVepa(pfName string) error {
	cmd := exec.Command("bridge", "link", "set", "dev", pfName, "hwmode", "vepa")

	// Run the command and capture any output or errors
	output, err := cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("Failed to set hwmode to vepa on %s: %v\nOutput: %s", pfName, err, string(output))
	}
	return err
}

// TODO: Use https://github.com/ovn-kubernetes/libovsdb/tree/main
func CreateOvSBridge(bridgeName string) error {
	cmd := exec.Command("chroot", "/host", "ovs-vsctl", "--may-exist", "add-br", bridgeName)
	klog.Infof("CreateOvSBridge(): %s", cmd.String())
	return cmd.Run()
}

// TODO: Use https://github.com/ovn-kubernetes/libovsdb/tree/main
func DeleteOvSBridge(bridgeName string) error {
	cmd := exec.Command("chroot", "/host", "ovs-vsctl", "del-br", bridgeName)
	klog.Infof("DeleteOvSBridge(): %s", cmd.String())
	return cmd.Run()
}

// TODO: Use https://github.com/ovn-kubernetes/libovsdb/tree/main
func AddInterfaceToOvSBridge(bridgeName string, ifname string) error {
	cmd := exec.Command("chroot", "/host", "ovs-vsctl", "--may-exist", "add-port", bridgeName, ifname)
	klog.Infof("AddInterfaceToOvSBridge(): %s", cmd.String())
	return cmd.Run()
}

// TODO: Use https://github.com/ovn-kubernetes/libovsdb/tree/main
func DeleteInterfaceFromOvSBridge(bridgeName string, ifname string) error {
	cmd := exec.Command("chroot", "/host", "ovs-vsctl", "del-port", bridgeName, ifname)
	klog.Infof("DeleteInterfaceFromOvSBridge(): %s", cmd.String())
	return cmd.Run()
}

// WaitForNetDevReady waits for a VF network device to be ready and available.
// It retries getting the netdev name from the PCIe address. Returns the list of interface names once they are available, or an error if timeout is reached.
func WaitForNetDevReady(platform platform.Platform, pcieAddr string, timeout time.Duration) (string, error) {
	var lastErr error
	var ifName string
	var elapsed time.Duration
	startTime := time.Now()

	klog.V(2).Infof("WaitForNetDevReady(): waiting for VF netdev to appear for PCIe address %s (timeout: %.2fs, interval: %v)", pcieAddr, timeout.Seconds(), retryInterval)

	for {
		ifName, lastErr = platform.GetNetDevNameFromPCIeAddr(pcieAddr)
		elapsed = time.Since(startTime)
		if lastErr == nil {
			klog.Infof("WaitForNetDevReady(): VF netdev ready for PCIe address %s: %v (elapsed: %.2fs)", pcieAddr, ifName, elapsed.Seconds())
			return ifName, nil
		}

		if elapsed > timeout {
			break
		}

		klog.V(2).Infof("WaitForNetDevReady(): VF netdev not ready for PCIe address %s, retrying... (elapsed: %.2fs, last error: %v)", pcieAddr, elapsed.Seconds(), lastErr)
		time.Sleep(retryInterval)
	}

	return "", fmt.Errorf("timeout waiting for VF netdev to appear for PCIe address %s after %.2fs: %w", pcieAddr, elapsed.Seconds(), lastErr)
}

// WaitForVfPciAddressReady waits for a VF PCI address to be available after SR-IOV VF creation.
// It retries getting the VF PCIe address from the VF index. Returns the PCIe address once it is available, or an error if timeout is reached.
func WaitForVfPciAddressReady(fs afero.Fs, pfName string, vfId int, timeout time.Duration) (string, error) {
	var lastErr error
	var pciAddr string
	var elapsed time.Duration
	startTime := time.Now()

	klog.V(2).Infof("WaitForVfPciAddressReady(): waiting for VF PCI address for PF %s VF %d (timeout: %.2fs, interval: %v)", pfName, vfId, timeout.Seconds(), retryInterval)

	for {
		pciAddr, lastErr = VfPCIAddressFromVfIndex(fs, pfName, vfId)
		elapsed = time.Since(startTime)
		if lastErr == nil && pciAddr != "" {
			klog.Infof("WaitForVfPciAddressReady(): VF PCI address ready: %s (elapsed: %.2fs)", pciAddr, elapsed.Seconds())
			return pciAddr, nil
		}

		if elapsed > timeout {
			break
		}

		klog.V(2).Infof("WaitForVfPciAddressReady(): VF PCI address not ready for PF %s VF %d, retrying... (elapsed: %.2fs, last error: %v)", pfName, vfId, elapsed.Seconds(), lastErr)
		time.Sleep(retryInterval)
	}

	return "", fmt.Errorf("timeout waiting for VF PCI address for PF %s VF %d after %.2fs: %w", pfName, vfId, elapsed.Seconds(), lastErr)
}

// WaitForLinkReady waits for a netdev link to be ready.
// It retries getting the netdev link by name. Returns the netdev link once it is available, or an error if timeout is reached.
func WaitForLinkReady(platform platform.Platform, pcieAddr string, timeout time.Duration) (string, netlink.Link, error) {
	var lastErr error
	var ifName string
	var elapsed time.Duration
	startTime := time.Now()

	klog.V(2).Infof("WaitForLinkReady(): waiting for pcie netdev link ready %s (timeout: %.2fs, interval: %v)", pcieAddr, timeout.Seconds(), retryInterval)

	for {
		// The reason why we need to get the NetDev name from PCIe address everytime is drivers can rename the netdev after SR-IOV VF creation. For example:
		// [587978.894260] ice 0000:ca:00.0: Enabling 8 VFs with 17 vectors and 16 queues per VF <= At this point, it is called eth0
		// ...
		// [587979.071864] iavf 0000:ca:01.0 ens7f0v0: renamed from eth0 <= Now it is called ens7f0v0
		// ...
		// [587979.640240] iavf 0000:ca:01.0 ens7f0v0: NIC Link is Up Speed is 25 Gbps Full Duplex <= ens7f0v0 is up
		ifName, lastErr = platform.GetNetDevNameFromPCIeAddr(pcieAddr)
		if lastErr == nil {
			vfLink, lastErr := netlink.LinkByName(ifName)
			if lastErr == nil {
				elapsed = time.Since(startTime)
				klog.Infof("WaitForLinkReady(): netdev link ready for pcie: %s ifName: %s (elapsed: %.2fs)", pcieAddr, ifName, elapsed.Seconds())
				return ifName, vfLink, nil
			}
		}

		elapsed = time.Since(startTime)
		if elapsed > timeout {
			break
		}

		klog.V(2).Infof("WaitForLinkReady(): netdev link not ready for pcie: %s ifName: %s, retrying... (elapsed: %.2fs, last error: %v)", pcieAddr, ifName, elapsed.Seconds(), lastErr)
		time.Sleep(retryInterval)
	}

	return "", nil, fmt.Errorf("timeout waiting for VF netdev link to be ready pcie: %s ifName: %s after %.2fs: %w", pcieAddr, ifName, elapsed.Seconds(), lastErr)
}
