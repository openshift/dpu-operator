package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	vspnetutils "github.com/openshift/dpu-operator/internal/daemon/vendor-specific-plugins/common"
	sgpb "github.com/openshift/dpu-operator/internal/daemon/vendor-specific-plugins/synaxg/protos/gen"
	"github.com/openshift/dpu-operator/internal/platform"
	"github.com/openshift/dpu-operator/internal/utils"
	opi "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"github.com/spf13/afero"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	Version     string = "0.0.1"
	DefaultPort int32  = 8085

	// SynaXG device identifiers for Marvell OCTEON Fusion CNF105xx (CNF10KA) silicon.
	// VendorID is Marvell's because SynaXG uses Marvell's chip.
	SynaXGVendorID       string = "177d" // Marvell Semiconductor
	SynaXGHostPFDeviceID string = "ba00" // CNF10KA host-side PF (octeon_ep driver)
	SynaXGHostVFDeviceID string = "ba03" // CNF10KA host-side VF
	// Static IPv4 addresses for the gRPC communication channel over VF0.
	// Hardcoded by convention (same pattern as Intel IPU using 192.168.1.x).
	// 192.168.1.3 is always the card/DPU side (OAM), 192.168.1.2 is always the host side (VSP).
	IPv4AddrCard string = "192.168.1.3"
	IPv4AddrHost string = "192.168.1.2"
	IPv4Subnet   string = "24"

	// Number of VFs to create for the communication channel.
	// VF0 is used for gRPC communication between Host VSP and Card OAM.
	CommChannelVfCount int = 1

	// Timeout for waiting for VF to be ready after SR-IOV creation.
	VfSetupTimeout time.Duration = 30 * time.Second

	// Firmware upgrade constants
	FirmwareChunkSize      int           = 1024 * 1024        // 1 MiB chunks for streaming firmware to OAM
	FirmwareUpgradeTimeout time.Duration = 1200 * time.Second // 20 minute timeout for firmware upload+flash

	// DefaultFirmwareImagePath is the default SynaXG firmware container image shipped with
	// this release. It must be updated each release cycle to pin the firmware version that
	// has been validated as part of the full operator stack, ensuring e2e compatibility.
	DefaultFirmwareImagePath string = "quay.io/synaxgcom/sdk-img:latest"
)

// allowedImagePrefixes defines the registry/repository prefixes from which
// firmware images may be pulled. Any image reference that does not start with
// one of these prefixes is rejected to prevent SSRF attacks.
var allowedImagePrefixes = []string{
	"quay.io/synaxgcom/",
}

type synaXGVspServer struct {
	// Embed unimplemented servers so we satisfy all gRPC service interfaces
	pb.UnimplementedLifeCycleServiceServer
	pb.UnimplementedNetworkFunctionServiceServer
	pb.UnimplementedDeviceServiceServer
	opi.UnimplementedBridgePortServiceServer
	pb.UnimplementedHeartbeatServiceServer
	pb.UnimplementedDataProcessingUnitManagementServiceServer

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
	dpuPcieAddress string // PCIe address of the SynaXG PF on the Host

	// OAM connection - used to proxy requests to the DPU-side OAM
	oamConn            *grpc.ClientConn
	oamHeartbeatClient pb.HeartbeatServiceClient
	oamBridgeClient    opi.BridgePortServiceClient
	oamSoftwareClient  sgpb.SoftwareManagementServiceClient
	oamSystemClient    sgpb.SystemManagementServiceClient

	// Communication channel info
	commVfIfName   string   // Host-side VF interface name for gRPC channel
	dataVfPciAddrs []string // PCI addresses of data-path VFs (VF1+), populated by SetNumVfs
}

// =============================================================================
// LifeCycleService Implementation
// =============================================================================

func (vsp *synaXGVspServer) Init(ctx context.Context, in *pb.InitRequest) (*pb.IpPort, error) {
	vsp.log.Info("Received Init() request", "DpuMode", in.DpuMode, "DpuIdentifier", in.DpuIdentifier)
	vsp.isDPUMode = in.DpuMode
	vsp.dpuIdentifier = plugin.DpuIdentifier(in.DpuIdentifier)

	if vsp.isDPUMode {
		return nil, fmt.Errorf("SynaXG VSP does not support DPU mode; the card side runs OAM directly")
	}

	// --- Host-side initialization ---

	// Step 1: Find the SynaXG PF PCIe address from the DPU identifier (serial number)
	pfPcieAddr, err := vsp.findSynaXGPF()
	if err != nil {
		return nil, fmt.Errorf("failed to find SynaXG PF: %v", err)
	}
	vsp.dpuPcieAddress = pfPcieAddr
	vsp.log.Info("Found SynaXG PF", "pcieAddress", pfPcieAddr)

	// Step 2: Create VFs on the PF for the communication channel
	err = vsp.setupCommChannelVF(pfPcieAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to setup communication channel VF: %v", err)
	}
	// Step 3: Configure static IPv4 address on the host-side VF
	ipPort, err := vsp.configureCommChannelIP()
	if err != nil {
		return nil, fmt.Errorf("failed to configure communication channel IP: %v", err)
	}

	// Step 4: Connect to OAM on the card side for proxying requests
	if err := vsp.connectToOAM(ipPort.Ip, ipPort.Port); err != nil {
		vsp.log.Error(err, "Failed to connect to OAM (ping will be unavailable, will retry later)")
		// Non-fatal: VSP can still operate, OAM connection can be retried
	}

	vsp.log.Info("Init() completed", "IP", ipPort.Ip, "Port", ipPort.Port)
	return &ipPort, nil
}

