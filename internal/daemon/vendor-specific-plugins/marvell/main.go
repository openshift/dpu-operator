package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	debugdp "github.com/openshift/dpu-operator/internal/daemon/vendor-specific-plugins/marvell/debug-dp"
	mrvlutils "github.com/openshift/dpu-operator/internal/daemon/vendor-specific-plugins/marvell/mrvl-utils"
	ovsdp "github.com/openshift/dpu-operator/internal/daemon/vendor-specific-plugins/marvell/ovs-dp"
	"github.com/openshift/dpu-operator/internal/utils"
	opi "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"github.com/vishvananda/netlink"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	SysBusPci      string = "/sys/bus/pci/devices"
	VendorID       string = "177d"
	DPUdeviceID    string = "a0f7"
	HostDeviceID   string = "b900"
	DefaultPort    int32  = 8085
	Version        string = "0.0.1"
	PortType       string = "veth"
	NoOfPortPairs  int    = 2
	IPv6AddrDpu    string = "fe80::1"
	IPv6AddrHost   string = "fe80::2"
	DataPlaneType  string = "ovs"
	NumPFs         int    = 1
	PFID           int    = 0
	isDPDK         bool   = false
	HostVFDeviceID string = "b903"
	DpuRpmDeviceID string = "a063"
	NfName         string = "mrvl-nf1"
	isNf           bool   = false
)

// multiple dataplane can be added using mrvldp interface functions
type mrvldp interface {
	AddPortToDataPlane(bridgeName string, portName string, vfPCIAddres string, isDPDK bool) error
	DeletePortFromDataPlane(bridgeName string, portName string) error
	InitDataPlane(bridgeName string) error
	ReadAllPortFromDataPlane(bridgeName string) (string, error)
	DeleteDataplane(bridgeName string) error
	AddFlowRuleToDataPlane(bridgeName string, inPort string, outPort string, dstMac string) error
	DeleteFlowRuleFromDataPlane(bridgeName string, inPort string, outPort string, dstMac string) error
}
type mrvlDeviceInfo struct {
	secInterfaceName string
	dpInterfaceName  string
	dpMAC            string
	portType         string
	health           string
	pciAddress       string
}

type vfInfo struct {
	vfName string
	mac    string
}
type mrvlNfPortMap struct {
	vfPort  []vfInfo
	inpPort string
	outPort string
}
type mrvlVspServer struct {
	pb.UnimplementedLifeCycleServiceServer
	pb.UnimplementedNetworkFunctionServiceServer
	pb.UnimplementedDeviceServiceServer
	opi.UnimplementedBridgePortServiceServer
	log           logr.Logger
	grpcServer    *grpc.Server
	wg            sync.WaitGroup
	done          chan error
	startedWg     sync.WaitGroup
	pathManager   utils.PathManager
	version       string
	isDPUMode     bool
	deviceStore   map[string]mrvlDeviceInfo
	noOfPortPairs int
	portType      string
	bridgeName    string
	mrvlDP        mrvldp
	networkStore  map[string]mrvlNfPortMap
	isNF          bool
}

// createVethPair function to create a veth pair with the given index and InterfaceInfo
func (vsp *mrvlVspServer) createVethPair(index int) error {
	//secInterfaceName is the name of the interface on the Network Function side
	//dpInterfaceName is the name of the interface on the Data Plane side
	secInterfaceName := fmt.Sprintf("nf_interface%d", index)
	dpInterfaceName := fmt.Sprintf("dp_interface%d", index)

	var nfLink netlink.Link
	var peerLink netlink.Link
	var err error

	nfLink, _ = netlink.LinkByName(secInterfaceName)
	if nfLink != nil {
		peerLink, _ = netlink.LinkByName(dpInterfaceName)
	}

	if nfLink == nil || peerLink == nil {
		vethLink := &netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{Name: secInterfaceName},
			PeerName:  dpInterfaceName,
		}
		if err := netlink.LinkAdd(vethLink); err != nil {
			return err
		}
		nfLink, err = netlink.LinkByName(secInterfaceName)
		if err != nil {
			return err
		}
		peerLink, err = netlink.LinkByName(dpInterfaceName)
		if err != nil {
			return err
		}
	}

	if err := netlink.LinkSetUp(nfLink); err != nil {
		return err
	}

	if err := netlink.LinkSetUp(peerLink); err != nil {
		return err
	}
	health := vsp.GetDeviceHealth(secInterfaceName)
	vsp.deviceStore[nfLink.Attrs().HardwareAddr.String()] = mrvlDeviceInfo{
		secInterfaceName: secInterfaceName,
		dpInterfaceName:  dpInterfaceName,
		dpMAC:            peerLink.Attrs().HardwareAddr.String(),
		health:           health,
		portType:         "veth",
	}
	return nil
}

