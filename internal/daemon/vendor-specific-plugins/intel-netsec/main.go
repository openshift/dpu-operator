package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"sync"

	"github.com/go-logr/logr"
	"github.com/jaypipes/ghw"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	vspnetutils "github.com/openshift/dpu-operator/internal/daemon/vendor-specific-plugins/common"
	"github.com/openshift/dpu-operator/internal/platform"
	"github.com/openshift/dpu-operator/internal/utils"
	opi "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"github.com/spf13/afero"
	"github.com/vishvananda/netlink"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	Version                              string = "0.0.1"
	IPv6AddrDpu                          string = "fe80::1"
	IPv6AddrHost                         string = "fe80::2"
	DefaultPort                          int32  = 8085
	IntelVendorID                        string = "8086"
	IntelNetSecHostVfDeviceID            string = "1889" // Intel Corporation Ethernet Adaptive Virtual Function
	IntelNetSecDpuSFPf0PCIeAddress       string = "0000:f4:00.0"
	IntelNetSecDpuSFPf1PCIeAddress       string = "0000:f4:00.1"
	IntelNetSecDpuBackplanef2PCIeAddress string = "0000:f4:00.2"
	IntelNetSecDpuBackplanef3PCIeAddress string = "0000:f4:00.3"
	VlanOffset                           int    = 2 // Vlan ID offset since vlan 0 is untagged and to reserve vlan 1
	NoOfVethPairs                        int    = 2 // Only support 1 Network Function with 2 pairs (TODO: Need CRD)
	MaxChains                            int    = 1 // Only support 1 Chain of Network Functions
	OvSBridgeName                        string = "br-secondary"
)

type intelNetSecVspServer struct {
	// Common interfaces for VSP
	pb.UnimplementedLifeCycleServiceServer
	pb.UnimplementedNetworkFunctionServiceServer
	pb.UnimplementedDeviceServiceServer
	opi.UnimplementedBridgePortServiceServer
	log            logr.Logger
	grpcServer     *grpc.Server
	wg             sync.WaitGroup
	done           chan error
	fs             afero.Fs
	startedWg      sync.WaitGroup
	pathManager    utils.PathManager
	version        string
	isDPUMode      bool
	platform       platform.Platform
	dpuIdentifier  plugin.DpuIdentifier // DPU identifier used to identify the DPU device by Serial Number from the Host
	dpuPcieAddress string               // PCIe address of the DPU device (function 0) on the Host
	// Intel NetSec Accelerator specific interfaces
	vfCnt              int
	vethTunnelPairDevs map[vspnetutils.VethPairKey]*vspnetutils.VEthPairDeviceInfo
	vfDevs             map[vspnetutils.VfDeviceKey]*vspnetutils.VfDeviceInfo
	// Network Functions
	numNfs                int
	serviceFunctionChains map[int]*intelNetSecServiceFunctionChain
}

// This datastructure represents a pseudo VF representor. Representor lanes are created with layer2 VLAN isolation.
type intelNetSecVfRepDev struct {
	vfRepDev    *vspnetutils.VfDeviceInfo
	hostSideMac string
}

type intelNetSecServiceFunctionChain struct {
	vfRepDevs map[vspnetutils.VfDeviceKey]*intelNetSecVfRepDev
	sfpVfDevs *vspnetutils.VfDeviceInfo
}

// getVFs retrieves the VF PCIe addresses for the given PF PCIe address for Intel NetSec accelerator devices.
func (vsp *intelNetSecVspServer) getVFs(pfPCIeAddress string) ([]string, error) {
	var pciVFAddresses []string

	devices, err := vsp.platform.PciDevices()
	if err != nil {
		return nil, fmt.Errorf("failed to get PCI devices: %v", err)
	}

	bus := ghw.PCIAddressFromString(pfPCIeAddress).Bus
	for _, pci := range devices {
		if ghw.PCIAddressFromString(pci.Address).Bus == bus {
			if pci.Vendor.ID == IntelVendorID &&
				pci.Product.ID == IntelNetSecHostVfDeviceID {
				pciVFAddresses = append(pciVFAddresses, pci.Address)
			}
		}
	}

	numVfs := len(pciVFAddresses)
	vsp.log.V(2).Info("getVFs(): found VFs", "NumVFs", numVfs, "DpuPcieAddress", vsp.dpuPcieAddress)
	return pciVFAddresses, nil
}