// findSynaXGPF returns the PCIe address of the SynaXG PF.
// It uses dpuIdentifier (format: "SynaXG-dpu-<sanitized-pci-addr>") to locate the exact PF,
// avoiding ambiguity in systems with multiple SynaXG cards.
func (vsp *synaXGVspServer) findSynaXGPF() (string, error) {
	// The identifier is produced by SynaXGDetector.GetDpuIdentifier():
	// fmt.Sprintf("SynaXG-dpu-%s", SanitizePCIAddress(pci.Address))
	// SanitizePCIAddress replaces ':' with '-', so we reverse that.
	const prefix = "SynaXG-dpu-"
	identifier := string(vsp.dpuIdentifier)
	if strings.HasPrefix(identifier, prefix) {
		sanitized := strings.TrimPrefix(identifier, prefix)
		// Restore PCI address: "0000-03-00.0" → "0000:03:00.0"
		expectedAddr := strings.ReplaceAll(sanitized, "-", ":")
		devices, err := vsp.platform.PciDevices()
		if err != nil {
			return "", fmt.Errorf("failed to get PCI devices: %v", err)
		}
		for _, dev := range devices {
			if dev.Address == expectedAddr &&
				dev.Vendor != nil && dev.Vendor.ID == SynaXGVendorID &&
				dev.Product != nil && dev.Product.ID == SynaXGHostPFDeviceID {
				vsp.log.Info("Found SynaXG PF via dpuIdentifier", "pcieAddress", dev.Address)
				return dev.Address, nil
			}
		}
		return "", fmt.Errorf("SynaXG PF not found at expected address %s (from dpuIdentifier %s)", expectedAddr, identifier)
	}

	// Fallback: identifier format not recognised (e.g. old deployments). Scan and return first match.
	vsp.log.Info("dpuIdentifier does not have expected prefix, falling back to first-match PCI scan", "identifier", identifier)
	devices, err := vsp.platform.PciDevices()
	if err != nil {
		return "", fmt.Errorf("failed to get PCI devices: %v", err)
	}
	for _, dev := range devices {
		if dev.Vendor != nil && dev.Vendor.ID == SynaXGVendorID &&
			dev.Product != nil && dev.Product.ID == SynaXGHostPFDeviceID {
			vsp.log.Info("Found SynaXG PF device (fallback scan)", "pcieAddress", dev.Address)
			return dev.Address, nil
		}
	}
	return "", errors.New("SynaXG PF device not found")
}

// setupCommChannelVF creates the SR-IOV VF on the PF for gRPC communication.
func (vsp *synaXGVspServer) setupCommChannelVF(pfPcieAddr string) error {
	vsp.log.Info("Creating VFs for communication channel", "pfPcieAddr", pfPcieAddr, "numVfs", CommChannelVfCount)

	// Create VFs via sriov_numvfs sysfs
	err := vspnetutils.SetSriovNumVfs(vsp.fs, pfPcieAddr, CommChannelVfCount)
	if err != nil {
		return fmt.Errorf("failed to set sriov_numvfs on %s: %v", pfPcieAddr, err)
	}

	// Get PF netdev name
	pfIfName, err := vsp.platform.GetNetDevNameFromPCIeAddr(pfPcieAddr)
	if err != nil {
		return fmt.Errorf("failed to get PF netdev name for %s: %v", pfPcieAddr, err)
	}

	// Wait for VF0 PCI address to appear
	vfPciAddr, err := vspnetutils.WaitForVfPciAddressReady(vsp.fs, pfIfName, 0, VfSetupTimeout)
	if err != nil {
		return fmt.Errorf("timeout waiting for VF0 PCI address: %v", err)
	}
	vsp.log.Info("VF0 PCI address ready", "vfPciAddr", vfPciAddr)

	// Wait for VF0 netdev to be ready
	vfIfName, _, err := vspnetutils.WaitForLinkReady(vsp.platform, vfPciAddr, VfSetupTimeout)
	if err != nil {
		return fmt.Errorf("timeout waiting for VF0 link ready: %v", err)
	}

	vsp.commVfIfName = vfIfName
	vsp.log.Info("Communication channel VF ready", "vfIfName", vfIfName, "vfPciAddr", vfPciAddr)
	return nil
}