func (vsp *mrvlVspServer) createHwLBK() error {
	//TODO: Implement HW Loopback
	klog.Infof("currently only veth pairs are supported")
	return errors.New("currently only veth pairs are supported")
}

// CleanVethPairs function to clean all the veth pairs created
func (vsp *mrvlVspServer) CleanVethPairs() error {
	var errResult error
	if vsp.deviceStore != nil {
		deviceStore := vsp.deviceStore
		vsp.deviceStore = nil
		for _, mrvlDeviceInfo := range deviceStore {
			nfLink, err := netlink.LinkByName(mrvlDeviceInfo.secInterfaceName)
			if err != nil {
				klog.Errorf("Error occurred in getting Link By Name: %v", err)
				errResult = errors.Join(errResult, err)
				continue
			}
			if err := netlink.LinkDel(nfLink); err != nil {
				klog.Errorf("Error occurred in deleting Link: %v", err)
				errResult = errors.Join(errResult, err)
				continue
			}
			klog.Infof("Deleted Veth Pair: %s", mrvlDeviceInfo.secInterfaceName)
		}
	}
	return errResult
}

// ConfigureNetworkInterface function to configure the network interface based on the config file
func (vsp *mrvlVspServer) ConfigureNetworkInterface() error {
	// TODO: Replace PortTyp and NoOfPortPairs with the values from the CR/ConfigMap
	vsp.portType = PortType
	vsp.noOfPortPairs = NoOfPortPairs

	switch vsp.portType {
	case "veth":
		klog.Infof("Creating Veth Pairs: %d", vsp.noOfPortPairs)
		for i := 0; i < vsp.noOfPortPairs; i++ {
			err := vsp.createVethPair(i)
			if err != nil {
				klog.Errorf("Error occurred in creating Veth Pair: %v", err)
				_ = vsp.CleanVethPairs()
				return err
			}
		}
	case "hwlbk":
		err := vsp.createHwLBK()
		if err != nil {
			klog.Errorf("Error occurred in creating HW loopback: %v", err)
			return err
		}
	default:
		return errors.New("invalid Port Type")
	}
	for nfMacAddress, mrvlDeviceInfo := range vsp.deviceStore {
		klog.Infof("nfMacAddress: %s, secInterfaceName: %s, dpInterfaceName: %s, dpMacAddress: %s, health: %s", nfMacAddress, mrvlDeviceInfo.secInterfaceName, mrvlDeviceInfo.dpInterfaceName, mrvlDeviceInfo.dpMAC, mrvlDeviceInfo.health)
	}
	return nil
}

// GetDeviceHealth function to get the health of the device based on the given secInterfaceName
func (vsp *mrvlVspServer) GetDeviceHealth(secInterfaceName string) string {
	switch vsp.portType {
	case "veth", "sriov":
		nfLink, err := netlink.LinkByName(secInterfaceName)
		if err != nil {
			return "Unhealthy"
		}
		//check if the interface is up =0 means interface is down
		if nfLink.Attrs().Flags&net.FlagUp == 0 {
			return "Unhealthy"
		}
		return "Healthy"
	case "hwlbk":
		return "Healthy" //TODO: Implement HW Loopback
	default:
		return "Unhealthy"
	}
}