// configureCommChannelIPs configures the communication channel IPs on the Host or DPU based on the mode.
func (vsp *intelNetSecVspServer) configureCommChannelIPs(dpuMode bool) (pb.IpPort, error) {
	var ifName string
	var addr string
	var err error
	if dpuMode {
		// All NetSec DPU devices have the same internal PCIe Addresses. Netdev names can change with each RHEL release.
		ifNames, err := vsp.platform.GetNetDevNameFromPCIeAddr(IntelNetSecDpuBackplanef2PCIeAddress)
		if err != nil {
			vsp.log.Error(err, "Error getting netdev name from PCIe address in DPU mode", "PCIeAddress", IntelNetSecDpuBackplanef2PCIeAddress)
			return pb.IpPort{}, err
		}

		if len(ifNames) != 1 {
			err = fmt.Errorf("expected exactly 1 interface for PCIe address %s, got %v", IntelNetSecDpuBackplanef2PCIeAddress, ifNames)
			vsp.log.Error(err, "Error getting netdev name from PCIe address in DPU mode", "PCIeAddress", IntelNetSecDpuBackplanef2PCIeAddress)
			return pb.IpPort{}, err
		}

		ifName = ifNames[0]
		addr = IPv6AddrDpu
	} else {
		ifNames, err := vsp.platform.GetNetDevNameFromPCIeAddr(vsp.dpuPcieAddress)
		if err != nil {
			vsp.log.Error(err, "Error getting netdev name from PCIe address in Host mode", "PCIeAddress", vsp.dpuPcieAddress)
			return pb.IpPort{}, err
		}

		if len(ifNames) != 1 {
			err = fmt.Errorf("expected exactly 1 interface for PCIe address %s, got %v", vsp.dpuPcieAddress, ifNames)
			vsp.log.Error(err, "Error getting netdev name from PCIe address in Host mode", "PCIeAddress", vsp.dpuPcieAddress)
			return pb.IpPort{}, err
		}

		ifName = ifNames[0]
		addr = IPv6AddrHost
	}

	vsp.log.Info("configureCommChannelIPs(): DpuMode", "DpuMode", dpuMode, "IfName", ifName, "Addr", addr)

	err = vspnetutils.EnableIPV6LinkLocal(vsp.fs, ifName, addr)
	addr = IPv6AddrDpu
	if err != nil {
		vsp.log.Error(err, "Error enabling IPv6 Link Local Address", "IfName", ifName, "Addr", addr)
		return pb.IpPort{}, err
	}
	var connStr string
	if dpuMode {
		connStr = "[" + addr + "%" + ifName + "]"
	} else {
		connStr = "[" + addr + "%25" + ifName + "]"
	}

	vsp.log.Info("configureCommChannelIPs(): IPv6 Link Local Address Enabled", "IfName", ifName, "ConnectionString", connStr)

	return pb.IpPort{
		Ip:   connStr,
		Port: DefaultPort,
	}, nil
}

// GetDpuPcieAddress retrieves the PCIe address of the DPU based on the provided DPU identifier.
// We only return the PCIe address of the first function (function 0) of the DPU device.
func (vsp *intelNetSecVspServer) GetDpuPcieAddress(dpuIdentifier plugin.DpuIdentifier) (string, error) {
	if vsp.isDPUMode {
		return "", nil
	}

	devices, err := vsp.platform.PciDevices()
	if err != nil {
		return "", fmt.Errorf("Error getting devices: %v", err)
	}

	for _, pci := range devices {
		if pci.Vendor.ID == platform.IntelVendorID && pci.Product.ID == platform.IntelNetSecHostDeviceID {
			serial, err := vsp.platform.ReadDeviceSerialNumber(pci)
			if err != nil {
				// Intel NetSec Network Devices should return a serial number.
				return "", fmt.Errorf("error reading device serial number for %s: %v", pci.Address, err)
			}
			if plugin.DpuIdentifier(serial) == dpuIdentifier {
				// Intel NetSec Accelerator's first device should have function 0.
				if ghw.PCIAddressFromString(pci.Address).Function == "0" {
					return pci.Address, nil
				}
			}
		}
	}

	return "", fmt.Errorf("DPU PCIe address not found for identifier: %s", dpuIdentifier)
}

// Intel NetSec Accelerator uses OvS as the data plane where the first SFP port is added to the bridge.
// TODO: Handle 2 SFP ports in the future.
func (vsp *intelNetSecVspServer) initOvSDataPlane(bridgeName string) error {
	vsp.log.Info("Initializing OvS Data Plane")

	err := vspnetutils.CreateOvSBridge(bridgeName)
	if err != nil {
		vsp.log.Error(err, "Error occurred in creating Bridge", "BridgeName", bridgeName)
		return err
	}

	vsp.log.Info("OvS Bridge Created Successfully", "BridgeName", bridgeName)

	// TODO: Need to resolve several issues:
	// 1. IntelNetSecDpuSFPf0PCIeAddress can be used for the cluster network.
	// 2. IntelNetSecDpuSFPf1PCIeAddress we use the second 25Gbe interface for now.
	// With 1) you can't have the same interface on 2 bridges.
	sfpPortIfNames, err := vsp.platform.GetNetDevNameFromPCIeAddr(IntelNetSecDpuSFPf1PCIeAddress)
	if err != nil {
		vsp.log.Error(err, "Error occurred in getting SFP Port Interface Name", "PCIeAddress", IntelNetSecDpuSFPf1PCIeAddress)
		return err
	}

	if len(sfpPortIfNames) != 1 {
		return fmt.Errorf("expected exactly 1 interface for PCIe address %s, got %v", IntelNetSecDpuSFPf1PCIeAddress, sfpPortIfNames)
	}

	sfpPortIfName := sfpPortIfNames[0]

	// The following code takes the second SFP port's first VF and adds this VF to the OvS bridge.
	// The reason why a VF is chosen is to support VLAN use cases and multiple independent chains.
	// TODO: When more chains are supported we need to put the following in a loop
	vfPcieAddr, err := vspnetutils.VfPCIAddressFromVfIndex(vsp.fs, sfpPortIfName, 0)
	if err != nil {
		vsp.log.Error(err, "Error getting VF PCI address", "VFID", 0, "InterfaceName", sfpPortIfName)
		return err
	}

	vfIfNames, err := vsp.platform.GetNetDevNameFromPCIeAddr(vfPcieAddr)
	if err != nil {
		vsp.log.Error(err, "Error occurred in getting SFP Port Interface Name", "PCIeAddress", IntelNetSecDpuSFPf1PCIeAddress)
		return err
	}

	if len(vfIfNames) != 1 {
		return fmt.Errorf("expected exactly 1 interface for PCIe address %s, got %v", vfPcieAddr, vfIfNames)
	}

	vfIfName := vfIfNames[0]

	sfpPortlink, err := netlink.LinkByName(sfpPortIfName)
	if err != nil {
		vsp.log.Error(err, "Failed to get link by name", "sfpPortIfName", sfpPortIfName)
		return err
	}

	if err := netlink.LinkSetVfSpoofchk(sfpPortlink, 0, false); err != nil {
		vsp.log.Error(err, "Failed to set spoof check off for VF", "VfID", 0, "sfpPortIfName", sfpPortIfName)
		return err
	}

	if err := netlink.LinkSetVfTrust(sfpPortlink, 0, true); err != nil {
		vsp.log.Error(err, "Failed to set trust on for VF", "VfID", 0, "sfpPortIfName", sfpPortIfName)
		return err
	}

	vsp.serviceFunctionChains[0] = &intelNetSecServiceFunctionChain{
		sfpVfDevs: &vspnetutils.VfDeviceInfo{
			VfKey: vspnetutils.VfDeviceKey{
				PfInterfaceName: sfpPortIfName,
				Id:              0,
			},
			PciAddress: vfPcieAddr,
			Vlan:       0,
			Allocated:  true,
		},
		vfRepDevs: make(map[vspnetutils.VfDeviceKey]*intelNetSecVfRepDev),
	}

	err = vspnetutils.AddInterfaceToOvSBridge(bridgeName, vfIfName)
	if err != nil {
		vsp.log.Error(err, "Error occurred in adding SFP Port Interface to Bridge", "BridgeName", bridgeName, "SfpPortIfName", sfpPortIfNames[0])
		return err
	}

	vsp.log.Info("SFP Port Interface Added to Bridge Successfully", "BridgeName", bridgeName, "SfpPortIfName", sfpPortIfNames[0])
	return nil
}

