package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	opi "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"gopkg.in/yaml.v2"
	"k8s.io/klog/v2"
)

type MarvellConfig struct {
	DeviceID string `yaml:"deviceID"`
	VendorID string `yaml:"vendorID"`
}

func readConfig() MarvellConfig {
	// read config file
	config_path := "/config.yaml"
	if config_path == "" {
		log.Fatalf("Marvell Plugin: config file not found")
	}

	configFile, err := os.ReadFile(config_path)
	if err != nil {
		klog.Errorf("Marvell Plugin: Error reading config file: %s", err)
	}

	var config MarvellConfig
	err = yaml.Unmarshal(configFile, &config)
	if err != nil {
		klog.Errorf("Marvell Plugin: Error parsing config file: %s", err)
	}
	klog.Infof("Config file is: %v", config)
	return config
}

const (
	DaemonBaseDir          string = "/var/run/dpu-daemon/"
	VendorPluginSocketPath string = DaemonBaseDir + "vendor-plugin/vendor-plugin.sock"
)

type server struct {
	pb.UnimplementedLifeCycleServiceServer
	pb.UnimplementedNetworkFunctionServiceServer
	opi.UnimplementedBridgePortServiceServer
}

func (s *server) Init(ctx context.Context, in *pb.InitRequest) (*pb.IpPort, error) {
	//service to send DPU IP to Daemon via UNIX socket
	klog.Infof("Received Init request with DpuMode:%v", in.DpuMode)
	//based on Dpu_mode, fetch the corresponding IP
	ipPort, err := s.fetchIP(in.DpuMode)
	return &pb.IpPort{
		Ip:   ipPort.Ip,
		Port: ipPort.Port,
	}, err
}

func (s *server) CreateBridgePort(ctx context.Context, in *opi.CreateBridgePortRequest) (*opi.BridgePort, error) {
	return nil, nil
}

func (s *server) DeleteBridgePort(ctx context.Context, in *opi.DeleteBridgePortRequest) (*emptypb.Empty, error) {
	return nil, nil
}

func (s *server) CreateNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {
        return nil, nil
}

func (s *server) DeleteNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {
        return nil, nil
}

// FetchIp Will fetch the IPv6 Address of VF being used for Comm Channel based on dpuMode on either host or DPU
func (s *server) fetchIP(dpuMode bool) (pb.IpPort, error) {
	if dpuMode {
		IfName, err := getDPUInterfaceName()
		if err != nil {
			klog.Errorf("Error Occured in getting Interface Name: %v", err)
			return pb.IpPort{}, err
		}
		IfDetails, err := net.InterfaceByName(IfName)
		if err != nil {
			klog.Errorf("Error Occured in getting InterfaceDetails By Name: %v", err)
			return pb.IpPort{}, err
		}
		// Get the interface addresses
		addrs, err := IfDetails.Addrs()
		if err != nil {
			klog.Errorf("Error Occured in getting IP Address: %v", err)
			return pb.IpPort{}, err
		}
		var IPv6Addres string
		// Iterate over the addresses
		for _, addr := range addrs {
			// Check if the address is an IPv6 address
			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() == nil {
				IPv6Addres = ipNet.IP.String()
				klog.Infof("IPv6 Address is :%v", ipNet.IP)
			}
		}
		ConnStr := "[" + IPv6Addres + "%" + IfName + "]"
		return pb.IpPort{
			// Ip:   "[fe80::7cf2:2fff:fe10:e18b]", //fetch the Link Local address of DPU
			Ip:   ConnStr,
			Port: 8085,
		}, nil
	} else {
		ifName, err := getHostInterfaceName()
		if err != nil {
			klog.Errorf("Erorr Occured in getting HostInterfaceName: %v", err)
			return pb.IpPort{}, err
		}
		LinkLocalIpv6, err := getNeighbourIPs(ifName)
		if err != nil {
			klog.Errorf("Error Occured in getting Neighbour IP: %v", err)
			return pb.IpPort{}, err
		}
		ConnStr := LinkLocalIpv6 + "%25" + ifName
		klog.Infof("IPv6 Address: %s", LinkLocalIpv6)
		ipPort := pb.IpPort{
			//Ip:   "[fe80::7cf2:2fff:fe10:e18b%enp5s0f0]",
			Ip:   ConnStr,
			Port: 8085,
		}
		return pb.IpPort{
			Ip:   ipPort.Ip,
			Port: ipPort.Port,
		}, nil

	}
}