// Init function to initialize the Marvell VSP Server with the given context and InitRequest
// It will return the IpPort and error
func (vsp *mrvlVspServer) Init(ctx context.Context, in *pb.InitRequest) (*pb.IpPort, error) {
	klog.Infof("Received Init() request: DpuMode: %v", in.DpuMode)
	vsp.isDPUMode = in.DpuMode
	ipPort, err := vsp.configureIP(in.DpuMode)
	if vsp.deviceStore == nil {
		vsp.deviceStore = make(map[string]mrvlDeviceInfo)
	}
	if vsp.isDPUMode {
		err := vsp.ConfigureNetworkInterface()
		if err != nil {
			klog.Errorf("Error occurred in configuring Network Interface: %v", err)
			vsp.Stop()
			return &pb.IpPort{}, err
		}
		// Initialize Marvell Data Path
		vsp.bridgeName = "br-mrv0" // TODO: example name discuss on it
		if err := vsp.mrvlDP.InitDataPlane(vsp.bridgeName); err != nil {
			klog.Errorf("Error occurred in initializing Data Path: %v", err)
			vsp.Stop()
			return &pb.IpPort{}, err
		}

	} else {
		vsp.portType = "sriov"
		VfsPCI, err := mrvlutils.GetAllVfsByDeviceID(HostVFDeviceID)
		if err != nil {
			return nil, err
		}
		for _, vfpci := range VfsPCI {
			health := vsp.GetDeviceHealth(vfpci)
			vsp.deviceStore[vfpci] = mrvlDeviceInfo{
				pciAddress: vfpci,
				health:     health,
				portType:   "sriov",
			}
		}
	}
	return &pb.IpPort{
		Ip:   ipPort.Ip,
		Port: ipPort.Port,
	}, err
}

// getVFName function to get the VF Name of the given BridgePortName on DPU
func (vsp *mrvlVspServer) getVFDetails(BridgePortName string) (string, string, error) {
	// regexp to get VFId from BridgePortName ex: host1-0 , vfId=0
	re := regexp.MustCompile(`host(\d+)-(\d+)`)
	matches := re.FindStringSubmatch(BridgePortName)
	if matches == nil {
		return "", "", errors.New("no VFId Match Found")
	}
	pfid, err := strconv.Atoi(matches[1])
	if err != nil {
		return "", "", err
	}
	vfId, err := strconv.Atoi(matches[2])
	if err != nil {
		return "", "", err
	}
	vfId = vfId + 1 // VF Id 0 is for PF
	klog.Infof("Mapped VF for PFID: %d, VFID: %d, NumPFs: %d", pfid, vfId, NumPFs)
	vfPciAddress, err := mrvlutils.Mapped_VF(NumPFs, PFID, vfId) // TODO: Get PF Count=1 and PF ID=0
	if err != nil {
		return "", "", err
	}
	klog.Infof("VF PCI Address: %s", vfPciAddress)
	if vfPciAddress == "" {
		return "", "", errors.New("mapped VF not found")
	}
	vfName := ""
	if isDPDK {
		vfName = fmt.Sprintf("vf%d-%d", pfid, vfId)
	} else {
		// NetDevices, err := sriovnet.GetNetDevicesFromPci(vfPciAddress)
		vfName, err = mrvlutils.GetNameByPCI(vfPciAddress)
		// vfName = NetDevices[0]
		if err != nil {
			return "", "", err
		}
	}
	return vfName, vfPciAddress, err
}