// createVethPairs creates a the veth pairs for DPU mode used by Network Functions.
func (vsp *intelNetSecVspServer) createVethPairs() error {
	for idx := 0; idx < NoOfVethPairs; idx++ {
		pair, err := vspnetutils.CreateNfVethPair(idx)
		if err != nil {
			vsp.log.Error(err, "Error creating veth pair", "Index", idx)
			return err
		}
		vsp.vethTunnelPairDevs[pair.VethKey] = pair
	}

	vsp.log.Info("createVethPairs(): Created veth pairs", "Count", len(vsp.vethTunnelPairDevs))
	return nil
}

func (vsp *intelNetSecVspServer) Init(ctx context.Context, in *pb.InitRequest) (*pb.IpPort, error) {
	var err error
	vsp.log.Info("Received Init() request", "DpuMode", in.DpuMode, "DpuIdentifier", in.DpuIdentifier)
	vsp.isDPUMode = in.DpuMode
	vsp.dpuIdentifier = plugin.DpuIdentifier(in.DpuIdentifier)

	vsp.dpuPcieAddress, err = vsp.GetDpuPcieAddress(vsp.dpuIdentifier)
	if err != nil {
		vsp.log.Error(err, "Error getting DPU PCIe address", "DpuIdentifier", vsp.dpuIdentifier)
		return nil, err
	}

	ipPort, err := vsp.configureCommChannelIPs(in.DpuMode)
	if err != nil {
		vsp.log.Error(err, "Error configuring IP", "DpuMode", in.DpuMode, "DpuPcieAddress", vsp.dpuPcieAddress)
		return nil, err
	}

	if vsp.isDPUMode {
		err = vsp.createVethPairs()
		if err != nil {
			vsp.log.Error(err, "Error creating veth pairs")
			return nil, err
		}

		err = vsp.initOvSDataPlane(OvSBridgeName)
		if err != nil {
			vsp.log.Error(err, "Error initializing OvS Data Plane")
			return nil, err
		}
	}

	vsp.log.Info("Init() completed", "DpuPcieAddress", vsp.dpuPcieAddress, "IP", ipPort.Ip, "Port", ipPort.Port)

	return &pb.IpPort{
		Ip:   ipPort.Ip,
		Port: ipPort.Port,
	}, nil
}

// GetDevices retrieves the list of devices (VFs or VETH Tunnel Pair Devices) based on the mode (DPU or Host).
func (vsp *intelNetSecVspServer) GetDevices(ctx context.Context, in *pb.Empty) (*pb.DeviceListResponse, error) {
	vsp.log.V(2).Info("Received GetDevices() request")
	devices := make(map[string]*pb.Device)

	if !vsp.isDPUMode {
		pfPcieAddress := vsp.dpuPcieAddress

		vfs, err := vsp.getVFs(pfPcieAddress)
		if err != nil {
			vsp.log.Error(err, "Error getting VFs for PF PCIe address", "PFPCIeAddress", pfPcieAddress)
			return nil, err
		}

		for _, vf := range vfs {
			vsp.log.V(2).Info("Adding device to the response", "VF", vf)
			devices[vf] = &pb.Device{
				ID:     vf,
				Health: "Healthy",
			}
		}
	} else {
		for _, tunnelPairDev := range vsp.vethTunnelPairDevs {
			vsp.log.V(2).Info("Adding device to the response", "TunnelPairDevice", tunnelPairDev)
			devices[tunnelPairDev.IfName] = &pb.Device{
				ID:     tunnelPairDev.IfName,
				Health: "Healthy",
			}
		}
	}

	return &pb.DeviceListResponse{
		Devices: devices,
	}, nil
}

