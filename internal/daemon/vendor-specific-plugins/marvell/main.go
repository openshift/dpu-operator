package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	ghw "github.com/jaypipes/ghw"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/openshift/dpu-operator/internal/utils"
	opi "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"github.com/vishvananda/netlink"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"k8s.io/klog/v2"
)

const (
	SysBusPci string = "/sys/bus/pci/devices"
)

type server struct {
	pb.UnimplementedLifeCycleServiceServer
	pb.UnimplementedNetworkFunctionServiceServer
	pb.UnimplementedDeviceServiceServer
	opi.UnimplementedBridgePortServiceServer
}

func (s *server) Init(ctx context.Context, in *pb.InitRequest) (*pb.IpPort, error) {
	klog.Infof("Received Init request with DpuMode:%v", in.DpuMode)
	ipPort, err := s.fetchIP(in.DpuMode)
	return &pb.IpPort{
		Ip:   ipPort.Ip,
		Port: ipPort.Port,
	}, err
}

func (s *server) CreateBridgePort(ctx context.Context, in *opi.CreateBridgePortRequest) (*opi.BridgePort, error) {
	out := new(opi.BridgePort)
	return out, nil
}

func (s *server) DeleteBridgePort(ctx context.Context, in *opi.DeleteBridgePortRequest) (*emptypb.Empty, error) {
	out := new(emptypb.Empty)
	return out, nil
}

func (s *server) CreateNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {
	out := new(pb.Empty)
	return out, nil
}

func (s *server) DeleteNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {
	out := new(pb.Empty)
	return out, nil
}

func (s *server) GetDevices(ctx context.Context, in *pb.Empty) (*pb.DeviceListResponse, error) {
	//Add functionality to list all the devices using ghw
	out := new(pb.DeviceListResponse)
	return out, nil
}

// FetchIp Will fetch the IPv6 Address of VF being used for Comm Channel based on dpuMode on either host or DPU
func (s *server) fetchIP(dpuMode bool) (pb.IpPort, error) {
	if dpuMode {
		IfName, err := getInterfaceName("a0f7")
		if err != nil {
			klog.Errorf("Error Occured in getting Interface Name: %v", err)
			return pb.IpPort{}, err
		}
		err = enableIPV6LinkLocal(IfName)
		if err != nil {
			klog.Errorf("Error occured in enabling IPv6 Link local Address: %v", err)
		}
		IfDetails, err := net.InterfaceByName(IfName)
		if err != nil {
			klog.Errorf("Error Occured in getting InterfaceDetails By Name: %v", err)
			return pb.IpPort{}, err
		}
		addrs, err := IfDetails.Addrs()
		if err != nil {
			klog.Errorf("Error Occured in getting IP Address: %v", err)
			return pb.IpPort{}, err
		}
		var IPv6Addres string
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() == nil {
				IPv6Addres = ipNet.IP.String()
				klog.Infof("IPv6 Address is :%v", ipNet.IP)
			}
		}
		ConnStr := "[" + IPv6Addres + "%" + IfName + "]"
		return pb.IpPort{
			Ip:   ConnStr,
			Port: 8085,
		}, nil
	} else {
		klog.Infof("Host Mode")
		ifName, err := getInterfaceName("b900")
		if err != nil {
			klog.Errorf("Erorr Occured in getting HostInterfaceName: %v", err)
			return pb.IpPort{}, err
		}
		klog.Infof("Interface Name: %s", ifName)
		err = enableIPV6LinkLocal(ifName)
		if err != nil {
			klog.Errorf("Error occured in enabling IPv6 Link local Address: %v", err)
		}
		LinkLocalIpv6, err := getNeighbourIPs(ifName)
		if err != nil {
			klog.Errorf("Error Occured in getting Neighbour IP: %v", err)
			return pb.IpPort{}, err
		}
		ConnStr := LinkLocalIpv6 + "%25" + ifName
		klog.Infof("Interface Name: %s", ifName)
		klog.Infof("IPv6 Address: %s", LinkLocalIpv6)
		ipPort := pb.IpPort{
			Ip:   ConnStr,
			Port: 8085,
		}
		return pb.IpPort{
			Ip:   ipPort.Ip,
			Port: ipPort.Port,
		}, nil
	}
}