// CreateBridgePort function to create a bridge port with the given context and CreateBridgePortRequest
// It will return the BridgePort and error
func (vsp *mrvlVspServer) CreateBridgePort(ctx context.Context, in *opi.CreateBridgePortRequest) (*opi.BridgePort, error) {
	klog.Infof("Received CreateBridgePort() request: BridgePortId: %v, BridgePort: %v", in.BridgePortId, in.BridgePort)
	portName := in.BridgePort.Name
	vfName, vfPCIAddress, err := vsp.getVFDetails(portName)
	if err != nil {
		klog.Errorf("Error occurred in getting VF Name: %v, BridgePortName: %v", err, portName)
		return nil, err
	}
	if err := vsp.mrvlDP.AddPortToDataPlane(vsp.bridgeName, vfName, vfPCIAddress, isDPDK); err != nil {
		klog.Errorf("Error occurred in adding Port to Bridge: %v", err)
		return nil, err
	}
	klog.Info("Port Added to Bridge Successfully")
	// Store port into networkstore
	if network, exists := vsp.networkStore[NfName]; exists {
		mac := in.BridgePort.Spec.MacAddress
		vfport := vfInfo{
			vfName: vfName,
			mac:    net.HardwareAddr(mac).String(),
		}
		network.vfPort = append(network.vfPort, vfport)
		vsp.networkStore[NfName] = network
	} else {
		vsp.networkStore[NfName] = mrvlNfPortMap{
			vfPort: []vfInfo{
				{
					vfName: vfName,
					mac:    net.HardwareAddr(in.BridgePort.Spec.MacAddress).String(),
				},
			},
			// No Network store exists hence no input/output port will be replaced by CNF with the actuall input/output port
			inpPort: "",
			outPort: "",
		}
	}
	// Add Flow Rule if there is an NF
	if vsp.isNF {
		klog.Info("Marvell Store looks like", vsp.networkStore)
		mac := net.HardwareAddr(in.BridgePort.Spec.MacAddress).String()
		// Add Flow rule from vfName to inPort (where in_port=vfname action=out_port=vsp.networkStore[NfName].inpPort)
		if err := vsp.mrvlDP.AddFlowRuleToDataPlane(vsp.bridgeName, vfName, vsp.networkStore[NfName].inpPort, ""); err != nil {
			klog.Errorf("Error occurred in adding Flow Rule: %v", err)
			return nil, err
		}

		// Add flow rule from inPort to vfName (where in_port=vsp.networkStore[NfName].inpPort action=out_port=vfName)
		// TODO: check if this can be done with Mac Learning?
		if err := vsp.mrvlDP.AddFlowRuleToDataPlane(vsp.bridgeName, vsp.networkStore[NfName].inpPort, vfName, mac); err != nil {
			klog.Errorf("Error occurred in adding Flow Rule: %v", err)
			return nil, err
		}
		// Add Hairpinning Flow Rule based on MAC Address
		if err := vsp.mrvlDP.AddFlowRuleToDataPlane(vsp.bridgeName, vsp.networkStore[NfName].outPort, vsp.networkStore[NfName].outPort, mac); err != nil {
			klog.Errorf("Error occurred in adding Flow Rule: %v", err)
			return nil, err
		}
		klog.Info("Flow Rule Added to Bridge Successfully")

	}
	if isDPDK {
		if err = mrvlutils.PrintDPDKPortInfo(vfPCIAddress); err != nil {
			klog.Errorf("Error occurred in printing DPDK Port Info: %v", err)
		}
	} else {
		if err = mrvlutils.PrintPortInfo(vfName); err != nil {
			klog.Errorf("Error occurred in printing Port Info: %v", err)
		}
	}
	out := &opi.BridgePort{
		Name:   fmt.Sprintf("bridge_port/%s", portName),
		Spec:   in.BridgePort.Spec,
		Status: &opi.BridgePortStatus{},
	}
	return out, nil
}

// DeleteBridgePort function to delete a bridge port with the given context and DeleteBridgePortRequest
// It will return the Empty and error
func (vsp *mrvlVspServer) DeleteBridgePort(ctx context.Context, in *opi.DeleteBridgePortRequest) (*emptypb.Empty, error) {
	klog.Infof("Received DeleteBridgePort() request: Name: %v, AllowMissing: %v", in.Name, in.AllowMissing)
	portName := in.Name
	vfName, _, err := vsp.getVFDetails(portName)
	klog.Infof("VF Name: %s", vfName)
	if err != nil {
		klog.Info("Error occurred in getting VF Name")
		return nil, err
	}
	// Delete Flow Rule before deleting Port from Bridge if there is NF
	if vsp.isNF {
		inpPort := vsp.networkStore[NfName].inpPort
		outPort := vsp.networkStore[NfName].outPort
		vf_mac := ""
		for _, vf := range vsp.networkStore[NfName].vfPort {
			if vf.vfName == vfName {
				vf_mac = vf.mac
				break
			}
		}
		if err := vsp.mrvlDP.DeleteFlowRuleFromDataPlane(vsp.bridgeName, vfName, "", ""); err != nil {
			klog.Errorf("Error occurred in deleting Flow Rule: %v", err)
			return nil, err
		}
		//Delete Hair pinning Flow Rule added for this port
		if err := vsp.mrvlDP.DeleteFlowRuleFromDataPlane(vsp.bridgeName, outPort, outPort, vf_mac); err != nil {
			klog.Errorf("Error occurred in deleting Flow Rule: %v", err)
			return nil, nil
		}
		// Delete Flow Rule from inpPort to vfName
		// TODO: This can be removed if Mac Learning is enabled
		if err := vsp.mrvlDP.DeleteFlowRuleFromDataPlane(vsp.bridgeName, inpPort, vfName, vf_mac); err != nil {
			klog.Errorf("Error occurred in deleting Flow Rule: %v", err)
			return nil, nil
		}
		klog.Info("Flow Rule Deleted from Bridge Successfully")
	}
	if err := vsp.mrvlDP.DeletePortFromDataPlane(vsp.bridgeName, vfName); err != nil {
		klog.Errorf("Error occurred in deleting Port from Bridge: %v", err)
		return nil, err
	}
	klog.Info("Port Deleted from Bridge Successfully")
	// Delete port from networkstore
	if network, exists := vsp.networkStore[NfName]; exists {
		for i, port := range network.vfPort {
			if port.vfName == vfName {
				copy(network.vfPort[i:], network.vfPort[i+1:])
				network.vfPort = network.vfPort[:len(network.vfPort)-1]
				vsp.networkStore[NfName] = network
				break
			}
		}
		vsp.networkStore[NfName] = network
		klog.Info("Port Deleted from Network Function Store Successfully")
	}
	klog.Info("Network Function Store: ", vsp.networkStore)
	if err = mrvlutils.PrintPortInfo(vfName); err != nil {
		klog.Errorf("Error occurred in printing Port Info: %v", err)
	}
	out := new(emptypb.Empty)
	return out, nil
}

