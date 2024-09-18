package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	ghw "github.com/jaypipes/ghw"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/openshift/dpu-operator/internal/utils"
	opi "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"github.com/vishvananda/netlink"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/kind/pkg/errors"
)

const (
	SysBusPci    string = "/sys/bus/pci/devices"
	VendorID     string = "177d"
	DPUdeviceID  string = "a0f7"
	HostDeviceID string = "b900"
	DefaultPort  int32  = 8085
	Version      string = "0.0.1"
)

type mrvlVspServer struct {
	pb.UnimplementedLifeCycleServiceServer
	pb.UnimplementedNetworkFunctionServiceServer
	pb.UnimplementedDeviceServiceServer
	opi.UnimplementedBridgePortServiceServer
	log         logr.Logger
	grpcServer  *grpc.Server
	wg          sync.WaitGroup
	done        chan error
	startedWg   sync.WaitGroup
	pathManager utils.PathManager
	version     string
	isDPUMode   bool
}

// Init function to initialize the Marvell VSP Server with the given context and InitRequest
// It will return the IpPort and error
func (vsp *mrvlVspServer) Init(ctx context.Context, in *pb.InitRequest) (*pb.IpPort, error) {
	klog.Infof("Received Init() request  DpuMode: %v", in.DpuMode)
	vsp.isDPUMode = in.DpuMode
	ipPort, err := vsp.fetchIP(in.DpuMode)
	return &pb.IpPort{
		Ip:   ipPort.Ip,
		Port: ipPort.Port,
	}, err
}

// CreateBridgePort function to create a bridge port with the given context and CreateBridgePortRequest
// It will return the BridgePort and error
func (vsp *mrvlVspServer) CreateBridgePort(ctx context.Context, in *opi.CreateBridgePortRequest) (*opi.BridgePort, error) {
	klog.Infof("Received CreateBridgePort() request  BridgePortId: %v", in.BridgePortId)
	out := new(opi.BridgePort)
	return out, nil
}

// DeleteBridgePort function to delete a bridge port with the given context and DeleteBridgePortRequest
// It will return the Empty and error
func (vsp *mrvlVspServer) DeleteBridgePort(ctx context.Context, in *opi.DeleteBridgePortRequest) (*emptypb.Empty, error) {
	klog.Infof("Received DeleteBridgePort() request  Name: %v , AllowMissing: %v", in.Name, in.AllowMissing)
	out := new(emptypb.Empty)
	return out, nil
}

// CreateNetworkFunction function to create a network function with the given context and NFRequest
// It will return the Empty and error
func (vsp *mrvlVspServer) CreateNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {
	klog.Infof("Received CreateNetworkFunction() request  Input: %v , Output: %v", in.Input, in.Output)
	out := new(pb.Empty)
	return out, nil
}

// DeleteNetworkFunction function to delete a network function with the given context and NFRequest
// It will return the Empty and error
func (vsp *mrvlVspServer) DeleteNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {
	klog.Infof("Received DeleteNetworkFunction() request  Input: %v , Output: %v", in.Input, in.Output)
	out := new(pb.Empty)
	return out, nil
}

// GetDevices function to get all the devices with the given context and Empty
// It will return the DeviceListResponse and error
func (vsp *mrvlVspServer) GetDevices(ctx context.Context, in *pb.Empty) (*pb.DeviceListResponse, error) {
	klog.Info("Received GetDevices() request")
	out := new(pb.DeviceListResponse)
	return out, nil
}

