package networkfn

import (
	"errors"
	"fmt"
	"net"

	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

func setDevTempName(dev netlink.Link) (netlink.Link, error) {
	// Generate a temp name with the interface index
	tempName := fmt.Sprintf("%s%d", "temp_", dev.Attrs().Index)

	// Rename to tempName
	if err := netlink.LinkSetName(dev, tempName); err != nil {
		return nil, fmt.Errorf("failed to rename device %q to %q: %v", dev.Attrs().Name, tempName, err)
	}

	// Get updated Link obj
	tempDev, err := netlink.LinkByName(tempName)
	if err != nil {
		return nil, fmt.Errorf("failed to find %q after rename to %q: %v", dev.Attrs().Name, tempName, err)
	}

	klog.Infof("setDevTempName: Set temp name %+v %q", tempDev, tempName)

	return tempDev, nil
}

func moveLinkInNetNamespace(hostDev netlink.Link, containerNs ns.NetNS, ifName string) (netlink.Link, error) {
	origLinkFlags := hostDev.Attrs().Flags
	origHostDevName := hostDev.Attrs().Name

	// Get the default namespace from the host
	defaultHostNs, err := ns.GetCurrentNS()
	if err != nil {
		return nil, fmt.Errorf("failed to get host namespace: %v", err)
	}

	// Devices can be renamed only when down
	if err = netlink.LinkSetDown(hostDev); err != nil {
		return nil, fmt.Errorf("failed to set %q down: %v", hostDev.Attrs().Name, err)
	}
	klog.Infof("moveLinkInNetNamespace: Set Link Down name %q", hostDev.Attrs().Name)

	// Restore original link state in case of error
	defer func() {
		if err != nil {
			// If the device is originally up, make sure to bring it up
			if origLinkFlags&net.FlagUp == net.FlagUp && hostDev != nil {
				_ = netlink.LinkSetUp(hostDev)
				klog.Infof("Error: moveLinkInNetNamespace: Set Link Up name %q", hostDev.Attrs().Name)
			}
		}
	}()

	hostDev, err = setDevTempName(hostDev)
	if err != nil {
		return nil, fmt.Errorf("failed to rename device %q to temporary name: %v", origHostDevName, err)
	}

	// Restore original netdev name in case of error
	defer func() {
		if err != nil && hostDev != nil {
			_ = netlink.LinkSetName(hostDev, origHostDevName)
			klog.Infof("Error: moveLinkInNetNamespace: Set Link Up on interface %q", hostDev.Attrs().Name)
		}
	}()

	// Move interface to the container network namespace
	if err = netlink.LinkSetNsFd(hostDev, int(containerNs.Fd())); err != nil {
		return nil, fmt.Errorf("failed to move %q to container ns: %v", hostDev.Attrs().Name, err)
	}
	klog.Infof("moveLinkInNetNamespace: Move interface %q to %q", hostDev.Attrs().Name, containerNs.Path())

	var contDev netlink.Link
	tempDevName := hostDev.Attrs().Name
	if err = containerNs.Do(func(_ ns.NetNS) error {
		var err error
		contDev, err = netlink.LinkByName(tempDevName)
		if err != nil {
			return fmt.Errorf("failed to find %q: %v", tempDevName, err)
		}

		// Move netdev back to host namespace in case of error
		defer func() {
			if err != nil {
				_ = netlink.LinkSetNsFd(contDev, int(defaultHostNs.Fd()))
				klog.Infof("Error: moveLinkInNetNamespace: Move interface %q to %q", contDev.Attrs().Name, defaultHostNs.Path())
				// we need to get updated link object as link was moved back to host namespace
				_ = defaultHostNs.Do(func(_ ns.NetNS) error {
					hostDev, _ = netlink.LinkByName(tempDevName)
					return nil
				})
			}
		}()

		// Save host device name into the container device's alias property
		if err = netlink.LinkSetAlias(contDev, origHostDevName); err != nil {
			return fmt.Errorf("failed to set alias to %q: %v", tempDevName, err)
		}
		klog.Infof("moveLinkInNetNamespace: Save original host name %q on %q", origHostDevName, contDev.Attrs().Name)

		// Rename container device to respect ifName coming from CNI netconf
		if err = netlink.LinkSetName(contDev, ifName); err != nil {
			return fmt.Errorf("failed to rename device %q to %q: %v", tempDevName, ifName, err)
		}
		klog.Infof("moveLinkInNetNamespace: Rename interface %q to %q", contDev.Attrs().Name, ifName)

		// Restore tempDevName in case of error
		defer func() {
			if err != nil {
				_ = netlink.LinkSetName(contDev, tempDevName)
				klog.Infof("Error: moveLinkInNetNamespace: Rename interface %q to %q", contDev.Attrs().Name, tempDevName)
			}
		}()

		// Bring container device up
		if err = netlink.LinkSetUp(contDev); err != nil {
			return fmt.Errorf("failed to set %q up: %v", ifName, err)
		}
		klog.Infof("moveLinkInNetNamespace: Set Link Up on interface %q", contDev.Attrs().Name)

		// Bring device down in case of error
		defer func() {
			if err != nil {
				_ = netlink.LinkSetDown(contDev)
				klog.Infof("Error: moveLinkInNetNamespace: Set Link Down on interface %q", contDev.Attrs().Name)
			}
		}()

		// Retrieve link again to get up-to-date name and attributes
		contDev, err = netlink.LinkByName(ifName)
		if err != nil {
			return fmt.Errorf("failed to find %q: %v", ifName, err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return contDev, nil
}

func moveLinkOutToHost(containerNs ns.NetNS, ifName string) error {
	// Get the default namespace from the host
	defaultHostNs, err := ns.GetCurrentNS()
	if err != nil {
		return err
	}
	defer defaultHostNs.Close()

	var tempName string
	var origDev netlink.Link
	err = containerNs.Do(func(_ ns.NetNS) error {
		dev, err := netlink.LinkByName(ifName)
		if err != nil {
			return fmt.Errorf("failed to find %q: %v", ifName, err)
		}
		origDev = dev

		// Devices can be renamed only when down
		if err = netlink.LinkSetDown(dev); err != nil {
			return fmt.Errorf("failed to set %q down: %v", ifName, err)
		}
		klog.Infof("moveLinkOutToHost: Set Link Down on interface %q", ifName)

		defer func() {
			// If moving the device to the host namespace fails, set its name back to ifName so that this
			// function can be retried. Also bring the device back up, unless it was already down before.
			if err != nil {
				_ = netlink.LinkSetName(dev, ifName)
				if dev.Attrs().Flags&net.FlagUp == net.FlagUp {
					_ = netlink.LinkSetUp(dev)
					klog.Infof("Error: moveLinkOutToHost: Set Link Up on interface %q", dev.Attrs().Name)
				}
			}
		}()

		newLink, err := setDevTempName(dev)
		if err != nil {
			return fmt.Errorf("failed to rename device %q to temporary name: %v", ifName, err)
		}
		dev = newLink
		tempName = dev.Attrs().Name

		if err = netlink.LinkSetNsFd(dev, int(defaultHostNs.Fd())); err != nil {
			return fmt.Errorf("failed to move %q to host netns: %v", tempName, err)
		}
		klog.Infof("moveLinkOutToHost: Move interface %q to %q", tempName, defaultHostNs.Path())
		return nil
	})

	if err != nil {
		return err
	}

	// Rename the device to its original name from the host namespace
	tempDev, err := netlink.LinkByName(tempName)
	if err != nil {
		return fmt.Errorf("failed to find %q in host namespace: %v", tempName, err)
	}

	// Use the device's alias to do this.
	if err = netlink.LinkSetName(tempDev, tempDev.Attrs().Alias); err != nil {
		// Move device back to container ns so it may be retired
		defer func() {
			_ = netlink.LinkSetNsFd(tempDev, int(containerNs.Fd()))
			_ = containerNs.Do(func(_ ns.NetNS) error {
				lnk, err := netlink.LinkByName(tempName)
				if err != nil {
					return err
				}
				_ = netlink.LinkSetName(lnk, ifName)
				if origDev.Attrs().Flags&net.FlagUp == net.FlagUp {
					_ = netlink.LinkSetUp(lnk)
				}
				return nil
			})
		}()
		return fmt.Errorf("failed to restore %q to original name %q: %v", tempName, tempDev.Attrs().Alias, err)
	}

	return nil
}

func CmdAdd(req *cnitypes.PodRequest) (*current.Result, error) {
	klog.Info("CmdAdd called for networkfn")

	conf := req.CNIConf

	klog.Infof("CmdAdd: conf %+v", conf)

	containerNs, err := ns.GetNS(req.Netns)
	if err != nil {
		return nil, fmt.Errorf("failed to open netns %+v: %v", containerNs, err)
	}
	defer containerNs.Close()

	klog.Infof("CmdAdd: Netns: %q", req.Netns)

	result := &current.Result{}
	var contDev netlink.Link

	// TODO: In the future we may want to support the following formats coming from the device plugin
	// pciAddr: For Netdev and DPDK use cases
	// auxDevices: Device plugins may allocate network device on a bus different than PCI
	// Also this code would not work for DPDK interfaces.
	hostDev, err := netlink.LinkByName(conf.DeviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to find host device: %v", err)
	}

	contDev, err = moveLinkInNetNamespace(hostDev, containerNs, req.IfName)
	if err != nil {
		return nil, fmt.Errorf("failed to move link %v", err)
	}

	result.Interfaces = []*current.Interface{{
		Name:    contDev.Attrs().Name,
		Mac:     contDev.Attrs().HardwareAddr.String(),
		Sandbox: containerNs.Path(),
	}}
	req.CNIConf.MAC = contDev.Attrs().HardwareAddr.String()

	if conf.IPAM.Type == "" {
		return result, nil
	}

	klog.Infof("CmdAdd: Running IPAM %q", conf.IPAM.Type)
	// Run the IPAM plugin and get back the config to apply
	r, err := ipam.ExecAdd(conf.IPAM.Type, req.CNIReq.Config)
	if err != nil {
		return nil, err
	}

	// Invoke ipam del if err to avoid ip leak
	defer func() {
		if err != nil {
			ipam.ExecDel(conf.IPAM.Type, req.CNIReq.Config)
		}
	}()

	// Convert the IPAM result was into the current Result type
	newResult, err := current.NewResultFromResult(r)
	if err != nil {
		return nil, err
	}

	if len(newResult.IPs) == 0 {
		return nil, errors.New("IPAM plugin returned missing IP config")
	}

	for _, ipc := range newResult.IPs {
		// All addresses apply to the container interface
		ipc.Interface = current.Int(0)
	}

	newResult.Interfaces = result.Interfaces

	err = containerNs.Do(func(_ ns.NetNS) error {
		return ipam.ConfigureIface(req.IfName, newResult)
	})
	if err != nil {
		return nil, err
	}

	newResult.DNS = conf.DNS

	return newResult, nil
}

func CmdDel(req *cnitypes.PodRequest) error {
	klog.Info("CmdDel called for networkfn")

	conf := req.CNIConf

	if req.Netns == "" {
		return nil
	}

	containerNs, err := ns.GetNS(req.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", req.Netns, err)
	}
	defer containerNs.Close()

	klog.Infof("CmdDel: Netns: %q", req.Netns)

	if conf.IPAM.Type != "" {
		if err := ipam.ExecDel(conf.IPAM.Type, req.CNIReq.Config); err != nil {
			return err
		}
	}

	klog.Infof("CmdDel: Running IPAM %q", conf.IPAM.Type)

	if err := moveLinkOutToHost(containerNs, req.IfName); err != nil {
		return err
	}

	return nil
}