// getVFName function to get the VF Name of the given OPI BridgePortName on DPU
func (vsp *intelNetSecVspServer) getConnectedVf(OPIBridgePortName string) (*vspnetutils.VfDeviceInfo, error) {
	// Use regex to get VFId from BridgePortName ex: host0-7
	re := regexp.MustCompile(`^host(\d+)-(\d+)$`)
	matches := re.FindStringSubmatch(OPIBridgePortName)
	if matches == nil {
		return nil, errors.New("OPI BridgePortName does not match expected format")
	}

	pfid, err := strconv.Atoi(matches[1])
	if err != nil {
		return nil, err
	}
	vfId, err := strconv.Atoi(matches[2])
	if err != nil {
		return nil, err
	}

	vsp.log.Info("PFID and VFID extracted", "PFID", pfid, "VFID", vfId)

	// TODO: Handle multiple PFs propely in the future. Only pfid 0 is supported for now.
	var backPlanePcieAddr string
	if pfid == 0 {
		backPlanePcieAddr = IntelNetSecDpuBackplanef2PCIeAddress
	} else {
		//backPlanePcieAddr = IntelNetSecDpuBackplanef3PCIeAddress
		err = fmt.Errorf("PFID %d is not supported", pfid)
		vsp.log.Error(err, "getConnectedVf() called with unsupported PFID")
		return nil, err
	}

	pfIfNames, err := vsp.platform.GetNetDevNameFromPCIeAddr(backPlanePcieAddr)
	if err != nil {
		vsp.log.Error(err, "Error getting netdev name from PCIe address", "PCIeAddress", backPlanePcieAddr)
		return nil, err
	}

	if len(pfIfNames) != 1 {
		err = fmt.Errorf("expected exactly 1 interface for PCIe address %s, got %v", backPlanePcieAddr, pfIfNames)
		vsp.log.Error(err, "Error getting netdev name from PCIe address", "PCIeAddress", backPlanePcieAddr)
		return nil, err
	}

	pfIfName := pfIfNames[0]

	// For Intel NetSec, PF ID is always 0 and VF ID is mapped one to one.
	key := vspnetutils.VfDeviceKey{
		PfInterfaceName: pfIfName,
		Id:              vfId,
	}

	vfDevInfo, ok := vsp.vfDevs[key]
	if !ok {
		err = fmt.Errorf("VF Device not found PFInterfaceName: %s, VFId: %d", pfIfName, vfId)
		vsp.log.Error(err, "getConnectedVf() could not find VF Device")
		return nil, err
	}

	return vfDevInfo, nil
}

func (vsp *intelNetSecVspServer) CreateBridgePort(ctx context.Context, in *opi.CreateBridgePortRequest) (*opi.BridgePort, error) {
	vsp.log.Info("Received CreateBridgePort() request", "BridgePortId", in.BridgePortId, "BridgePort", in.BridgePort)

	vfDevice, err := vsp.getConnectedVf(in.BridgePort.Name)
	if err != nil {
		vsp.log.Error(err, "Error getting connected VF for BridgePort", "BridgePortName", in.BridgePort.Name)
		return nil, err
	}

	if vfDevice.Allocated == true {
		err = fmt.Errorf("VF Device is already allocated PFInterfaceName: %s, VFId: %d", vfDevice.VfKey.PfInterfaceName, vfDevice.VfKey.Id)
		vsp.log.Error(err, "CreateBridgePort() VF Device is already allocated")
		return nil, err
	}

	vfIfNames, err := vsp.platform.GetNetDevNameFromPCIeAddr(vfDevice.PciAddress)
	if err != nil {
		vsp.log.Error(err, "Error getting netdev name from PCIe address", "PCIeAddress", vfDevice.PciAddress)
		return nil, err
	}

	if len(vfIfNames) != 1 {
		err = fmt.Errorf("expected exactly 1 interface for PCIe address %s, got %v", vfDevice.PciAddress, vfIfNames)
		vsp.log.Error(err, "Error getting netdev name from PCIe address", "PCIeAddress", vfDevice.PciAddress)
		return nil, err
	}

	vfIfName := vfIfNames[0]

	err = vspnetutils.AddInterfaceToOvSBridge(OvSBridgeName, vfIfName)
	if err != nil {
		vsp.log.Error(err, "Error adding VF to OvS Bridge", "BridgePortName", in.BridgePort.Name, "vfIfName", vfIfName)
		return nil, err
	}

	macStr := net.HardwareAddr(in.BridgePort.Spec.MacAddress).String()
	_, exists := vsp.serviceFunctionChains[0].vfRepDevs[vfDevice.VfKey]
	if exists {
		err = fmt.Errorf("VF Device is already exists PFInterfaceName: %s, VFId: %d", vfDevice.VfKey.PfInterfaceName, vfDevice.VfKey.Id)
		vsp.log.Error(err, "CreateBridgePort() VF Device is already exists")
		return nil, err
	} else {
		vsp.serviceFunctionChains[0].vfRepDevs[vfDevice.VfKey] = &intelNetSecVfRepDev{
			vfRepDev:    vfDevice,
			hostSideMac: macStr,
		}
		vfDevice.Allocated = true
		vsp.log.Info("CreateBridgePort(): Added VF to Service Function Chain", "BridgePortName", in.BridgePort.Name, "vfIfName", vfIfName, "vfDevice", vfDevice, "hostSideMac", macStr)
	}

	vsp.log.Info("CreateBridgePort(): Added VF to OvS Bridge", "BridgePortName", in.BridgePort.Name, "vfIfName", vfIfName)

	return &opi.BridgePort{}, nil
}