// CreateNetworkFunction function to create a network function with the given context and NFRequest
// It will return the Empty and error
func (vsp *mrvlVspServer) CreateNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {
	klog.Infof("Received CreateNetworkFunction() request: Input: %v, Output: %v", in.Input, in.Output)
	vsp.isNF = true
	inpDpInterfaceName := vsp.deviceStore[in.Input].dpInterfaceName
	outDpInterfaceName := vsp.deviceStore[in.Output].dpInterfaceName
	out, err := vsp.AddNetworkFunction(inpDpInterfaceName, outDpInterfaceName, NfName)
	return out, err
}

// AddNetworkFunction function to add a network function with the given Interface Name and NFName
// It will return the Empty and error
func (vsp *mrvlVspServer) AddNetworkFunction(inpDpInterfaceName string, outDpInterfaceName string, nfName string) (*pb.Empty, error) {
	if err := vsp.mrvlDP.AddPortToDataPlane(vsp.bridgeName, inpDpInterfaceName, "", isDPDK); err != nil {
		klog.Errorf("Error occurred in adding Port to Bridge: %v", err)
		return nil, err
	}
	klog.Info("Input Port Added to Bridge Successfully")
	if err := vsp.mrvlDP.AddPortToDataPlane(vsp.bridgeName, outDpInterfaceName, "", isDPDK); err != nil {
		klog.Errorf("Error occurred in adding Port to Bridge: %v", err)
		return nil, err
	}
	klog.Info("Output Port Added to Bridge Successfully")
	if _, exists := vsp.networkStore[nfName]; exists {
		dpuVfs := vsp.networkStore[nfName].vfPort
		vsp.networkStore[nfName] = mrvlNfPortMap{
			vfPort:  dpuVfs,
			inpPort: inpDpInterfaceName,
			outPort: outDpInterfaceName,
		}
		for _, vf := range dpuVfs {
			if err := vsp.mrvlDP.AddFlowRuleToDataPlane(vsp.bridgeName, vf.vfName, inpDpInterfaceName, ""); err != nil {
				klog.Errorf("Error occurred in adding Flow Rule: %v", err)
				return nil, err
			}
			klog.Info("Flow Rule Added to Bridge Successfully from vfName to inpPort")
			if err := vsp.mrvlDP.AddFlowRuleToDataPlane(vsp.bridgeName, inpDpInterfaceName, vf.vfName, vf.mac); err != nil {
				klog.Errorf("Error occurred in adding Flow Rule: %v", err)
				return nil, err
			}
			klog.Info("Flow Rule Added to Bridge Successfully from inpPort to vfName")
			// Add Hairpinning Flow Rule based on MAC Address
			if err := vsp.mrvlDP.AddFlowRuleToDataPlane(vsp.bridgeName, outDpInterfaceName, outDpInterfaceName, vf.mac); err != nil {
				klog.Errorf("Error occurred in adding Flow Rule: %v", err)
				return nil, err
			}
			klog.Infof("Flow Rule Added to Bridge Successfully for Hairpinning outport:%s", outDpInterfaceName)
		}
	} else {
		vsp.networkStore[nfName] = mrvlNfPortMap{
			inpPort: inpDpInterfaceName,
			outPort: outDpInterfaceName,
		}
		klog.Info("Network Function Store: ", vsp.networkStore)
	}

	// Add Flow Rule for Out port to RPM Interface & RPM to OurPort
	DpuRpmInterfaceName, err := mrvlutils.GetNameByDeviceID(DpuRpmDeviceID)
	if err != nil {
		klog.Errorf("Error occurred in getting RPM Interface Name: %v", err)
		return nil, err
	}
	if err := vsp.mrvlDP.AddFlowRuleToDataPlane(vsp.bridgeName, outDpInterfaceName, DpuRpmInterfaceName, ""); err != nil {
		klog.Errorf("Error occurred in adding Flow Rule: %v", err)
		return nil, err
	}
	klog.Info("Flow Rule Added to Bridge Successfully from outPort to RPM Interface")
	if err := vsp.mrvlDP.AddFlowRuleToDataPlane(vsp.bridgeName, DpuRpmInterfaceName, outDpInterfaceName, ""); err != nil {
		klog.Errorf("Error occurred in adding Flow Rule: %v", err)
		return nil, err
	}
	klog.Info("Flow Rule Added to Bridge Successfully from RPM Interface to outPort")
	out := new(pb.Empty)
	return out, nil
}
func (vsp *mrvlVspServer) DeleteNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {
	klog.Infof("Received DeleteNetworkFunction() request: Input: %v, Output: %v", in.Input, in.Output)
	vsp.isNF = false
	inpDpInterfaceName := vsp.deviceStore[in.Input].dpInterfaceName
	outDpInterfaceName := vsp.deviceStore[in.Output].dpInterfaceName
	out, err := vsp.DeleteNetworkFunctionPort(inpDpInterfaceName, outDpInterfaceName, NfName)
	return out, err
}