// configureCommChannelIP configures a static IPv4 address on the host-side VF
// and returns the card-side IP:Port that the host should connect to.
func (vsp *synaXGVspServer) configureCommChannelIP() (pb.IpPort, error) {
	ifName := vsp.commVfIfName

	// Tell NetworkManager to not manage our interface (may fail if NM is not running — that's fine).
	_ = exec.Command("nsenter", "-t", "1", "-m", "-u", "-n", "-i", "--",
		"nmcli", "device", "set", ifName, "managed", "no").Run()

	// Bring the interface up
	if err := exec.Command("ip", "link", "set", ifName, "up").Run(); err != nil {
		return pb.IpPort{}, fmt.Errorf("failed to bring up %s: %v", ifName, err)
	}

	// Configure host side: 192.168.1.2/24 on the VF
	vsp.log.Info("Configuring IPv4 on host VF", "ifName", ifName, "addr", IPv4AddrHost)
	if err := exec.Command("ip", "addr", "replace", IPv4AddrHost+"/"+IPv4Subnet, "dev", ifName).Run(); err != nil {
		return pb.IpPort{}, fmt.Errorf("failed to configure IPv4 %s/%s on %s: %v", IPv4AddrHost, IPv4Subnet, ifName, err)
	}

	vsp.log.Info("Communication channel configured",
		"hostAddr", IPv4AddrHost,
		"cardAddr", IPv4AddrCard,
		"port", DefaultPort,
	)

	// Return the CARD side address (192.168.1.1) — this is what HostSideManager will dial.
	// IPv4 does not need zone ID or special encoding, just plain IP.
	return pb.IpPort{
		Ip:   IPv4AddrCard,
		Port: DefaultPort,
	}, nil
}

// =============================================================================
// OAM Connection (proxy to card-side OAM)
// =============================================================================

func (vsp *synaXGVspServer) connectToOAM(ip string, port int32) error {
	addr := fmt.Sprintf("%s:%d", ip, port)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to create OAM client at %s: %v", addr, err)
	}
	vsp.oamConn = conn
	vsp.oamHeartbeatClient = pb.NewHeartbeatServiceClient(conn)
	vsp.oamBridgeClient = opi.NewBridgePortServiceClient(conn)
	vsp.oamSoftwareClient = sgpb.NewSoftwareManagementServiceClient(conn)
	vsp.oamSystemClient = sgpb.NewSystemManagementServiceClient(conn)
	vsp.log.Info("Connected to card-side OAM", "addr", addr)
	return nil
}

// closeOAMConnection tears down the OAM gRPC connection and nils all clients.
func (vsp *synaXGVspServer) closeOAMConnection() {
	if vsp.oamConn != nil {
		vsp.oamConn.Close()
		vsp.oamConn = nil
		vsp.oamHeartbeatClient = nil
		vsp.oamBridgeClient = nil
		vsp.oamSoftwareClient = nil
		vsp.oamSystemClient = nil
	}
}

// =============================================================================
// HeartbeatService - proxied to OAM
// =============================================================================

func (vsp *synaXGVspServer) Ping(ctx context.Context, req *pb.PingRequest) (*pb.PingResponse, error) {
	if vsp.oamHeartbeatClient == nil {
		return nil, fmt.Errorf("OAM heartbeat client not initialized")
	}
	return vsp.oamHeartbeatClient.Ping(ctx, req)
}

// =============================================================================
// BridgePortService - proxied to OAM
// =============================================================================

func (vsp *synaXGVspServer) CreateBridgePort(ctx context.Context, in *opi.CreateBridgePortRequest) (*opi.BridgePort, error) {
	vsp.log.Info("Proxying CreateBridgePort to OAM", "name", in.BridgePort.GetName())
	if vsp.oamBridgeClient == nil {
		return nil, fmt.Errorf("OAM bridge client not initialized")
	}
	return vsp.oamBridgeClient.CreateBridgePort(ctx, in)
}

func (vsp *synaXGVspServer) DeleteBridgePort(ctx context.Context, in *opi.DeleteBridgePortRequest) (*emptypb.Empty, error) {
	vsp.log.Info("Proxying DeleteBridgePort to OAM", "name", in.Name)
	if vsp.oamBridgeClient == nil {
		return nil, fmt.Errorf("OAM bridge client not initialized")
	}
	return vsp.oamBridgeClient.DeleteBridgePort(ctx, in)
}

// =============================================================================
// DeviceService
// =============================================================================

func (vsp *synaXGVspServer) GetDevices(ctx context.Context, in *pb.Empty) (*pb.DeviceListResponse, error) {
	vsp.log.V(2).Info("Received GetDevices() request")
	devices := make(map[string]*pb.Device)
	for _, pciAddr := range vsp.dataVfPciAddrs {
		devices[pciAddr] = &pb.Device{
			ID:     pciAddr,
			Health: "Healthy",
		}
	}
	vsp.log.V(2).Info("GetDevices() returning devices", "count", len(devices))
	return &pb.DeviceListResponse{Devices: devices}, nil
}