func (vsp *intelNetSecVspServer) DeleteBridgePort(ctx context.Context, in *opi.DeleteBridgePortRequest) (*emptypb.Empty, error) {
	vsp.log.Info("Received DeleteBridgePort() request", "Name", in.Name, "AllowMissing", in.AllowMissing)

	vfDevice, err := vsp.getConnectedVf(in.Name)
	if err != nil {
		vsp.log.Error(err, "Error getting connected VF for BridgePort", "BridgePortName", in.Name)
		return nil, err
	}

	vfIfNames, err := vsp.platform.GetNetDevNameFromPCIeAddr(vfDevice.PciAddress)
	if err != nil {
		vsp.log.Error(err, "Error getting netdev name from PCIe address", "PCIeAddress", vfDevice.PciAddress)
		return nil, err
	}

	if len(vfIfNames) != 1 {
		err = fmt.Errorf("expected exactly 1 interface for PCIe address %s, got %v", vfDevice.PciAddress, vfIfNames)
		vsp.log.Error(err, "Error getting netdev name from PCIe address", "PCIeAddress", vfDevice.PciAddress)
		return nil, err
	}

	vfIfName := vfIfNames[0]

	err = vspnetutils.DeleteInterfaceFromOvSBridge(OvSBridgeName, vfIfName)
	if err != nil {
		vsp.log.Error(err, "Error adding VF to OvS Bridge", "BridgePortName", in.Name, "vfIfName", vfIfName)
		return nil, err
	}

	vfDeviceFromSFC, ok := vsp.serviceFunctionChains[0].vfRepDevs[vfDevice.VfKey]
	if !ok {
		err = fmt.Errorf("VF Device not found PFInterfaceName: %s, VFId: %d", vfDevice.VfKey.PfInterfaceName, vfDevice.VfKey.Id)
		vsp.log.Error(err, "DeleteBridgePort() could not find VF Device")
		return nil, err
	}

	if vfDeviceFromSFC.vfRepDev.Allocated == false {
		err = fmt.Errorf("VF Device is not allocated PFInterfaceName: %s, VFId: %d", vfDevice.VfKey.PfInterfaceName, vfDevice.VfKey.Id)
		vsp.log.Error(err, "DeleteBridgePort() VF Device is not allocated")
		return nil, err
	}

	vfDeviceFromSFC.vfRepDev.Allocated = false
	delete(vsp.serviceFunctionChains[0].vfRepDevs, vfDevice.VfKey)

	vsp.log.Info("DeleteBridgePort(): Deleted VF from OvS Bridge", "BridgePortName", in.Name, "vfIfName", vfIfName)

	return nil, nil
}

func (vsp *intelNetSecVspServer) vethUpAndAddToBridge(veth *vspnetutils.VEthPairDeviceInfo) error {
	// The peer interface is the dataplane interface that needs to be up and added to the bridge.
	vethLink, err := netlink.LinkByName(veth.PeerName)
	if err != nil {
		vsp.log.Error(err, "Error getting inport Veth", "InportVeth", veth.IfName, "PeerName", veth.PeerName)
		return err
	}

	vsp.log.Info("vethUpAndAddToBridge(): Setting up dataplane interface", "PeerName", veth.PeerName)
	if err := netlink.LinkSetUp(vethLink); err != nil {
		vsp.log.Error(err, "Error setting up inport Veth", "InportVeth", veth.IfName, "PeerName", veth.PeerName)
		return err
	}

	vsp.log.Info("vethUpAndAddToBridge(): Adding dataplane interface to OvS Bridge", "PeerName", veth.PeerName)
	err = vspnetutils.AddInterfaceToOvSBridge(OvSBridgeName, veth.PeerName)
	if err != nil {
		vsp.log.Error(err, "Error adding inport Veth to OvS Bridge", "InportVeth", veth.IfName, "PeerName", veth.PeerName)
		return err
	}

	return nil
}

// TODO: Implement this
func (vsp *intelNetSecVspServer) CreateNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {
	vsp.log.Info("Received CreateNetworkFunction() request", "Input", in.Input, "Output", in.Output)

	// The expectation is that 2 devices would be allocated for the Network Function and we use the mac addresses
	// of each VETH pair to find it and add them to the bridge.
	inportVeth, ok := vsp.vethTunnelPairDevs[vspnetutils.VethPairKey{IfMac: in.Input}]
	if !ok {
		err := fmt.Errorf("veth pair not found for input: %s", in.Input)
		vsp.log.Error(err, "Error getting inport Veth", "InportVeth", in.Input)
		return nil, err
	}
	vsp.log.Info("CreateNetworkFunction(): Inport Veth", "InportVeth", inportVeth)

	outportVeth, ok := vsp.vethTunnelPairDevs[vspnetutils.VethPairKey{IfMac: in.Output}]
	if !ok {
		err := fmt.Errorf("veth pair not found for output: %s", in.Output)
		vsp.log.Error(err, "Error getting outport Veth", "OutportVeth", in.Output)
		return nil, err
	}
	vsp.log.Info("CreateNetworkFunction(): Outport Veth", "OutportVeth", outportVeth)

	err := vsp.vethUpAndAddToBridge(inportVeth)
	if err != nil {
		vsp.log.Error(err, "Error setting up inport Veth", "InportVeth", inportVeth.IfName)
		return nil, err
	}

	err = vsp.vethUpAndAddToBridge(outportVeth)
	if err != nil {
		vsp.log.Error(err, "Error setting up outport Veth", "OutportVeth", outportVeth.IfName)
		return nil, err
	}

	vsp.log.Info("CreateNetworkFunction(): Added Flow Rules to OvS Bridge", "InportVeth", inportVeth.IfName, "OutportVeth", outportVeth.IfName)
	vsp.numNfs++
	out := new(pb.Empty)
	return out, nil
}