// DeleteNetworkFunction function to delete a network function with the given context and NFRequest
// It will return the Empty and error
func (vsp *mrvlVspServer) DeleteNetworkFunctionPort(inpDpInterfaceName string, outDpInterfaceName string, nfName string) (*pb.Empty, error) {
	dpuVfsName := vsp.networkStore[nfName].vfPort
	for _, vf := range dpuVfsName {
		if err := vsp.mrvlDP.DeleteFlowRuleFromDataPlane(vsp.bridgeName, vf.vfName, inpDpInterfaceName, ""); err != nil {
			klog.Errorf("Error occurred in deleting Flow Rule: %v", err)
			return nil, err
		}
		klog.Infof("Flow Rule Deleted from Bridge Successfully inport:%s", vf.vfName)
	}
	if err := vsp.mrvlDP.DeleteFlowRuleFromDataPlane(vsp.bridgeName, inpDpInterfaceName, "", ""); err != nil {
		klog.Errorf("DNF: Error occurred in deleting Flow Rule: %v", err)
		return nil, err
	}
	klog.Infof("Flow Rule Deleted from Bridge Successfully inport:%s", inpDpInterfaceName)
	// Delete flow rule for out port to RPM Interface & RPM to OurPort
	if err := vsp.mrvlDP.DeleteFlowRuleFromDataPlane(vsp.bridgeName, outDpInterfaceName, "", ""); err != nil {
		klog.Errorf("DNF: Error occurred in deleting Flow Rule: %v", err)
		return nil, err
	}
	klog.Infof("Flow Rule Deleted from Bridge Successfully inport:%s", outDpInterfaceName)

	DpuRpmInterfaceName, err := mrvlutils.GetNameByDeviceID(DpuRpmDeviceID)
	if err != nil {
		klog.Errorf("DNF: Error occurred in getting RPM Interface Name: %v", err)
		return nil, err
	}
	if err := vsp.mrvlDP.DeleteFlowRuleFromDataPlane(vsp.bridgeName, DpuRpmInterfaceName, "", ""); err != nil {
		klog.Errorf("DNF: Error occurred in deleting Flow Rule: %v", err)
		return nil, err
	}
	klog.Infof("flow Rule Deleted from Bridge Successfully inport:%s", DpuRpmInterfaceName)
	if err := vsp.mrvlDP.DeletePortFromDataPlane(vsp.bridgeName, inpDpInterfaceName); err != nil {
		klog.Errorf("Error occurred in deleting Port from Bridge: %v", err)
		return nil, err
	}
	klog.Info("Input Port Deleted from Bridge Successfully")
	if err := vsp.mrvlDP.DeletePortFromDataPlane(vsp.bridgeName, outDpInterfaceName); err != nil {
		klog.Errorf("Error occurred in deleting Port from Bridge: %v", err)
		return nil, err
	}
	klog.Info("Output Port Deleted from Bridge Successfully")
	out := new(pb.Empty)
	return out, nil
}