func GetPfName(vf string) (string, error) {
	pfSymLink := filepath.Join(SysBusPci, vf, "net")
	_, err := os.Lstat(pfSymLink)
	if err != nil {
		return "", err
	}

	files, err := os.ReadDir(pfSymLink)
	if err != nil {
		return "", err
	}

	if len(files) < 1 {
		return "", fmt.Errorf("PF network device not found")
	}

	return strings.TrimSpace(files[0].Name()), nil
}

func getInterfaceName(deviceID string) (string, error) {
	targetVendorID := "177d"
	targetDeviceID := deviceID
	pci, err := ghw.PCI()
	if err != nil {
		log.Fatalf("Error getting PCI info: %v", err)
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
	klog.Infof("PCI Address: %s", pciAddress)
	ifname, err := GetPfName(pciAddress)
	return ifname, err
}

// code to enable IPv6 link local on an interface
func enableIPV6LinkLocal(interfaceName string) error {
	baseNsenterCmd := "nsenter -t 1 -m -u -n -i -- "
	nmcliCmdStr := baseNsenterCmd + "nmcli con add type ethernet ifname " + interfaceName + " ipv6.method link-local"
	nmcliCmd := exec.Command("/bin/sh", "-c", nmcliCmdStr)
	if err := nmcliCmd.Run(); err != nil {
		return err
	}
	// wait for interface to get IPv6 Link local address
	time.Sleep(5 * time.Second)
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return err
	}

	address, err := netlink.AddrList(link, netlink.FAMILY_V6)
	if err != nil {
		return err
	}

	hasIPv6 := false
	for _, addr := range address {
		if addr.IP.To4() == nil && addr.IP.To16() != nil {
			hasIPv6 = true
		}
	}
	if !hasIPv6 {
		log.Fatalf("There is no IPv6 Address ")
	}

	cmdS := baseNsenterCmd + "/usr/bin/ping6 -c 2 -I " + interfaceName + " ff02::1 &> /dev/null"
	cmd := exec.Command("/bin/sh", "-c", cmdS)
	_, err = cmd.Output()
	if err != nil {
		return err
	}
	time.Sleep(5 * time.Second)
	klog.Infof("Successfully enabled IPv6 link-local address on interface %s\n", interfaceName)
	return nil
}

// code to return neighbourIP link local IPv6 of an interface
func getNeighbourIPs(Ifname string) (string, error) {
	link, err := netlink.LinkByName(Ifname)
	if err != nil {
		return "", fmt.Errorf("error occurred in getting link by name: %v", err)
	}

	neighbours, err := netlink.NeighList(link.Attrs().Index, netlink.FAMILY_V6)
	if err != nil {
		return "", fmt.Errorf("error occurred in getting neighbour list: %v", err)
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

func main() {
	err := (&utils.PathManager{}).EnsureSocketDirExists((&utils.PathManager{}).VendorPluginSocket())
	if err != nil {
		log.Fatalf("failed to listen UNIX domain socket: %v", err)
	}
	lis, err := net.Listen("unix", (&utils.PathManager{}).VendorPluginSocket())
	if err != nil {
		log.Fatalf("failed to listen UNIX domain socket: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterNetworkFunctionServiceServer(s, &server{})
	pb.RegisterLifeCycleServiceServer(s, &server{})
	pb.RegisterDeviceServiceServer(s, &server{})
	opi.RegisterBridgePortServiceServer(s, &server{})
	log.Printf("gRPC server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve on UNIX: %v", err)
	}
}