// TODO: Implement this
func (vsp *intelNetSecVspServer) DeleteNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {
	vsp.log.Info("Received DeleteNetworkFunction() request", "Input", in.Input, "Output", in.Output)

	vsp.numNfs--

	inportVeth, ok := vsp.vethTunnelPairDevs[vspnetutils.VethPairKey{IfMac: in.Input}]
	if !ok {
		err := fmt.Errorf("veth pair not found for input: %s", in.Input)
		vsp.log.Error(err, "Error getting inport Veth", "InportVeth", in.Input)
		return nil, err
	}
	vsp.log.Info("DeleteNetworkFunction(): Inport Veth", "InportVeth", inportVeth)

	outportVeth, ok := vsp.vethTunnelPairDevs[vspnetutils.VethPairKey{IfMac: in.Output}]
	if !ok {
		err := fmt.Errorf("veth pair not found for output: %s", in.Output)
		vsp.log.Error(err, "Error getting outport Veth", "OutportVeth", in.Output)
		return nil, err
	}
	vsp.log.Info("DeleteNetworkFunction(): Outport Veth", "OutportVeth", outportVeth)

	err := vspnetutils.DeleteInterfaceFromOvSBridge(OvSBridgeName, inportVeth.IfName)
	if err != nil {
		vsp.log.Error(err, "Error deleting interface from OvS Bridge", "InportVeth", inportVeth.IfName)
		return nil, err
	}
	vsp.log.Info("DeleteNetworkFunction(): Deleted Interface from OvS Bridge", "InportVeth", inportVeth.IfName)

	err = vspnetutils.DeleteInterfaceFromOvSBridge(OvSBridgeName, outportVeth.IfName)
	if err != nil {
		vsp.log.Error(err, "Error deleting interface from OvS Bridge", "OutportVeth", outportVeth.IfName)
		return nil, err
	}
	vsp.log.Info("DeleteNetworkFunction(): Deleted Interface from OvS Bridge", "OutportVeth", outportVeth.IfName)

	return nil, nil
}

// setVlanIdsSpoofChk function to set the VLAN IDs for host facing interfaces such that each container
// will have a unique VLAN ID for isolating traffic. This is to ensure that each VF must be
// forwarded to the NetSec Acclerator to be processed.
func (vsp *intelNetSecVspServer) setVlanIdsSpoofChkInternalPorts(pcieAddr string, numVfs int) error {
	vsp.log.Info("setVlanIdsSpoofChkInternalPorts(): Setting VLAN IDs, Spoof Check off, Trust on for VFs", "PCIeAddress", pcieAddr, "NumVFs", numVfs)
	var ifName string
	var err error

	ifNames, err := vsp.platform.GetNetDevNameFromPCIeAddr(pcieAddr)
	if err != nil {
		vsp.log.Error(err, "Error getting netdev name from PCIe address", "PCIeAddress", pcieAddr)
		return err
	}

	if len(ifNames) != 1 {
		err = fmt.Errorf("expected exactly 1 interface for PCIe address %s, got %v", pcieAddr, ifNames)
		vsp.log.Error(err, "Error getting netdev name from PCIe address", "PCIeAddress", pcieAddr)
		return err
	}

	ifName = ifNames[0]

	// WORKAROUND: Set the PF hardware mode to VEPA (Virtual Ethernet Port Aggregator)
	// Only needed on the host because the NetSec Accelerator will switch packets, but the
	// host will not switch packets.
	if !vsp.isDPUMode {
		err = vspnetutils.SetPfHwModeVepa(ifName)
		if err != nil {
			vsp.log.Error(err, "Error setting PF hardware mode to VEPA", "IfName", ifName)
			return err
		}
	}

	// Map each VF ID to the corresponding VLAN ID.
	for vfID := 0; vfID < numVfs; vfID++ {
		vlan := vfID + VlanOffset

		link, err := netlink.LinkByName(ifName)
		if err != nil {
			vsp.log.Error(err, "Failed to get link by name", "IfName", ifName)
			return err
		}

		// This is the equivalent of "ip link set dev <PF> vf <ID> vlan <VLAN>"
		// SetSriovVlanId sets the VLAN ID for a specific virtual function (VF)
		// As per ip-link (8) VLAN is special where it disable VLAN tagging and filtering
		// Also incoming traffic will be filtered for the specific VLAN ID and will
		// have all VLAN tags stripped before being passed to the VF.
		if err := netlink.LinkSetVfVlan(link, vfID, vlan); err != nil {
			vsp.log.Error(err, "Failed to set VLAN on VF", "VfID", vfID, "VlanID", vlan, "InterfaceName", ifName)
			return err
		}

		// This is the equivalent of "ip link set dev <PF> vf <ID> spoofchk <VLAN>"
		// SetSriovVlanId sets the Spoof Check for a specific virtual function (VF)
		// As per ip-link (8) Spoof Check is used to enable/disable the spoof check on the VF.
		// When enabled, the VF will only accept packets with the source MAC address matching the VF's
		// MAC address. When disabled, the VF will accept packets with any source MAC address.
		// This is needed since the MAC address will be of the internal interface ports of the network functions
		// and not the MAC address of the VF.
		if err := netlink.LinkSetVfSpoofchk(link, vfID, false); err != nil {
			vsp.log.Error(err, "Failed to set spoof check off for VF", "VfID", vfID, "InterfaceName", ifName)
			return err
		}

		// WORKAROUND: For now we want VFs to be trusted for promiscuous mode.
		// This is the equivalent of "ip link set dev <PF> vf <ID> trust <VLAN>"
		if err := netlink.LinkSetVfTrust(link, vfID, true); err != nil {
			vsp.log.Error(err, "Failed to set trust on for VF", "VfID", vfID, "InterfaceName", ifName)
			return err
		}

		pcieAddr, err := vspnetutils.VfPCIAddressFromVfIndex(vsp.fs, ifName, vfID)
		if err != nil {
			vsp.log.Error(err, "Error getting VF PCI address", "VFID", vfID, "InterfaceName", ifName)
			return err
		}

		key := vspnetutils.VfDeviceKey{
			PfInterfaceName: ifName,
			Id:              vfID,
		}

		vsp.vfDevs[key] = &vspnetutils.VfDeviceInfo{
			VfKey:      key,
			PciAddress: pcieAddr,
			Vlan:       vlan,
			Allocated:  false,
		}

		vsp.log.Info("setVlanIdsSpoofChkInternalPorts(): Set VLAN ID for VF", "VFID", vfID, "VlanID", vlan, "InterfaceName", ifName, "PCIeAddress", pcieAddr)
		vsp.log.Info("setVlanIdsSpoofChkInternalPorts(): VF Device Info", "VFKey", key, "VfDeviceInfo", vsp.vfDevs[key])
	}

	return err
}

