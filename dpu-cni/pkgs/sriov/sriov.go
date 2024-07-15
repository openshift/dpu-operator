package sriov

import (
	"errors"
	"fmt"
	"strings"

	"github.com/containernetworking/plugins/pkg/ns"
	"k8s.io/klog/v2"

	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/sriovconfig"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/sriovutils"
	"github.com/vishvananda/netlink"
)

const (
	// TODO: Some vendors have issues with Host VLANs, thus disable the feature for now
	host_vlans = false
)

type pciUtils interface {
	GetSriovNumVfs(ifName string) (int, error)
	GetVFLinkNamesFromVFID(pfName string, vfID int) ([]string, error)
	GetPciAddress(ifName string, vf int) (string, error)
	EnableArpAndNdiscNotify(ifName string) error
}

type pciUtilsImpl struct{}

func (p *pciUtilsImpl) GetSriovNumVfs(ifName string) (int, error) {
	return sriovutils.GetSriovNumVfs(ifName)
}

func (p *pciUtilsImpl) GetVFLinkNamesFromVFID(pfName string, vfID int) ([]string, error) {
	return sriovutils.GetVFLinkNamesFromVFID(pfName, vfID)
}

func (p *pciUtilsImpl) GetPciAddress(ifName string, vf int) (string, error) {
	return sriovutils.GetPciAddress(ifName, vf)
}

func (p *pciUtilsImpl) EnableArpAndNdiscNotify(ifName string) error {
	return sriovutils.EnableArpAndNdiscNotify(ifName)
}

// Manager provides interface invoke sriov nic related operations
type Manager interface {
	SetupVF(conf *cnitypes.NetConf, podifName string, netns ns.NetNS) error
	ReleaseVF(conf *cnitypes.NetConf, podifName string, netns ns.NetNS) error
	ResetVFConfig(conf *cnitypes.NetConf) error
	ApplyVFConfig(conf *cnitypes.NetConf) error
	FillOriginalVfInfo(conf *cnitypes.NetConf) error
	CmdAdd(req *cnitypes.PodRequest) (*current.Result, error)
	CmdDel(req *cnitypes.PodRequest) error
}

type sriovManager struct {
	nLink sriovutils.NetlinkManager
	utils pciUtils
}

// NewSriovManager returns an instance of SriovManager
func NewSriovManager() *sriovManager {
	return &sriovManager{
		nLink: &sriovutils.MyNetlink{},
		utils: &pciUtilsImpl{},
	}
}