func (vsp *synaXGVspServer) SetNumVfs(ctx context.Context, in *pb.VfCount) (*pb.VfCount, error) {
	vsp.log.Info("Received SetNumVfs() request", "count", in.VfCnt)
	if vsp.dpuPcieAddress == "" {
		return &pb.VfCount{VfCnt: 0}, fmt.Errorf("PF PCIe address not set — was Init() called?")
	}

	// VF0 is reserved for the gRPC comm channel (created in Init).
	// Data-path VFs start at VF1, so total sriov_numvfs = comm (1) + requested data VFs.
	totalVfs := CommChannelVfCount + int(in.VfCnt)
	vsp.log.Info("Setting sriov_numvfs", "total", totalVfs, "commVfs", CommChannelVfCount, "dataVfs", in.VfCnt)

	if err := vspnetutils.SetSriovNumVfs(vsp.fs, vsp.dpuPcieAddress, totalVfs); err != nil {
		return &pb.VfCount{VfCnt: 0}, fmt.Errorf("failed to set sriov_numvfs to %d on %s: %v", totalVfs, vsp.dpuPcieAddress, err)
	}

	// Collect PCI addresses for data VFs (VF1 .. VF(totalVfs-1)).
	pfIfName, err := vsp.platform.GetNetDevNameFromPCIeAddr(vsp.dpuPcieAddress)
	if err != nil {
		return &pb.VfCount{VfCnt: 0}, fmt.Errorf("failed to get PF netdev name: %v", err)
	}

	var dataAddrs []string
	for vfId := CommChannelVfCount; vfId < totalVfs; vfId++ {
		addr, err := vspnetutils.WaitForVfPciAddressReady(vsp.fs, pfIfName, vfId, VfSetupTimeout)
		if err != nil {
			return &pb.VfCount{VfCnt: 0}, fmt.Errorf("timeout waiting for VF%d PCI address: %v", vfId, err)
		}
		dataAddrs = append(dataAddrs, addr)
		vsp.log.Info("Data VF ready", "vfId", vfId, "pciAddr", addr)
	}

	vsp.dataVfPciAddrs = dataAddrs
	vsp.log.Info("SetNumVfs() complete", "dataVfs", len(dataAddrs))
	return in, nil
}

// =============================================================================
// NetworkFunctionService
// =============================================================================

func (vsp *synaXGVspServer) CreateNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {
	vsp.log.Info("Received CreateNetworkFunction() request", "Input", in.Input, "Output", in.Output)
	// TODO: Implement
	return &pb.Empty{}, nil
}

func (vsp *synaXGVspServer) DeleteNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {
	vsp.log.Info("Received DeleteNetworkFunction() request", "Input", in.Input, "Output", in.Output)
	// TODO: Implement
	return &pb.Empty{}, nil
}

// =============================================================================
// DataProcessingUnitManagementService
// =============================================================================

func (vsp *synaXGVspServer) DpuRebootFunction(ctx context.Context, in *pb.DPURebootRequest) (*pb.DPUManagementResponse, error) {
	vsp.log.Info("Received DpuRebootFunction() request", "force", in.GetForce(), "pciAddress", vsp.dpuPcieAddress)

	pfPcieAddr := vsp.dpuPcieAddress
	if pfPcieAddr == "" {
		return &pb.DPUManagementResponse{
			Success: false, Status: "Error",
			Message: "PF PCIe address not set — was Init() called?",
		}, fmt.Errorf("PF PCIe address not set")
	}

	// The reboot is done in a background goroutine because:
	//  1. unbind destroys the VF (and therefore the gRPC comm channel to OAM)
	//  2. bind re-creates the PF but VFs + IP need to be re-provisioned
	//  3. We must return the gRPC response to the caller BEFORE unbinding,
	//     otherwise the caller hangs on a dead connection.
	//
	// The goroutine performs:
	//   unbind → sleep (card reboots) → bind → re-create VF → re-assign IP → reconnect OAM
	//
	// After this goroutine completes, Ping() will work again, which is how
	// the reconciler detects that the reboot finished.
	go vsp.performRebootCycle(pfPcieAddr)

	return &pb.DPUManagementResponse{
		Success: true,
		Status:  "Rebooting",
		Message: fmt.Sprintf("Reboot initiated for DPU at %s (async — Ping will fail until DPU is back)", pfPcieAddr),
	}, nil
}