func (vsp *intelNetSecVspServer) setSpoofChkExternalPorts(pcieAddr string, numVfs int) error {
	vsp.log.Info("setSpoofChkExternalPorts(): Setting Spoof Check off for VFs", "PCIeAddress", pcieAddr, "NumVFs", numVfs)

	ifNames, err := vsp.platform.GetNetDevNameFromPCIeAddr(pcieAddr)
	if err != nil {
		vsp.log.Error(err, "Error getting netdev name from PCIe address", "PCIeAddress", pcieAddr)
		return err
	}

	if len(ifNames) != 1 {
		err = fmt.Errorf("expected exactly 1 interface for PCIe address %s, got %v", pcieAddr, ifNames)
		vsp.log.Error(err, "Error getting netdev name from PCIe address", "PCIeAddress", pcieAddr)
		return err
	}

	ifName := ifNames[0]

	for vfID := 0; vfID < numVfs; vfID++ {
		link, err := netlink.LinkByName(ifName)
		if err != nil {
			vsp.log.Error(err, "Failed to get link by name", "IfName", ifName)
			return err
		}

		// This is the equivalent of "ip link set dev <PF> vf <ID> spoofchk <VLAN>"
		// SetSriovVlanId sets the Spoof Check for a specific virtual function (VF)
		// As per ip-link (8) Spoof Check is used to enable/disable the spoof check on the VF.
		// When enabled, the VF will only accept packets with the source MAC address matching the VF's
		// MAC address. When disabled, the VF will accept packets with any source MAC address.
		// This is needed since the MAC address will be of the internal interface ports of the network functions
		// and not the MAC address of the VF.
		if err := netlink.LinkSetVfSpoofchk(link, vfID, false); err != nil {
			vsp.log.Error(err, "Failed to set spoof check off for VF", "VfID", vfID, "InterfaceName", ifName)
			return err
		}

		vsp.log.Info("setSpoofChkExternalPorts(): Set spoof check off for VF", "InterfaceName", ifName, "VfID", vfID)
	}

	return nil
}

// SetNumVfs function to set the number of VFs with the given context and VfCount
func (vsp *intelNetSecVspServer) SetNumVfs(ctx context.Context, in *pb.VfCount) (*pb.VfCount, error) {
	vsp.log.Info("SetNumVfs() called", "VfCnt", in.VfCnt)
	var err error

	if vsp.isDPUMode {
		err = vspnetutils.SetSriovNumVfs(vsp.fs, IntelNetSecDpuBackplanef2PCIeAddress, int(in.VfCnt))
		if err != nil {
			vsp.log.Error(err, "Error setting number of VFs", "isDPUMode", vsp.isDPUMode, "PcieAddress", IntelNetSecDpuBackplanef2PCIeAddress, "VfCnt", in.VfCnt)
			return &pb.VfCount{VfCnt: 0}, err
		}

		err = vsp.setVlanIdsSpoofChkInternalPorts(IntelNetSecDpuBackplanef2PCIeAddress, int(in.VfCnt))
		if err != nil {
			vsp.log.Error(err, "Error setting VLAN IDs for VFs", "isDPUMode", vsp.isDPUMode, "PcieAddress", IntelNetSecDpuBackplanef2PCIeAddress, "VfCnt", in.VfCnt)
			return &pb.VfCount{VfCnt: 0}, err
		}

		err = vspnetutils.SetSriovNumVfs(vsp.fs, IntelNetSecDpuSFPf1PCIeAddress, MaxChains)
		if err != nil {
			vsp.log.Error(err, "Error occurred in setting number of VFs for SFP Port", "isDPUMode", vsp.isDPUMode, "PcieAddress", IntelNetSecDpuSFPf1PCIeAddress, "MaxChains", MaxChains)
			return &pb.VfCount{VfCnt: 0}, err
		}

		err = vsp.setSpoofChkExternalPorts(IntelNetSecDpuSFPf1PCIeAddress, MaxChains)
		if err != nil {
			vsp.log.Error(err, "Error setting spoof check off for VFs", "isDPUMode", vsp.isDPUMode, "PcieAddress", IntelNetSecDpuSFPf1PCIeAddress, "MaxChains", MaxChains)
			return &pb.VfCount{VfCnt: 0}, err
		}
	} else {
		err = vspnetutils.SetSriovNumVfs(vsp.fs, vsp.dpuPcieAddress, int(in.VfCnt))
		if err != nil {
			vsp.log.Error(err, "Error setting number of VFs", "isDPUMode", vsp.isDPUMode, "PcieAddress", vsp.dpuPcieAddress, "VfCnt", in.VfCnt)
			return &pb.VfCount{VfCnt: 0}, err
		}

		err = vsp.setVlanIdsSpoofChkInternalPorts(vsp.dpuPcieAddress, int(in.VfCnt))
		if err != nil {
			vsp.log.Error(err, "Error setting VLAN IDs for VFs", "isDPUMode", vsp.isDPUMode, "PcieAddress", vsp.dpuPcieAddress, "VfCnt", in.VfCnt)
			return &pb.VfCount{VfCnt: 0}, err
		}
	}

	vsp.vfCnt = int(in.VfCnt)
	return in, err
}