// SetupVF sets up a VF in Pod netns
func (s *sriovManager) SetupVF(conf *cnitypes.NetConf, podifName string, netns ns.NetNS) error {
	linkName := conf.OrigVfState.HostIFName

	linkObj, err := s.nLink.LinkByName(linkName)
	if err != nil {
		return fmt.Errorf("error getting VF netdevice with name %s", linkName)
	}

	// Save the original effective MAC address before overriding it
	conf.OrigVfState.EffectiveMAC = linkObj.Attrs().HardwareAddr.String()

	// tempName used as intermediary name to avoid name conflicts
	tempName := fmt.Sprintf("%s%d", "temp_", linkObj.Attrs().Index)

	// 1. Set link down
	klog.Infof("1. Set link down %+v", linkObj)
	if err := s.nLink.LinkSetDown(linkObj); err != nil {
		return fmt.Errorf("failed to down vf device %q: %v", linkName, err)
	}

	// 2. Set temp name
	klog.Infof("2. Set temp name %+v %s", linkObj, tempName)
	if err := s.nLink.LinkSetName(linkObj, tempName); err != nil {
		return fmt.Errorf("error setting temp IF name %s for %s", tempName, linkName)
	}

	// 3. Change netns
	klog.Infof("3. Change netns %+v %d", linkObj, int(netns.Fd()))
	if err := s.nLink.LinkSetNsFd(linkObj, int(netns.Fd())); err != nil {
		return fmt.Errorf("failed to move IF %s to netns: %q", tempName, err)
	}

	if err := netns.Do(func(_ ns.NetNS) error {
		// 4. Set Pod IF name
		klog.Infof("4. Set Pod IF name %+v %s", linkObj, podifName)
		if err := s.nLink.LinkSetName(linkObj, podifName); err != nil {
			return fmt.Errorf("error setting container interface name %s for %s", linkName, tempName)
		}

		// 5. Enable IPv4 ARP notify and IPv6 Network Discovery notify
		// Error is ignored here because enabling this feature is only a performance enhancement.
		klog.Infof("5. Enable IPv4 ARP notify and IPv6 Network Discovery notify %s", podifName)
		_ = s.utils.EnableArpAndNdiscNotify(podifName)

		// 6. Set MAC address
		if conf.MAC != "" {
			klog.Infof("6. Set MAC address %+v %s %s", s.nLink, podifName, conf.MAC)
			err = sriovutils.SetVFEffectiveMAC(s.nLink, podifName, conf.MAC)
			if err != nil {
				return fmt.Errorf("failed to set netlink MAC address to %s: %v", conf.MAC, err)
			}
		}

		// 7. Bring IF up in Pod netns
		klog.Infof("7. Bring IF up in Pod netns %+v", linkObj)
		if err := s.nLink.LinkSetUp(linkObj); err != nil {
			return fmt.Errorf("error bringing interface up in container ns: %q", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("error setting up interface in container namespace: %q", err)
	}

	return nil
}

// ReleaseVF reset a VF from Pod netns and return it to init netns
func (s *sriovManager) ReleaseVF(conf *cnitypes.NetConf, podifName string, netns ns.NetNS) error {
	initns, err := ns.GetCurrentNS()
	if err != nil {
		return fmt.Errorf("failed to get init netns: %v", err)
	}

	return netns.Do(func(_ ns.NetNS) error {
		// get VF device
		klog.Infof("Get VF device %s", podifName)
		linkObj, err := s.nLink.LinkByName(podifName)
		if err != nil {
			return fmt.Errorf("failed to get netlink device with name %s: %q", podifName, err)
		}

		// shutdown VF device
		klog.Infof("Shutdown VF device %+v", linkObj)
		if err = s.nLink.LinkSetDown(linkObj); err != nil {
			return fmt.Errorf("failed to set link %s down: %q", podifName, err)
		}

		// rename VF device
		klog.Infof("Rename VF device %+v %s", linkObj, conf.OrigVfState.HostIFName)
		err = s.nLink.LinkSetName(linkObj, conf.OrigVfState.HostIFName)
		if err != nil {
			return fmt.Errorf("failed to rename link %s to host name %s: %q", podifName, conf.OrigVfState.HostIFName, err)
		}

		if conf.MAC != "" {
			// reset effective MAC address
			klog.Infof("Reset effective MAC address %+v %s %s", s.nLink, conf.OrigVfState.HostIFName, conf.OrigVfState.EffectiveMAC)
			err = sriovutils.SetVFEffectiveMAC(s.nLink, conf.OrigVfState.HostIFName, conf.OrigVfState.EffectiveMAC)
			if err != nil {
				return fmt.Errorf("failed to restore original effective netlink MAC address %s: %v", conf.OrigVfState.EffectiveMAC, err)
			}
		}

		// move VF device to init netns
		klog.Infof("Move VF device to init netns %+v %d", linkObj, int(initns.Fd()))
		if err = s.nLink.LinkSetNsFd(linkObj, int(initns.Fd())); err != nil {
			return fmt.Errorf("failed to move interface %s to init netns: %v", conf.OrigVfState.HostIFName, err)
		}

		return nil
	})
}

func getVfInfo(link netlink.Link, id int) *netlink.VfInfo {
	attrs := link.Attrs()
	for _, vf := range attrs.Vfs {
		if vf.ID == id {
			return &vf
		}
	}
	return nil
}

// ApplyVFConfig configure a VF with parameters given in NetConf
func (s *sriovManager) ApplyVFConfig(conf *cnitypes.NetConf) error {
	pfLink, err := s.nLink.LinkByName(conf.Master)
	if err != nil {
		return fmt.Errorf("failed to lookup master %q: %v", conf.Master, err)
	}
	// 1. Set vlan
	if host_vlans {
		if err = s.nLink.LinkSetVfVlanQosProto(pfLink, conf.VFID, *conf.Vlan, *conf.VlanQoS, cnitypes.VlanProtoInt[*conf.VlanProto]); err != nil {
			return fmt.Errorf("failed to set vf %d vlan configuration - id %d, qos %d and proto %s: %v", conf.VFID, *conf.Vlan, *conf.VlanQoS, *conf.VlanProto, err)
		}
	}

	// 2. Set mac address
	if conf.MAC != "" {
		// when we restore the original hardware mac address we may get a device or resource busy. so we introduce retry
		if err := sriovutils.SetVFHardwareMAC(s.nLink, conf.Master, conf.VFID, conf.MAC); err != nil {
			return fmt.Errorf("failed to set MAC address to %s: %v", conf.MAC, err)
		}
	}

	// 3. Set min/max tx link rate. 0 means no rate limiting. Support depends on NICs and driver.
	var minTxRate, maxTxRate int
	rateConfigured := false
	if conf.MinTxRate != nil {
		minTxRate = *conf.MinTxRate
		rateConfigured = true
	}

	if conf.MaxTxRate != nil {
		maxTxRate = *conf.MaxTxRate
		rateConfigured = true
	}

	if rateConfigured {
		if err = s.nLink.LinkSetVfRate(pfLink, conf.VFID, minTxRate, maxTxRate); err != nil {
			return fmt.Errorf("failed to set vf %d min_tx_rate to %d Mbps: max_tx_rate to %d Mbps: %v",
				conf.VFID, minTxRate, maxTxRate, err)
		}
	}

	// 4. Set spoofchk flag
	if conf.SpoofChk != "" {
		spoofChk := false
		if conf.SpoofChk == "on" {
			spoofChk = true
		}
		if err = s.nLink.LinkSetVfSpoofchk(pfLink, conf.VFID, spoofChk); err != nil {
			return fmt.Errorf("failed to set vf %d spoofchk flag to %s: %v", conf.VFID, conf.SpoofChk, err)
		}
	}

	// 5. Set trust flag
	if conf.Trust != "" {
		trust := false
		if conf.Trust == "on" {
			trust = true
		}
		if err = s.nLink.LinkSetVfTrust(pfLink, conf.VFID, trust); err != nil {
			return fmt.Errorf("failed to set vf %d trust flag to %s: %v", conf.VFID, conf.Trust, err)
		}
	}

	// 6. Set link state
	if conf.LinkState != "" {
		var state uint32
		switch conf.LinkState {
		case "auto":
			state = netlink.VF_LINK_STATE_AUTO
		case "enable":
			state = netlink.VF_LINK_STATE_ENABLE
		case "disable":
			state = netlink.VF_LINK_STATE_DISABLE
		default:
			// the value should have been validated earlier, return error if we somehow got here
			return fmt.Errorf("unknown link state %s when setting it for vf %d: %v", conf.LinkState, conf.VFID, err)
		}
		if err = s.nLink.LinkSetVfState(pfLink, conf.VFID, state); err != nil {
			return fmt.Errorf("failed to set vf %d link state to %d: %v", conf.VFID, state, err)
		}
	}

	return nil
}

// FillOriginalVfInfo fills the original vf info
func (s *sriovManager) FillOriginalVfInfo(conf *cnitypes.NetConf) error {
	pfLink, err := s.nLink.LinkByName(conf.Master)
	if err != nil {
		return fmt.Errorf("failed to lookup master %q: %v", conf.Master, err)
	}
	// Save current the VF state before modifying it
	vfState := getVfInfo(pfLink, conf.VFID)
	if vfState == nil {
		return fmt.Errorf("failed to find vf %d", conf.VFID)
	}
	conf.OrigVfState.FillFromVfInfo(vfState)

	return err
}

// ResetVFConfig reset a VF to its original state
func (s *sriovManager) ResetVFConfig(conf *cnitypes.NetConf) error {
	pfLink, err := s.nLink.LinkByName(conf.Master)
	if err != nil {
		return fmt.Errorf("failed to lookup master %q: %v", conf.Master, err)
	}

	if host_vlans {
		// Set 802.1q as default in case cache config does not have a value for vlan proto.
		if conf.OrigVfState.VlanProto == 0 {
			conf.OrigVfState.VlanProto = cnitypes.VlanProtoInt[cnitypes.Proto8021q]
		}

		if err = s.nLink.LinkSetVfVlanQosProto(pfLink, conf.VFID, conf.OrigVfState.Vlan, conf.OrigVfState.VlanQoS, conf.OrigVfState.VlanProto); err != nil {
			return fmt.Errorf("failed to set vf %d vlan configuration - id %d, qos %d and proto %d: %v", conf.VFID, conf.OrigVfState.Vlan, conf.OrigVfState.VlanQoS, conf.OrigVfState.VlanProto, err)
		}
	}

	// Restore spoofchk
	if conf.SpoofChk != "" {
		if err = s.nLink.LinkSetVfSpoofchk(pfLink, conf.VFID, conf.OrigVfState.SpoofChk); err != nil {
			return fmt.Errorf("failed to restore spoofchk for vf %d: %v", conf.VFID, err)
		}
	}

	// Restore the original administrative MAC address
	if conf.MAC != "" {
		// when we restore the original hardware mac address we may get a device or resource busy. so we introduce retry
		if err := sriovutils.SetVFHardwareMAC(s.nLink, conf.Master, conf.VFID, conf.OrigVfState.AdminMAC); err != nil {
			return fmt.Errorf("failed to restore original administrative MAC address %s: %v", conf.OrigVfState.AdminMAC, err)
		}
	}

	// Restore VF trust
	if conf.Trust != "" {
		if err = s.nLink.LinkSetVfTrust(pfLink, conf.VFID, conf.OrigVfState.Trust); err != nil {
			return fmt.Errorf("failed to set trust for vf %d: %v", conf.VFID, err)
		}
	}

	// Restore rate limiting
	if conf.MinTxRate != nil || conf.MaxTxRate != nil {
		if err = s.nLink.LinkSetVfRate(pfLink, conf.VFID, conf.OrigVfState.MinTxRate, conf.OrigVfState.MaxTxRate); err != nil {
			return fmt.Errorf("failed to disable rate limiting for vf %d %v", conf.VFID, err)
		}
	}

	// Restore link state to `auto`
	if conf.LinkState != "" {
		// Reset only when link_state was explicitly specified, to  accommodate for drivers / NICs
		// that don't support the netlink command (e.g. igb driver)
		if err = s.nLink.LinkSetVfState(pfLink, conf.VFID, conf.OrigVfState.LinkState); err != nil {
			return fmt.Errorf("failed to set link state to auto for vf %d: %v", conf.VFID, err)
		}
	}

	return nil
}

func (sm *sriovManager) CmdAdd(req *cnitypes.PodRequest) (*current.Result, error) {
	klog.Info("CmdAdd called")

	netConf, err := sriovconfig.LoadConf(req.CNIConf)
	if err != nil {
		return nil, fmt.Errorf("SRIOV-CNI failed to load netconf: %v", err)
	}

	// RuntimeConfig takes preference than envArgs.
	// This maintains compatibility of using envArgs
	// for MAC config.
	if netConf.RuntimeConfig.Mac != "" {
		netConf.MAC = netConf.RuntimeConfig.Mac
	}

	// Always use lower case for mac address
	netConf.MAC = strings.ToLower(netConf.MAC)

	netns, err := ns.GetNS(req.Netns)
	if err != nil {
		return nil, fmt.Errorf("failed to open netns %q: %v", netns, err)
	}
	defer netns.Close()

	// TODO: Some vendors have issues with Netlink, thus disable the feature for now
	// err = sm.FillOriginalVfInfo(netConf)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to get original vf information: %v", err)
	// }
	defer func() {
		if err != nil {
			err := netns.Do(func(_ ns.NetNS) error {
				_, err := netlink.LinkByName(req.IfName)
				return err
			})
			if err == nil {
				_ = sm.ReleaseVF(netConf, req.IfName, netns)
			}
			// Reset the VF if failure occurs before the netconf is cached
			_ = sm.ResetVFConfig(netConf)
		}
	}()
	if err := sm.ApplyVFConfig(netConf); err != nil {
		return nil, fmt.Errorf("SRIOV-CNI failed to configure VF %q", err)
	}

	result := &current.Result{}
	result.CNIVersion = netConf.CNIVersion
	result.Interfaces = []*current.Interface{{
		Name:    req.IfName,
		Sandbox: netns.Path(),
	}}

	if !netConf.DPDKMode {
		err = sm.SetupVF(netConf, req.IfName, netns)

		if err != nil {
			return nil, fmt.Errorf("failed to set up pod interface %q from the device %q: %v", req.IfName, netConf.Master, err)
		}
	}

	result.Interfaces[0].Mac = sriovconfig.GetMacAddressForResult(netConf)

	// run the IPAM plugin
	if netConf.IPAM.Type != "" {
		var r types.Result
		r, err = ipam.ExecAdd(netConf.IPAM.Type, req.CNIReq.Config)
		if err != nil {
			return nil, fmt.Errorf("failed to set up IPAM plugin type %q from the device %q: %v", netConf.IPAM.Type, netConf.Master, err)
		}

		defer func() {
			if err != nil {
				_ = ipam.ExecDel(netConf.IPAM.Type, req.CNIReq.Config)
			}
		}()

		// Convert the IPAM result into the current Result type
		var newResult *current.Result
		newResult, err = current.NewResultFromResult(r)
		if err != nil {
			return nil, err
		}

		if len(newResult.IPs) == 0 {
			err = errors.New("IPAM plugin returned missing IP config")
			return nil, err
		}

		newResult.Interfaces = result.Interfaces

		for _, ipc := range newResult.IPs {
			// All addresses apply to the container interface (move from host)
			ipc.Interface = current.Int(0)
		}

		if !netConf.DPDKMode {
			err = netns.Do(func(_ ns.NetNS) error {
				err := ipam.ConfigureIface(req.IfName, newResult)
				if err != nil {
					return err
				}

				/* After IPAM configuration is done, the following needs to handle the case of an IP address being reused by a different pods.
				 * This is achieved by sending Gratuitous ARPs and/or Unsolicited Neighbor Advertisements unconditionally.
				 * Although we set arp_notify and ndisc_notify unconditionally on the interface (please see EnableArpAndNdiscNotify()), the kernel
				 * only sends GARPs/Unsolicited NA when the interface goes from down to up, or when the link-layer address changes on the interfaces.
				 * These scenarios are perfectly valid and recommended to be enabled for optimal network performance.
				 * However for our specific case, which the kernel is unaware of, is the reuse of IP addresses across pods where each pod has a different
				 * link-layer address for it's SRIOV interface. The ARP/Neighbor cache residing in neighbors would be invalid if an IP address is reused.
				 * In order to update the cache, the GARP/Unsolicited NA packets should be sent for performance reasons. Otherwise, the neighbors
				 * may be sending packets with the incorrect link-layer address. Eventually, most network stacks would send ARPs and/or Neighbor
				 * Solicitation packets when the connection is unreachable. This would correct the invalid cache; however this may take a significant
				 * amount of time to complete.
				 *
				 * The error is ignored here because enabling this feature is only a performance enhancement.
				 */
				_ = sriovutils.AnnounceIPs(req.IfName, newResult.IPs)
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
		result = newResult
	}

	// Cache NetConf for CmdDel
	klog.Infof("Cache NetConf for CmdDel %s %+v", sriovconfig.DefaultCNIDir, netConf)
	if err = sriovutils.SaveNetConf(req.ContainerId, sriovconfig.DefaultCNIDir, req.IfName, netConf); err != nil {
		return nil, fmt.Errorf("error saving NetConf %q", err)
	}

	// Mark the pci address as in use.
	klog.Infof("Mark the PCI address as in use %s %s", sriovconfig.DefaultCNIDir, netConf.DeviceID)
	allocator := sriovutils.NewPCIAllocator(sriovconfig.DefaultCNIDir)
	if err = allocator.SaveAllocatedPCI(netConf.DeviceID, req.Netns); err != nil {
		return nil, fmt.Errorf("error saving the pci allocation for vf pci address %s: %v", netConf.DeviceID, err)
	}

	return result, nil
}

func (sm *sriovManager) CmdDel(req *cnitypes.PodRequest) error {
	klog.Info("CmdDel called")

	netConf, cRefPath, err := sriovconfig.LoadConfFromCache(req.ContainerId, req.IfName)
	if err != nil {
		// If cmdDel() fails, cached netconf is cleaned up by
		// the followed defer call. However, subsequence calls
		// of cmdDel() from kubelet fail in a dead loop due to
		// cached netconf doesn't exist.
		// Return nil when LoadConfFromCache fails since the rest
		// of cmdDel() code relies on netconf as input argument
		// and there is no meaning to continue.
		klog.Warningf("Cannot load config file from cache: %v", err)
		return nil
	}

	defer func() {
		if err == nil && cRefPath != "" {
			_ = sriovutils.CleanCachedNetConf(cRefPath)
		}
	}()

	if netConf.IPAM.Type != "" {
		err = ipam.ExecDel(netConf.IPAM.Type, req.CNIReq.Config)
		if err != nil {
			return err
		}
	}

	// https://github.com/kubernetes/kubernetes/pull/35240
	if req.Netns == "" {
		return nil
	}

	// Verify VF ID existence.
	if _, err := sriovutils.GetVfid(netConf.DeviceID, netConf.Master); err != nil {
		return fmt.Errorf("cmdDel() error obtaining VF ID: %q", err)
	}

	/* ResetVFConfig resets a VF administratively. We must run ResetVFConfig
	   before ReleaseVF because some drivers will error out if we try to
	   reset netdev VF with trust off. So, reset VF MAC address via PF first.
	*/
	if err := sm.ResetVFConfig(netConf); err != nil {
		return fmt.Errorf("cmdDel() error reseting VF: %q", err)
	}

	if !netConf.DPDKMode {
		netns, err := ns.GetNS(req.Netns)
		if err != nil {
			// according to:
			// https://github.com/kubernetes/kubernetes/issues/43014#issuecomment-287164444
			// if provided path does not exist (e.x. when node was restarted)
			// plugin should silently return with success after releasing
			// IPAM resources
			_, ok := err.(ns.NSPathNotExistErr)
			if ok {
				return nil
			}

			return fmt.Errorf("failed to open netns %s: %q", netns, err)
		}
		defer netns.Close()

		if err = sm.ReleaseVF(netConf, req.IfName, netns); err != nil {
			return err
		}
	}
	req.CNIConf.VFID = netConf.VFID

	// Mark the pci address as released
	klog.Infof("Mark the PCI address as released %s %s", sriovconfig.DefaultCNIDir, netConf.DeviceID)
	allocator := sriovutils.NewPCIAllocator(sriovconfig.DefaultCNIDir)
	if err = allocator.DeleteAllocatedPCI(netConf.DeviceID); err != nil {
		return fmt.Errorf("error cleaning the pci allocation for vf pci address %s: %v", netConf.DeviceID, err)
	}

	return nil
}