// dpuIpPort function to get the IPv6 Address of DPU being used for Comm Channel
// It will return the IpPort and error
func (vsp *mrvlVspServer) dpuIpPort() (pb.IpPort, error) {
	klog.Infof("GetInterface Name  DPUdeviceID: %v", DPUdeviceID)
	IfName, err := getInterfaceName(DPUdeviceID)
	if err != nil {
		klog.Errorf("Error occurred in getting Interface Name: %v", err)
		return pb.IpPort{}, err
	}
	err = enableIPV6LinkLocal(IfName)
	if err != nil {
		klog.Errorf("Error occurred in enabling IPv6 Link local Address: %v", err)
		return pb.IpPort{}, err
	}
	klog.Infof("IPv6 Link Local Address Enabled  IfName: %v", IfName)
	IfDetails, err := net.InterfaceByName(IfName)
	if err != nil {
		klog.Errorf("Error occurred in getting InterfaceDetails By Name: %v", err)
		return pb.IpPort{}, err
	}
	addrs, err := IfDetails.Addrs()
	if err != nil {
		klog.Errorf("Error occurred in getting IP Address: %v", err)
		return pb.IpPort{}, err
	}
	var IPv6Addres string
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() == nil {
			IPv6Addres = ipNet.IP.String()
		}
	}
	if IPv6Addres == "" {
		klog.Info("IPv6 Address is not found")
		return pb.IpPort{}, errors.New("There is no IPv6 Address")
	}
	klog.Infof("IPv6 Address: %s", IPv6Addres)
	ConnStr := "[" + IPv6Addres + "%" + IfName + "]"
	klog.Infof("Connection String: %s", ConnStr)
	return pb.IpPort{
		Ip:   ConnStr,
		Port: DefaultPort,
	}, nil
}

// hostIpPort function to get the IPv6 Address of Host being used for Comm Channel
// It will return the IpPort and error
func (vsp *mrvlVspServer) hostIpPort() (pb.IpPort, error) {
	klog.Infof("GetInterface Name  HostDeviceID: %v", HostDeviceID)
	// Get the Interface Name on Host for the given Device ID
	ifName, err := getInterfaceName(HostDeviceID)
	if err != nil {
		klog.Errorf("Error occurred in getting Interface Name: %v", err)
		return pb.IpPort{}, err
	}
	klog.Infof("Interface Name  InterfaceName: %v", ifName)
	err = enableIPV6LinkLocal(ifName)
	if err != nil {
		vsp.log.Error(err, "Error occurred in enabling IPv6 Link local Address: %v")
		return pb.IpPort{}, err
	}
	klog.Infof("IPv6 Link Local Address Enabled  IfName: %v", ifName)
	klog.Infof("Get Neighbour IP  InterfaceName: %v", ifName)
	LinkLocalIpv6, err := getNeighbourIPs(ifName)
	if err != nil {
		klog.Errorf("Error occurred in getting Neighbour IP: %v", err)
		return pb.IpPort{}, err
	}
	ConnStr := "[" + LinkLocalIpv6 + "%25" + ifName + "]"
	klog.Infof("IPv6 Address: %s", LinkLocalIpv6)
	klog.Infof("Connection String: %s", ConnStr)
	return pb.IpPort{
		Ip:   ConnStr,
		Port: DefaultPort,
	}, nil
}

// FetchIp Will fetch the IPv6 Address of VF being used for Comm Channel based on dpuMode on either host or DPU
func (vsp *mrvlVspServer) fetchIP(dpuMode bool) (pb.IpPort, error) {
	if dpuMode {
		vsp.log.Info("DPU Mode")
		return vsp.dpuIpPort()
	} else {
		vsp.log.Info("Host Mode")
		return vsp.hostIpPort()
	}
}