func (vsp *intelNetSecVspServer) Listen() (net.Listener, error) {
	err := vsp.pathManager.EnsureSocketDirExists(vsp.pathManager.VendorPluginSocket())
	if err != nil {
		return nil, fmt.Errorf("failed to create run directory for vendor plugin socket: %v", err)
	}
	listener, err := net.Listen("unix", vsp.pathManager.VendorPluginSocket())
	if err != nil {
		return nil, fmt.Errorf("failed to listen on the vendor plugin socket: %v", err)
	}
	vsp.grpcServer = grpc.NewServer()
	pb.RegisterNetworkFunctionServiceServer(vsp.grpcServer, vsp)
	pb.RegisterLifeCycleServiceServer(vsp.grpcServer, vsp)
	pb.RegisterDeviceServiceServer(vsp.grpcServer, vsp)
	opi.RegisterBridgePortServiceServer(vsp.grpcServer, vsp)
	vsp.log.Info("gRPC server listening", "SocketPath", vsp.pathManager.VendorPluginSocket(), "listenerAddr", listener.Addr())

	return listener, nil
}

func (vsp *intelNetSecVspServer) Serve(listener net.Listener) error {
	vsp.wg.Add(1)
	go func() {
		vsp.version = Version
		vsp.log.Info("Starting Intel NetSec VSP Server", "Version", vsp.version)
		if err := vsp.grpcServer.Serve(listener); err != nil {
			vsp.done <- err
		} else {
			vsp.done <- nil
		}
		vsp.log.Info("Intel NetSec VSP Server stopped")
		vsp.wg.Done()
	}()

	// Block on any go routines writing to the done channel when an error occurs or they
	// are forced to exit.
	err := <-vsp.done

	vsp.grpcServer.Stop()
	vsp.wg.Wait()
	vsp.startedWg.Done()
	return err
}

func (vsp *intelNetSecVspServer) Stop() {
	if err := vspnetutils.DeleteOvSBridge(OvSBridgeName); err != nil {
		vsp.log.Error(err, "Error occurred during deleting OvS Bridge", "BridgeName", OvSBridgeName)
	}

	for _, tunnelPairDev := range vsp.vethTunnelPairDevs {
		if err := vspnetutils.DestroyVethPair(tunnelPairDev); err != nil {
			vsp.log.Error(err, "Error occurred during deleting Veth-Peer", "VethPeerName", tunnelPairDev.IfName)
		} else {
			vsp.log.Info("Deleted Veth-Peer", "VethPeerName", tunnelPairDev.IfName)
		}
	}

	vsp.grpcServer.Stop()
	vsp.done <- nil
	vsp.startedWg.Wait()
}

func WithPathManager(pathManager utils.PathManager) func(*intelNetSecVspServer) {
	return func(vsp *intelNetSecVspServer) {
		vsp.pathManager = pathManager
	}
}

func NewIntelNetSecVspServer(opts ...func(*intelNetSecVspServer)) *intelNetSecVspServer {
	var mode string
	flag.StringVar(&mode, "mode", "", "Mode for the daemon, can be either host or dpu")
	options := zap.Options{
		Development: true,
		Level:       zapcore.InfoLevel,
	}
	options.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&options)))
	vsp := &intelNetSecVspServer{
		log:                   ctrl.Log.WithName("IntelNetSecVsp"),
		pathManager:           *utils.NewPathManager("/"),
		done:                  make(chan error),
		fs:                    afero.NewOsFs(),
		platform:              &platform.HardwarePlatform{},
		vfDevs:                make(map[vspnetutils.VfDeviceKey]*vspnetutils.VfDeviceInfo),
		vethTunnelPairDevs:    make(map[vspnetutils.VethPairKey]*vspnetutils.VEthPairDeviceInfo),
		numNfs:                0,
		serviceFunctionChains: make(map[int]*intelNetSecServiceFunctionChain),
	}

	for _, opt := range opts {
		opt(vsp)
	}

	return vsp
}

func main() {
	intelNetSecVspServer := NewIntelNetSecVspServer()
	listener, err := intelNetSecVspServer.Listen()

	if err != nil {
		intelNetSecVspServer.log.Error(err, "Failed to Listen Intel NetSec VSP server")
		return
	}
	err = intelNetSecVspServer.Serve(listener)
	if err != nil {
		intelNetSecVspServer.log.Error(err, "Failed to serve  Intel NetSec VSP server")
		return
	}
}