// GetDevices function to get all the devices with the given context and Empty
// It will return the DeviceListResponse and error
func (vsp *mrvlVspServer) GetDevices(ctx context.Context, in *pb.Empty) (*pb.DeviceListResponse, error) {
	klog.Info("Received GetDevices() request")
	devices := make(map[string]*pb.Device)
	if vsp.deviceStore == nil {
		return nil, errors.New("device Store is empty")
	}
	if vsp.isDPUMode {
		for _, mrvlDeviceInfo := range vsp.deviceStore {
			devices[mrvlDeviceInfo.secInterfaceName] = &pb.Device{
				ID:     mrvlDeviceInfo.secInterfaceName,
				Health: mrvlDeviceInfo.health,
			}
		}
	} else {
		for _, mrvlDeviceInfo := range vsp.deviceStore {
			devices[mrvlDeviceInfo.pciAddress] = &pb.Device{
				ID:     mrvlDeviceInfo.pciAddress,
				Health: mrvlDeviceInfo.health,
			}
		}
	}
	return &pb.DeviceListResponse{
		Devices: devices,
	}, nil
}

// SetNumVfs function to set the number of VFs with the given context and VfCount
func (vsp *mrvlVspServer) SetNumVfs(ctx context.Context, in *pb.VfCount) (*pb.VfCount, error) {
	klog.Infof("Received SetNumVfs() request: VfCnt: %v", in.VfCnt)
	if vsp.isDPUMode {
		return nil, errors.New("SetNumVfs is not supported in DPU Mode")
	}
	pciAddress, err := mrvlutils.GetPCIByDeviceID(HostDeviceID)
	if pciAddress == "" || err != nil {
		return nil, errors.New("PCI Address not found")
	}
	vfcnt := in.VfCnt
	if vfcnt < 0 {
		return nil, errors.New("invalid VF Count")
	}

	// reset sriov_numvfs to 0 before setting to a number
	resetCmd := exec.Command("sh", "-c", fmt.Sprintf("echo 0 > /sys/bus/pci/devices/%s/sriov_numvfs", pciAddress))
	_, err = resetCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to reset sriov_numvfs to 0: %v", err)
	}

	// set sriov_numvfs to the given number
	cmd := exec.Command("sh", "-c", fmt.Sprintf("echo %d > /sys/bus/pci/devices/%s/sriov_numvfs", vfcnt, pciAddress))
	_, err = cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	out := &pb.VfCount{
		VfCnt: vfcnt,
	}
	return out, nil
}

func (vsp *mrvlVspServer) configureIP(dpuMode bool) (pb.IpPort, error) {
	var addr string
	var deviceID string
	if dpuMode {
		addr = IPv6AddrDpu
		deviceID = DPUdeviceID
	} else {
		addr = IPv6AddrHost
		deviceID = HostDeviceID
	}
	ifName, err := mrvlutils.GetNameByDeviceID(deviceID)
	if err != nil {
		klog.Errorf("Error occurred in getting Interface Name: %v", err)
		return pb.IpPort{}, err
	}
	klog.Infof("Interface Name: %s", ifName)
	err = enableIPV6LinkLocal(ifName, addr)
	addr = IPv6AddrDpu
	if err != nil {
		klog.Errorf("Error occurred in enabling IPv6 Link local Address: %v", err)
		return pb.IpPort{}, err
	}
	var connStr string
	if dpuMode {
		connStr = "[" + addr + "%" + ifName + "]"
	} else {
		connStr = "[" + addr + "%25" + ifName + "]"
	}
	klog.Infof("IPv6 Link Local Address Enabled IfName: %v, Connection String: %s", ifName, connStr)
	return pb.IpPort{
		Ip:   connStr,
		Port: DefaultPort,
	}, nil

}

func linkHasAddrgenmodeEui64(interfaceName string) bool {
	out, err := exec.Command("ip", "-d", "link", "show", "dev", interfaceName).Output()
	return err == nil && strings.Contains(string(out), "addrgenmode eui64")
}