// performRebootCycle runs the full unbind → wait → bind → VF/IP restore → OAM reconnect
// sequence. It runs in a background goroutine so the gRPC response can be sent first.
func (vsp *synaXGVspServer) performRebootCycle(pfPcieAddr string) {
	vsp.log.Info("Starting reboot cycle", "pciAddress", pfPcieAddr)
	// --- Step 1: Close the existing OAM connection (it is about to break) ---
	vsp.closeOAMConnection()

	// --- Step 2: Unbind the PF driver ---
	// This destroys all VFs under this PF (sriov_numvfs → 0, netdevs removed).
	unbindPath := fmt.Sprintf("/sys/bus/pci/devices/%s/driver/unbind", pfPcieAddr)
	vsp.log.Info("Unbinding PF driver", "path", unbindPath)
	if err := exec.Command("sh", "-c", fmt.Sprintf("echo '%s' > %s", pfPcieAddr, unbindPath)).Run(); err != nil {
		vsp.log.Error(err, "Failed to unbind PF driver", "pciAddress", pfPcieAddr)
		return
	}
	vsp.log.Info("PF unbound — DPU is rebooting, VFs destroyed")

	// --- Step 3: Wait for the card to reboot ---
	// The card-side OAM will start after the card finishes booting and
	// configure 192.168.1.3 on its side of the VF.
	rebootWait := 120 * time.Second
	vsp.log.Info("Waiting for DPU to reboot", "duration", rebootWait)
	time.Sleep(rebootWait)

	// --- Step 4: Bind the PF driver ---
	// PF netdev reappears but sriov_numvfs is still 0 — no VFs yet.
	bindPath := fmt.Sprintf("/sys/bus/pci/drivers/%s/bind", vsp.pfDriverName())
	vsp.log.Info("Binding PF driver", "path", bindPath)
	if err := exec.Command("sh", "-c", fmt.Sprintf("echo '%s' > %s", pfPcieAddr, bindPath)).Run(); err != nil {
		vsp.log.Error(err, "Failed to bind PF driver", "pciAddress", pfPcieAddr)
		return
	}
	vsp.log.Info("PF bound successfully")

	// Give the driver a moment to settle
	time.Sleep(5 * time.Second)

	// --- Step 5: Re-create VFs and configure IP ---
	// Identical to what Init() does in steps 2 & 3.
	if err := vsp.setupCommChannelVF(pfPcieAddr); err != nil {
		vsp.log.Error(err, "Failed to re-create communication channel VF after reboot")
		return
	}

	if _, err := vsp.configureCommChannelIP(); err != nil {
		vsp.log.Error(err, "Failed to re-configure IP on communication channel VF after reboot")
		return
	}

	// --- Step 6: Reconnect to OAM ---
	// The card-side OAM should be up by now with 192.168.1.3.
	// Retry a few times in case OAM is still starting.
	var lastErr error
	for attempt := 1; attempt <= 10; attempt++ {
		if err := vsp.connectToOAM(IPv4AddrCard, DefaultPort); err != nil {
			lastErr = err
			vsp.log.V(1).Info("OAM not ready yet, retrying",
				"attempt", attempt, "error", err)
			time.Sleep(5 * time.Second)
			continue
		}
		vsp.log.Info("Reboot cycle completed — OAM reconnected, Ping should now succeed")
		return
	}
	vsp.log.Error(lastErr, "Reboot cycle completed but failed to reconnect to OAM after all retries")
}

// pfDriverName returns the kernel driver name for the SynaXG PF.
// This is used to construct the bind path after unbind.
// TODO: Read this dynamically from /sys/bus/pci/devices/<addr>/driver if needed.
func (vsp *synaXGVspServer) pfDriverName() string {
	return "octeon_ep" // Same driver used in the original synaxg.go Reboot()
}

