package main

import (
	"context"
	"flag"
	"fmt"
	"net"
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
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"k8s.io/klog/v2"
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
)

type intelNetSecVspServer struct {
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
	dpuIdentifier  plugin.DpuIdentifier
	dpuPcieAddress string
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
	vsp.log.Info("getVFs(): found VFs", "NumVFs", numVfs, "DpuPcieAddress", vsp.dpuPcieAddress)
	return pciVFAddresses, nil
}

func (vsp *intelNetSecVspServer) configureIP(dpuMode bool) (pb.IpPort, error) {
	var ifName string
	var addr string
	var err error
	if dpuMode {
		// All NetSec DPU devices have the same internal PCIe Addresses. Netdev names can change with each RHEL release.
		ifName, err = vspnetutils.GetNetDevNameFromPCIeAddr(vsp.platform, IntelNetSecDpuBackplanef2PCIeAddress)
		if err != nil {
			klog.Errorf("Error getting netdev name from PCIe address in DPU mode %s: %v", IntelNetSecDpuBackplanef2PCIeAddress, err)
			return pb.IpPort{}, err
		}
		addr = IPv6AddrDpu
	} else {
		ifName, err = vspnetutils.GetNetDevNameFromPCIeAddr(vsp.platform, vsp.dpuPcieAddress)
		if err != nil {
			klog.Errorf("Error getting netdev name from PCIe address in Host mode %s: %v", vsp.dpuPcieAddress, err)
			return pb.IpPort{}, err
		}
		addr = IPv6AddrHost
	}

	vsp.log.Info("configureIP(): DpuMode", "DpuMode", dpuMode, "IfName", ifName, "Addr", addr)

	err = vspnetutils.EnableIPV6LinkLocal(vsp.fs, ifName, addr)
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

func (vsp *intelNetSecVspServer) Init(ctx context.Context, in *pb.InitRequest) (*pb.IpPort, error) {
	var err error
	klog.Infof("Received Init() request: DpuMode: %v DpuIdentifier: %v", in.DpuMode, in.DpuIdentifier)
	vsp.isDPUMode = in.DpuMode
	vsp.dpuIdentifier = plugin.DpuIdentifier(in.DpuIdentifier)

	vsp.dpuPcieAddress, err = vsp.GetDpuPcieAddress(vsp.dpuIdentifier)
	if err != nil {
		klog.Errorf("Error getting DPU PCIe address: %v", err)
		return nil, err
	}

	ipPort, err := vsp.configureIP(in.DpuMode)

	return &pb.IpPort{
		Ip:   ipPort.Ip,
		Port: ipPort.Port,
	}, err
}

// TODO: Implement this correctly, it needs to handle VETH interfaces for service function chaining.
func (vsp *intelNetSecVspServer) GetDevices(ctx context.Context, in *pb.Empty) (*pb.DeviceListResponse, error) {
	klog.Info("Received GetDevices() request")
	devices := make(map[string]*pb.Device)

	var pfPcieAddress string
	if vsp.isDPUMode {
		pfPcieAddress = IntelNetSecDpuBackplanef2PCIeAddress
	} else {
		pfPcieAddress = vsp.dpuPcieAddress
	}

	vfs, err := vsp.getVFs(pfPcieAddress)
	if err != nil {
		klog.Errorf("Error getting VFs: %v", err)
		return nil, err
	}

	for _, vf := range vfs {
		klog.Infof("Adding device %s to the response", vf)
		devices[vf] = &pb.Device{
			ID:     vf,
			Health: "Healthy",
		}
	}

	return &pb.DeviceListResponse{
		Devices: devices,
	}, nil
}

// TODO: Implement this
func (vsp *intelNetSecVspServer) CreateBridgePort(ctx context.Context, in *opi.CreateBridgePortRequest) (*opi.BridgePort, error) {
	vsp.log.Info("Received CreateBridgePort() request", "BridgePortId", in.BridgePortId, "BridgePortId", in.BridgePortId)
	return &opi.BridgePort{}, nil
}

// TODO: Implement this
func (vsp *intelNetSecVspServer) DeleteBridgePort(ctx context.Context, in *opi.DeleteBridgePortRequest) (*emptypb.Empty, error) {
	vsp.log.Info("Received DeleteBridgePort() request", "Name", in.Name, "AllowMissing", in.AllowMissing)
	return nil, nil
}

// TODO: Implement this
func (vsp *intelNetSecVspServer) CreateNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {
	vsp.log.Info("Received CreateNetworkFunction() request", "Input", in.Input, "Output", in.Output)
	return nil, nil
}

// TODO: Implement this
func (vsp *intelNetSecVspServer) DeleteNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {
	vsp.log.Info("Received DeleteNetworkFunction() request", "Input", in.Input, "Output", in.Output)
	return nil, nil
}

// TODO: FIX ME: This function is not implemented fully for Service Function Chaining.
// SetNumVfs function to set the number of VFs with the given context and VfCount
func (vsp *intelNetSecVspServer) SetNumVfs(ctx context.Context, in *pb.VfCount) (*pb.VfCount, error) {
	klog.Infof("Received SetNumVfs() request: VfCnt: %v", in.VfCnt)
	var err error

	if vsp.isDPUMode {
		err = vspnetutils.SetSriovNumVfs(vsp.fs, IntelNetSecDpuBackplanef2PCIeAddress, int(in.VfCnt))
	} else {
		err = vspnetutils.SetSriovNumVfs(vsp.fs, vsp.dpuPcieAddress, int(in.VfCnt))
	}

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
	klog.Infof("gRPC server is listening on %v", listener.Addr())

	return listener, nil
}

func (vsp *intelNetSecVspServer) Serve(listener net.Listener) error {
	vsp.wg.Add(1)
	go func() {
		vsp.version = Version
		klog.Infof("Starting Intel NetSec VSP Server: Version: %s", vsp.version)
		if err := vsp.grpcServer.Serve(listener); err != nil {
			vsp.done <- err
		} else {
			vsp.done <- nil
		}
		klog.Info("Stopping Intel NetSec VSP Server")
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
		Level:       zapcore.DebugLevel,
	}
	options.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&options)))
	vsp := &intelNetSecVspServer{
		log:         ctrl.Log.WithName("IntelNetSecVsp"),
		pathManager: *utils.NewPathManager("/"),
		done:        make(chan error),
		fs:          afero.NewOsFs(),
		platform:    &platform.HardwarePlatform{},
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