// enableIPV6LinkLocal function to enable the IPv6 Link Local Address on the given Interface Name
// It will return the error
func enableIPV6LinkLocal(interfaceName string, ipv6Addr string) error {
	// Tell NetworkManager to not manage our interface.
	err1 := exec.Command("nsenter", "-t", "1", "-m", "-u", "-n", "-i", "--", "nmcli", "device", "set", interfaceName, "managed", "no").Run()
	if err1 != nil {
		// This error may be fine. Maybe our host doesn't even run
		// NetworkManager. Ignore.
		klog.Infof("nmcli device set %s managed no failed with error %v", interfaceName, err1)
	}

	optimistic_dad_file := "/proc/sys/net/ipv6/conf/" + interfaceName + "/optimistic_dad"
	err1 = os.WriteFile(optimistic_dad_file, []byte("1"), os.ModeAppend)
	if err1 != nil {
		klog.Errorf("Error setting %s: %v", optimistic_dad_file, err1)
	}

	if linkHasAddrgenmodeEui64(interfaceName) {
		// Kernel may require that the SDP interfaces are up at all times (RHEL-90248).
		// If the addrgenmode is already eui64, assume we are fine and don't need to reset
		// it (and don't need to toggle the link state).
	} else {
		// Ensure to set addrgenmode and toggle link state (which can result in creating
		// the IPv6 link local address. Ignore errors here.
		exec.Command("ip", "link", "set", interfaceName, "addrgenmode", "eui64").Run()
		exec.Command("ip", "link", "set", interfaceName, "down").Run()
	}

	err := exec.Command("ip", "link", "set", interfaceName, "up").Run()
	if err != nil {
		return fmt.Errorf("Error setting link %s up: %v", interfaceName, err)
	}

	err = exec.Command("ip", "addr", "replace", ipv6Addr+"/64", "dev", interfaceName, "optimistic").Run()
	if err != nil {
		return fmt.Errorf("Error configuring IPv6 address %s/64 on link %s: %v", ipv6Addr, interfaceName, err)
	}
	return nil
}

// Listen function to listen on the UNIX domain socket
// It will return the Listener and error
func (vsp *mrvlVspServer) Listen() (net.Listener, error) {
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
	klog.Infof("gRPC server is listening on %v", listener.Addr())

	return listener, nil
}

// Serve function to serve the gRPC server on the given listener
// It will return the error
func (vsp *mrvlVspServer) Serve(listener net.Listener) error {
	vsp.wg.Add(1)
	go func() {
		vsp.version = Version
		klog.Infof("Starting Marvell VSP Server: Version: %s", vsp.version)
		if err := vsp.grpcServer.Serve(listener); err != nil {
			vsp.done <- err
		} else {
			vsp.done <- nil
		}
		klog.Info("Stopping Marvell VSP Server")
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

func (vsp *mrvlVspServer) Stop() {
	if err := vsp.mrvlDP.DeleteDataplane(vsp.bridgeName); err != nil {
		klog.Errorf("Error occurred during DeleteDataPlane: %v", err)
	}
	if err := vsp.CleanVethPairs(); err != nil {
		klog.Errorf("Error occurred during clearning Veth-Peers: %v", err)
	}
	vsp.grpcServer.Stop()
	vsp.done <- nil
	vsp.startedWg.Wait()

}
func WithPathManager(pathManager utils.PathManager) func(*mrvlVspServer) {
	return func(vsp *mrvlVspServer) {
		vsp.pathManager = pathManager
	}
}

func NewMarvellVspServer(opts ...func(*mrvlVspServer)) *mrvlVspServer {
	var mode string
	flag.StringVar(&mode, "mode", "", "Mode for the daemon, can be either host or dpu")
	options := zap.Options{
		Development: true,
		Level:       zapcore.DebugLevel,
	}
	options.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&options)))
	vsp := &mrvlVspServer{
		log:          ctrl.Log.WithName("MarvellVsp"),
		pathManager:  *utils.NewPathManager("/"),
		deviceStore:  make(map[string]mrvlDeviceInfo),
		done:         make(chan error),
		mrvlDP:       ovsdp.NewOvsDP(),
		networkStore: make(map[string]mrvlNfPortMap),
		isNF:         isNf,
	}
	if DataPlaneType == "debug" {
		vsp.mrvlDP = debugdp.NewDebugDP()
	}

	for _, opt := range opts {
		opt(vsp)
	}

	return vsp
}

func main() {
	mrvlVspServer := NewMarvellVspServer()
	listener, err := mrvlVspServer.Listen()

	if err != nil {
		mrvlVspServer.log.Error(err, "Failed to Listen Marvell VSP server")
		return
	}
	err = mrvlVspServer.Serve(listener)
	if err != nil {
		mrvlVspServer.log.Error(err, "Failed to serve Marvell VSP server")
		return
	}
}