func (vsp *synaXGVspServer) DpuFirmwareUpgradeFunction(ctx context.Context, in *pb.DPUFirmwareUpgradeRequest) (*pb.DPUManagementResponse, error) {
	vsp.log.Info("Received DpuFirmwareUpgradeFunction() request",
		"firmwareType", in.GetFirmwareType(),
		"firmwareImagePath", in.GetFirmwareImagePath(),
		"pciAddress", vsp.dpuPcieAddress)

	if in.GetFirmwareType() == "" {
		return &pb.DPUManagementResponse{
			Success: false,
			Status:  "InvalidRequest",
			Message: "firmware_type is required",
		}, fmt.Errorf("firmware_type is required")
	}

	// Use the caller-supplied firmware path if provided; otherwise fall back to the
	// hard-coded default so that the operator retains full control of the firmware
	// version shipped with this release (contributor requirement).
	imagePath := in.GetFirmwareImagePath()
	if imagePath == "" {
		imagePath = DefaultFirmwareImagePath
		vsp.log.Info("No firmware path specified, using release default", "path", imagePath)
	}

	if err := validateImageRef(imagePath); err != nil {
		vsp.log.Error(err, "Firmware image rejected by allowlist", "image", imagePath)
		return &pb.DPUManagementResponse{
			Success: false,
			Status:  "Forbidden",
			Message: err.Error(),
		}, err
	}

	if vsp.oamSoftwareClient == nil || vsp.oamSystemClient == nil {
		return &pb.DPUManagementResponse{
			Success: false,
			Status:  "Error",
			Message: "OAM not connected — was Init() called?",
		}, fmt.Errorf("OAM software/system client not initialized")
	}

	// Step 1: Pull firmware file from the container image registry.
	vsp.log.Info("Pulling firmware from image", "image", imagePath)
	localPath, err := pullFirmwareFromImage(imagePath, ".tar.gz")
	if err != nil {
		return &pb.DPUManagementResponse{
			Success: false,
			Status:  "PullFailed",
			Message: fmt.Sprintf("failed to pull firmware image: %v", err),
		}, fmt.Errorf("failed to pull firmware image: %v", err)
	}
	vsp.log.Info("Firmware file pulled", "localPath", localPath)

	// Step 2: Check if the firmware version differs from what is currently on the card.
	needsUpgrade, err := vsp.checkFirmwareVersion(ctx, localPath)
	if err != nil {
		vsp.log.Error(err, "Firmware version check failed, proceeding with upgrade anyway")
		// Non-fatal: we still attempt the upgrade if we can't determine the version.
	} else if !needsUpgrade {
		vsp.log.Info("Firmware version unchanged, no upgrade needed")
		return &pb.DPUManagementResponse{
			Success: true,
			Status:  "AlreadyUpToDate",
			Message: "firmware version on card matches the target version, no upgrade needed",
		}, nil
	}

	// Step 3: Stream the firmware file to OAM's SoftwareUpgradeStream RPC.
	vsp.log.Info("Streaming firmware to OAM", "localPath", localPath)
	upgradeCtx, cancel := context.WithTimeout(ctx, FirmwareUpgradeTimeout)
	defer cancel()

	upgradeResp, err := vsp.streamFirmwareToOAM(upgradeCtx, localPath)
	if err != nil {
		return &pb.DPUManagementResponse{
			Success: false,
			Status:  "UpgradeFailed",
			Message: fmt.Sprintf("firmware stream to OAM failed: %v", err),
		}, fmt.Errorf("firmware stream to OAM failed: %v", err)
	}

	// Step 4: Evaluate the OAM response.
	if upgradeResp.Result == sgpb.UpgradeResultStatus_UPG_NOT_RUNNING {
		vsp.log.Info("OAM reports upgrade not needed (UPG_NOT_RUNNING)")
		return &pb.DPUManagementResponse{
			Success: true,
			Status:  "AlreadyUpToDate",
			Message: "OAM reports firmware upgrade not needed",
		}, nil
	} else if upgradeResp.Result == sgpb.UpgradeResultStatus_UPG_SUCCESS {
		vsp.log.Info("Firmware upgrade succeeded — card will need a reboot to activate")
		return &pb.DPUManagementResponse{
			Success: true,
			Status:  "UpgradeComplete",
			Message: fmt.Sprintf("Firmware flashed successfully on DPU at %s (reboot required to activate)", vsp.dpuPcieAddress),
		}, nil
	}

	// Any other result is a failure.
	errMsg := upgradeResp.GetErrorMessage()
	vsp.log.Error(nil, "Firmware upgrade failed on OAM side",
		"result", upgradeResp.Result, "errorMessage", errMsg)
	return &pb.DPUManagementResponse{
		Success: false,
		Status:  "UpgradeFailed",
		Message: fmt.Sprintf("OAM firmware upgrade failed: result=%v, error=%s", upgradeResp.Result, errMsg),
	}, fmt.Errorf("OAM firmware upgrade failed: result=%v, error=%s", upgradeResp.Result, errMsg)
}

// =============================================================================
// Firmware Upgrade Helpers
// =============================================================================

// validateImageRef checks that imageRef belongs to one of the allowed
// registry/repository prefixes defined in allowedImagePrefixes.
// This prevents SSRF by rejecting arbitrary user-supplied image URLs.
// We do a simple prefix check rather than full OCI canonicalization,
// since all allowed images are on quay.io and fully qualified.
func validateImageRef(imageRef string) error {
	for _, prefix := range allowedImagePrefixes {
		if strings.HasPrefix(imageRef, prefix) {
			return nil
		}
	}
	return fmt.Errorf("image %q is not from an allowed repository (allowed prefixes: %v)", imageRef, allowedImagePrefixes)
}

// ociManifest holds the minimal OCI/Docker v2 image manifest fields we need.
type ociManifest struct {
	Layers []struct {
		Digest string `json:"digest"`
	} `json:"layers"`
}

// ociRegistryToken is the response from the registry auth endpoint.
type ociRegistryToken struct {
	Token string `json:"token"`
}

// pullFirmwareFromImage pulls a container image from a registry using the OCI
// Distribution API (stdlib only – no external OCI library required) and
// extracts the first file whose name ends with fileSuffix (e.g. ".tar.gz").
// Returns the local filesystem path of the extracted file.
func pullFirmwareFromImage(imageRef string, fileSuffix string) (string, error) {
	registry, repo, tag := parseImageRefParts(imageRef)
	client := &http.Client{Timeout: 60 * time.Second}

	// 1. Obtain bearer token for the repository.
	token, err := fetchRegistryToken(client, registry, repo)
	if err != nil {
		return "", fmt.Errorf("fetching registry token for %s/%s: %w", registry, repo, err)
	}

	// 2. Fetch the image manifest to get layer digests.
	manifest, err := fetchManifest(client, registry, repo, tag, token)
	if err != nil {
		return "", fmt.Errorf("fetching manifest for %s/%s:%s: %w", registry, repo, tag, err)
	}

	// 3. Search each layer for the target file.
	for _, layer := range manifest.Layers {
		path, err := extractFileFromBlob(client, registry, repo, layer.Digest, fileSuffix, token)
		if err != nil {
			continue // layer may not contain the file; try next
		}
		if path != "" {
			return path, nil
		}
	}
	return "", fmt.Errorf("no file with suffix %q found in image %q", fileSuffix, imageRef)
}