// getDPUInterfaceName will return the VF1 Interface Name on DPU Side
func getDPUInterfaceName() (string, error) {
	// /sys/bus/pci/devices/0002:0f:00.1/net
	baseNsenterCmd := "nsenter -t 1 -m -u -n -i -- "
	cmd := baseNsenterCmd + " lspci -d 177d:a0f7 -n | awk '{print $1}'"
	cmdExec := exec.Command("/bin/sh", "-c", cmd)
	listOfVfs, err := cmdExec.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Error Occured in  running command: %s, Reason:%v", cmd, err)
	}
	ListOfVfsStr := string(listOfVfs)
	vfs := strings.Split(ListOfVfsStr, "\n")
	//VF0 Maps to PF and VF1 is the first VF
	var vf1 string
	//get the vf1
	for _, vf := range vfs {
		if strings.HasSuffix(vf, ".1") {
			vf1 = vf
			break
		}
	}
	klog.Infof("VF1 is :%s", vf1)
	path := "/sys/bus/pci/devices/" + vf1 + "/net/"
	cmd = "ls " + path
	cmdExec = exec.Command("/bin/sh", "-c", cmd)
	ifname, err := cmdExec.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Error occured in running command:%s, Reason: %v", cmd, err)
	}
	ifn := string(ifname)
	ifn = strings.TrimSuffix(ifn, "\n")
	return ifn, nil
}

// getHostInterfaceName will return the VF1 Interface Name on Host Side
func getHostInterfaceName() (string, error) {
	baseNsenterCmd := "nsenter -t 1 -m -u -n -i -- " //namespace enter to  -t 1 (init process of Host), -m = mount; -u = identifiers; -n = network; -i = IPC;
	config := readConfig()
	klog.Infof("Config inside getHostINterfaceName: %v", config)
	cmd := baseNsenterCmd + " lspci -d " + config.VendorID + ":" + config.DeviceID + " -n | awk '{print $1}'"
	// cmd := baseNsenterCmd + " lspci -d 177d:b200 -n | awk '{print $1}'"
	cmdExec := exec.Command("/bin/sh", "-c", cmd)
	ListOfPFs, err := cmdExec.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Error occured in runnning command: %s, Reason: %v", cmd, err)
	}
	klog.Infof("ListOfPFs are: %v", string(ListOfPFs))
	pf := string(ListOfPFs)
	if len(pf) == 8 {
		pf = "0000:" + pf
	}
	pf = strings.TrimSuffix(pf, "\n")
	path := "/sys/bus/pci/devices/" + pf + "/net/"
	cmd = "ls " + path
	cmdExec = exec.Command("/bin/sh", "-c", cmd)
	ifname, err := cmdExec.CombinedOutput()
	klog.Infof("Interface Name on Host: %v", string(ifname))
	if err != nil {
		return "", fmt.Errorf("Error occured in running command: %s, Reason: %v", cmd, err)
	}
	ifn := string(ifname)
	ifn = strings.TrimSuffix(ifn, "\n")
	return ifn, nil
}

// getNeighbourIPs will return  the ipv6 Neighbour address of an Interface with Name Interface Name as an argument
func getNeighbourIPs(Ifname string) (string, error) {
	baseNsenterCmd := "nsenter -t 1 -m -u -n -i -- "
	cmdS := baseNsenterCmd + "/usr/bin/ping6 -c 2 -I " + Ifname + " ff02::1 &> /dev/null"
	cmd := exec.Command("/bin/sh", "-c", cmdS)
	_, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("Error occured in running command: %s, Reason: %v", cmd, err)
	}
	// cmdS = "ip neigh show dev " + Ifname
	cmd = exec.Command("ip", "neigh", "show", "dev", Ifname)
	//neighbours contains all the neighbour of interface ifname
	neighbours, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("Error occured in running command: %s, Reason: %v", cmd, err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(neighbours)))
	var IPv6Address string
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		// Check if the line contains an IPv6 address
		if len(fields) >= 3 && strings.HasPrefix(fields[0], "fe80::") {
			IPv6Address = fields[0]
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return IPv6Address, nil
}

func main() {
	//UNIX socket setup
	if err := os.RemoveAll(VendorPluginSocketPath); err != nil {
		log.Fatalf("Failed to remove old socket: %v", err)
	}
	dir := filepath.Dir(VendorPluginSocketPath)
	os.MkdirAll(dir, 0755)
	lis, err := net.Listen("unix", VendorPluginSocketPath)
	if err != nil {
		log.Fatalf("failed to listen UNIX domain socket: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterNetworkFunctionServiceServer(s,&server{})
	pb.RegisterLifeCycleServiceServer(s, &server{})
	opi.RegisterBridgePortServiceServer(s, &server{})
	log.Printf("gRPC server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve on UNIX: %v", err)
	}
}