// GetPfName function to get the PF Name of the given  PCI Address
// It will return the PF Name and error
func GetPfName(pciAddress string) (string, error) {
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

// getInterfaceName function to get the Interface Name of the given Device ID and vendor ID
// It will return the Interface Name and error
func getInterfaceName(deviceID string) (string, error) {
	targetVendorID := VendorID
	targetDeviceID := deviceID
	pci, err := ghw.PCI()
	if err != nil {
		return "", err
	}
	devices := pci.ListDevices()
	var pciAddress string
	for _, device := range devices {
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
	ifname, err := GetPfName(pciAddress)
	if err != nil {
		return "", err
	}
	return ifname, nil
}

// enableIPV6LinkLocal function to enable the IPv6 Link Local Address on the given Interface Name
// It will return the error
func enableIPV6LinkLocal(interfaceName string) error {
	baseNsenterCmd := "nsenter -t 1 -m -u -n -i -- "
	nmcliCmdStr := baseNsenterCmd + "nmcli con add type ethernet ifname " + interfaceName + " ipv6.method link-local"
	nmcliCmd := exec.Command("/bin/sh", "-c", nmcliCmdStr)
	if err := nmcliCmd.Run(); err != nil {
		return err
	}

	// wait for interface to get IPv6 Link local address
	time.Sleep(3 * time.Second)
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return err
	}

	//returns the list of IPv6 addresses on the given interface
	address, err := netlink.AddrList(link, netlink.FAMILY_V6)
	if err != nil {
		return err
	}

	if len(address) == 0 {
		return errors.Errorf("IPv6 address not found on interface %s", interfaceName)
	}

	//ping to get the IPv6 Neighbour Entry in Arp Table
	cmdS := baseNsenterCmd + "/usr/bin/ping6 -c 2 -I " + interfaceName + " ff02::1 &> /dev/null"
	cmd := exec.Command("/bin/sh", "-c", cmdS)
	_, err = cmd.Output()
	if err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	return nil
}

// getNeighbourIPs function to get the Neighbour IP of the given Interface Name
// It will return the Neighbour IP and error
func getNeighbourIPs(Ifname string) (string, error) {
	link, err := netlink.LinkByName(Ifname)
	if err != nil {
		return "", err
	}

	neighbours, err := netlink.NeighList(link.Attrs().Index, netlink.FAMILY_V6)
	if err != nil {
		return "", err
	}
	if len(neighbours) == 0 {
		return "", errors.New("neighbour list is empty")
	}
	var IPv6Address string
	for _, neighbour := range neighbours {
		if strings.HasPrefix(neighbour.IP.String(), "fe80::") {
			IPv6Address = neighbour.IP.String()
			break
		}
	}

	return IPv6Address, nil
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
	pb.RegisterNetworkFunctionServiceServer(vsp.grpcServer, &mrvlVspServer{})
	pb.RegisterLifeCycleServiceServer(vsp.grpcServer, &mrvlVspServer{})
	pb.RegisterDeviceServiceServer(vsp.grpcServer, &mrvlVspServer{})
	opi.RegisterBridgePortServiceServer(vsp.grpcServer, &mrvlVspServer{})
	// vsp.log.Info("gRPC server is listening", "listener.Addr()", listener.Addr())
	klog.Infof("gRPC server is listening on : %v", listener.Addr())

	return listener, nil
}

// Serve function to serve the gRPC server on the given listener
// It will return the error
func (vsp *mrvlVspServer) Serve(listener net.Listener) error {
	vsp.wg.Add(1)
	go func() {
		vsp.version = Version
		klog.Infof("Starting Marvell VSP Server, Version: %s", vsp.version)
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

func NewMarvellVspServer(opts ...func(*mrvlVspServer)) *mrvlVspServer {
	vsp := &mrvlVspServer{
		log: ctrl.Log.WithName("MarvellVsp"),
	}

	for _, opt := range opts {
		opt(vsp)
	}

	return vsp
}

func main() {
	// mrvlVspServer := commchannel.NewMarvellVspServer()
	mrvlVspServer := NewMarvellVspServer()
	listener, err := mrvlVspServer.Listen()
	if err != nil {
		klog.Fatalf("Failed to Listen Marvell VSP server: %v", err)
		return
	}
	// klog.Infof("Marvell VSP server started")
	err = mrvlVspServer.Serve(listener)
	if err != nil {
		klog.Fatalf("Failed to serve Marvell VSP server: %v", err)
		return
	}
}