// parseImageRefParts splits an image reference into (registry, repository, tag).
// Examples:
//
//	"quay.io/org/repo:v1"       → ("quay.io", "org/repo", "v1")
//	"quay.io/org/repo"          → ("quay.io", "org/repo", "latest")
func parseImageRefParts(imageRef string) (registry, repo, tag string) {
	// Split off tag (last colon after the last slash).
	if idx := strings.LastIndex(imageRef, ":"); idx > strings.LastIndex(imageRef, "/") {
		tag = imageRef[idx+1:]
		imageRef = imageRef[:idx]
	} else {
		tag = "latest"
	}
	// Split registry from repository.
	if slash := strings.Index(imageRef, "/"); slash != -1 {
		registry = imageRef[:slash]
		repo = imageRef[slash+1:]
	} else {
		registry = "registry-1.docker.io"
		repo = "library/" + imageRef
	}
	return
}

// fetchRegistryToken performs the Bearer token challenge against the registry
// and returns an access token for the given repository.
func fetchRegistryToken(client *http.Client, registry, repo string) (string, error) {
	// Quay.io (and most OCI registries) accept anonymous pulls via this endpoint.
	tokenURL := fmt.Sprintf("https://%s/v2/auth?service=%s&scope=repository:%s:pull", registry, registry, repo)
	resp, err := client.Get(tokenURL)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", tokenURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var tok ociRegistryToken
	if err := json.Unmarshal(body, &tok); err != nil || tok.Token == "" {
		// Some registries skip auth for public images — treat empty token as OK.
		return "", nil
	}
	return tok.Token, nil
}

// fetchManifest retrieves the OCI image manifest for the given image.
func fetchManifest(client *http.Client, registry, repo, tag, token string) (*ociManifest, error) {
	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, repo, tag)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json,application/vnd.oci.image.manifest.v1+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	var m ociManifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("decoding manifest: %w", err)
	}
	return &m, nil
}

// extractFileFromBlob downloads a blob (layer) and extracts the first file
// whose name ends with fileSuffix from its tar stream.
// Layers are gzip-compressed tarballs; blobs with other media types are skipped.
func extractFileFromBlob(client *http.Client, registry, repo, digest, fileSuffix, token string) (string, error) {
	url := fmt.Sprintf("https://%s/v2/%s/blobs/%s", registry, repo, digest)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d from blob %s", resp.StatusCode, digest)
	}

	// Layers are gzip-compressed tarballs.
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("gzip reader for blob %s: %w", digest, err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("reading tar from blob %s: %w", digest, err)
		}
		if !strings.HasSuffix(hdr.Name, fileSuffix) {
			continue
		}
		outputPath := filepath.Join(os.TempDir(), filepath.Base(hdr.Name))
		outFile, err := os.Create(outputPath)
		if err != nil {
			return "", fmt.Errorf("creating output file %s: %w", outputPath, err)
		}
		written, err := io.Copy(outFile, tr)
		outFile.Close()
		if err != nil {
			return outputPath, fmt.Errorf("writing output file %s: %w", outputPath, err)
		}
		fmt.Printf("Firmware file extracted: %s (%d bytes)\n", outputPath, written)
		return outputPath, nil
	}
	return "", nil // file not in this layer
}

// streamFirmwareToOAM opens a client-streaming RPC to OAM's
// SoftwareManagementService.SoftwareUpgradeStream and sends the firmware
// file in 1 MiB chunks.
func (vsp *synaXGVspServer) streamFirmwareToOAM(ctx context.Context, localPath string) (*sgpb.SoftwareUpgradeResponse, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("opening firmware file %s: %w", localPath, err)
	}
	defer f.Close()

	stream, err := vsp.oamSoftwareClient.SoftwareUpgradeStream(ctx)
	if err != nil {
		return nil, fmt.Errorf("opening SoftwareUpgradeStream: %w", err)
	}

	buf := make([]byte, FirmwareChunkSize)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			req := &sgpb.SoftwareUpgradeStreamRequest{
				RemoteFile: localPath,
				ChunkData:  buf[:n],
			}
			if sendErr := stream.Send(req); sendErr != nil {
				// If the server closed the stream early, break out and
				// let CloseAndRecv report the real error.
				if sendErr == io.EOF {
					break
				}
				return nil, fmt.Errorf("sending firmware chunk: %w", sendErr)
			}
		}
		if err == io.EOF {
			vsp.log.Info("Firmware file upload complete", "path", localPath)
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading firmware file: %w", err)
		}
	}

	resp, err := stream.CloseAndRecv()
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("closing firmware stream: %w", err)
	}

	vsp.log.Info("OAM SoftwareUpgradeStream response received",
		"result", resp.GetResult(), "errorMessage", resp.GetErrorMessage())
	return resp, nil
}

