package sriovconfig

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/sriovutils"
)

var (
	// DefaultCNIDir used for caching NetConf
	DefaultCNIDir = "/var/lib/cni/dpusriov"
)

// LoadConf parses and validates stdin netconf and returns NetConf object
func LoadConf(n *cnitypes.NetConf) (*cnitypes.NetConf, error) {
	// DeviceID takes precedence; if we are given a VF pciaddr then work from there
	if n.DeviceID != "" {
		// Get rest of the VF information
		pfName, vfID, err := getVfInfo(n.DeviceID)
		if err != nil {
			return nil, fmt.Errorf("LoadConf(): failed to get VF information: %q", err)
		}
		n.VFID = vfID
		n.Master = pfName
	} else {
		return nil, fmt.Errorf("LoadConf(): VF pci addr is required")
	}

	// Check if the device is already allocated.
	// This is to prevent issues where kubelet request to delete a pod and in the same time a new pod using the same
	// vf is started. we can have an issue where the cmdDel of the old pod is called AFTER the cmdAdd of the new one
	// This will block the new pod creation until the cmdDel is done.
	// FIXME: Fix Logging
	//logging.Debug("Check if the device is already allocated",
	//	"func", "LoadConf",
	//	"DefaultCNIDir", DefaultCNIDir,
	//	"n.DeviceID", n.DeviceID)
	allocator := sriovutils.NewPCIAllocator(DefaultCNIDir)
	isAllocated, err := allocator.IsAllocated(n.DeviceID)
	if err != nil {
		return n, err
	}

	if isAllocated {
		return n, fmt.Errorf("pci address %s is already allocated", n.DeviceID)
	}

	// Assuming VF is netdev interface; Get interface name(s)
	hostIFName, err := sriovutils.GetVFLinkName(n.DeviceID)
	if err != nil || hostIFName == "" {
		// VF interface not found; check if VF has dpdk driver
		hasDpdkDriver, err := sriovutils.HasDpdkDriver(n.DeviceID)
		if err != nil {
			return nil, fmt.Errorf("LoadConf(): failed to detect if VF %s has dpdk driver %q", n.DeviceID, err)
		}
		n.DPDKMode = hasDpdkDriver
	}

	if hostIFName != "" {
		n.OrigVfState.HostIFName = hostIFName
	}

	if hostIFName == "" && !n.DPDKMode {
		return nil, fmt.Errorf("LoadConf(): the VF %s does not have a interface name or a dpdk driver", n.DeviceID)
	}

	if n.Vlan == nil {
		vlan := 0
		n.Vlan = &vlan
	}

	// validate vlan id range
	if *n.Vlan < 0 || *n.Vlan > 4094 {
		return nil, fmt.Errorf("LoadConf(): vlan id %d invalid: value must be in the range 0-4094", *n.Vlan)
	}

	if n.VlanQoS == nil {
		qos := 0
		n.VlanQoS = &qos
	}

	// validate that VLAN QoS is in the 0-7 range
	if *n.VlanQoS < 0 || *n.VlanQoS > 7 {
		return nil, fmt.Errorf("LoadConf(): vlan QoS PCP %d invalid: value must be in the range 0-7", *n.VlanQoS)
	}

	// validate non-zero value for vlan id if vlan qos is set to a non-zero value
	if *n.VlanQoS != 0 && *n.Vlan == 0 {
		return nil, fmt.Errorf("LoadConf(): non-zero vlan id must be configured to set vlan QoS to a non-zero value")
	}

	if n.VlanProto == nil {
		proto := cnitypes.Proto8021q
		n.VlanProto = &proto
	}

	*n.VlanProto = strings.ToLower(*n.VlanProto)
	if *n.VlanProto != cnitypes.Proto8021ad && *n.VlanProto != cnitypes.Proto8021q {
		return nil, fmt.Errorf("LoadConf(): vlan Proto %s invalid: value must be '802.1Q' or '802.1ad'", *n.VlanProto)
	}

	// validate non-zero value for vlan id if vlan proto is set to 802.1ad
	if *n.VlanProto == cnitypes.Proto8021ad && *n.Vlan == 0 {
		return nil, fmt.Errorf("LoadConf(): non-zero vlan id must be configured to set vlan proto 802.1ad")
	}

	// validate that link state is one of supported values
	if n.LinkState != "" && n.LinkState != "auto" && n.LinkState != "enable" && n.LinkState != "disable" {
		return nil, fmt.Errorf("LoadConf(): invalid link_state value: %s", n.LinkState)
	}

	return n, nil
}

func getVfInfo(vfPci string) (string, int, error) {
	var vfID int

	pf, err := sriovutils.GetPfName(vfPci)
	if err != nil {
		return "", vfID, err
	}

	vfID, err = sriovutils.GetVfid(vfPci, pf)
	if err != nil {
		return "", vfID, err
	}

	return pf, vfID, nil
}

// LoadConfFromCache retrieves cached NetConf returns it along with a handle for removal
func LoadConfFromCache(containerID string, ifName string) (*cnitypes.NetConf, string, error) {
	netConf := &cnitypes.NetConf{}

	s := []string{containerID, ifName}
	cRef := strings.Join(s, "-")
	cRefPath := filepath.Join(DefaultCNIDir, cRef)

	netConfBytes, err := sriovutils.ReadScratchNetConf(cRefPath)
	if err != nil {
		return nil, "", fmt.Errorf("error reading cached NetConf in %s with name %s", DefaultCNIDir, cRef)
	}

	if err = json.Unmarshal(netConfBytes, netConf); err != nil {
		return nil, "", fmt.Errorf("failed to parse NetConf: %q", err)
	}

	return netConf, cRefPath, nil
}

// GetMacAddressForResult return the mac address we should report to the CNI call return object
// if the device is on kernel mode we report that one back
// if not we check the administrative mac address on the PF
// if it is set and is not zero, report it.
func GetMacAddressForResult(netConf *cnitypes.NetConf) string {
	if netConf.MAC != "" {
		return netConf.MAC
	}
	if !netConf.DPDKMode {
		return netConf.OrigVfState.EffectiveMAC
	}
	if netConf.OrigVfState.AdminMAC != "00:00:00:00:00:00" {
		return netConf.OrigVfState.AdminMAC
	}

	return ""
}