// checkFirmwareVersion queries the card's current firmware version via OAM's
// SystemManagementService and compares it with the version embedded in the
// firmware filename (convention: <name>-<version>-<rest>.tar.gz).
// Returns true if an upgrade is needed (versions differ), false otherwise.
func (vsp *synaXGVspServer) checkFirmwareVersion(ctx context.Context, firmwarePath string) (bool, error) {
	if vsp.oamSystemClient == nil {
		return true, fmt.Errorf("OAM system client not initialised")
	}

	queryCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	resp, err := vsp.oamSystemClient.GetSystemBasicInfo(queryCtx, &sgpb.GetSystemBasicInfoRequest{})
	if err != nil {
		return true, fmt.Errorf("GetSystemBasicInfo RPC failed: %w", err)
	}

	currentVersion := resp.GetSystemInfo().GetFirmwareVersion()
	vsp.log.Info("Card firmware version", "current", currentVersion)

	// Extract target version from the firmware filename.
	// Convention: the filename contains dash-separated tokens and the second
	// token is the version string. E.g. "synaxg-1.2.3-rc1.tar.gz" → "1.2.3".
	baseName := filepath.Base(firmwarePath)
	parts := strings.Split(baseName, "-")
	if len(parts) < 2 {
		return true, fmt.Errorf("cannot parse version from firmware filename %q", baseName)
	}
	targetVersion := parts[1]
	vsp.log.Info("Target firmware version", "target", targetVersion)

	if currentVersion == targetVersion {
		return false, nil
	}
	return true, nil
}

// =============================================================================
// gRPC Server Lifecycle (Unix Socket - Daemon connects here)
// =============================================================================

func (vsp *synaXGVspServer) Listen() (net.Listener, error) {
	err := vsp.pathManager.EnsureSocketDirExists(vsp.pathManager.VendorPluginSocket())
	if err != nil {
		return nil, fmt.Errorf("failed to create socket directory: %v", err)
	}

	listener, err := net.Listen("unix", vsp.pathManager.VendorPluginSocket())
	if err != nil {
		return nil, fmt.Errorf("failed to listen on vendor plugin socket: %v", err)
	}
	vsp.grpcServer = grpc.NewServer()
	pb.RegisterNetworkFunctionServiceServer(vsp.grpcServer, vsp)
	pb.RegisterLifeCycleServiceServer(vsp.grpcServer, vsp)
	pb.RegisterDeviceServiceServer(vsp.grpcServer, vsp)
	opi.RegisterBridgePortServiceServer(vsp.grpcServer, vsp)
	pb.RegisterHeartbeatServiceServer(vsp.grpcServer, vsp)
	pb.RegisterDataProcessingUnitManagementServiceServer(vsp.grpcServer, vsp)

	vsp.log.Info("gRPC server listening", "socketPath", vsp.pathManager.VendorPluginSocket())
	return listener, nil
}

func (vsp *synaXGVspServer) Serve(listener net.Listener) error {
	vsp.wg.Add(1)
	go func() {
		vsp.version = Version
		vsp.log.Info("Starting SynaXG VSP Server", "version", vsp.version)
		if err := vsp.grpcServer.Serve(listener); err != nil {
			vsp.done <- err
		} else {
			vsp.done <- nil
		}
		vsp.log.Info("SynaXG VSP Server stopped")
		vsp.wg.Done()
	}()

	err := <-vsp.done
	vsp.grpcServer.Stop()
	vsp.wg.Wait()
	vsp.startedWg.Done()
	return err
}

func (vsp *synaXGVspServer) Stop() {
	vsp.closeOAMConnection()
	vsp.grpcServer.Stop()
	vsp.done <- nil
	vsp.startedWg.Wait()
}

// =============================================================================
// Constructor and main
// =============================================================================

func WithPathManager(pathManager utils.PathManager) func(*synaXGVspServer) {
	return func(vsp *synaXGVspServer) {
		vsp.pathManager = pathManager
	}
}

func NewSynaXGVspServer(opts ...func(*synaXGVspServer)) *synaXGVspServer {
	options := zap.Options{
		Development: true,
		Level:       zapcore.InfoLevel,
	}
	options.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&options)))

	vsp := &synaXGVspServer{
		log:         ctrl.Log.WithName("SynaXGVsp"),
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
	vsp := NewSynaXGVspServer()
	listener, err := vsp.Listen()
	if err != nil {
		vsp.log.Error(err, "Failed to listen")
		return
	}

	err = vsp.Serve(listener)
	if err != nil {
		vsp.log.Error(err, "Failed to serve")
	}
}
