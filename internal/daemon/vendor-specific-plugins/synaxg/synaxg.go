package main

import (
	"context"
	"flag"
	"fmt"
	"google.golang.org/grpc/credentials/insecure"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
	"archive/tar"
	"strings"

	"github.com/go-logr/logr"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	mrvlutils "github.com/openshift/dpu-operator/internal/daemon/vendor-specific-plugins/marvell/mrvl-utils"
	dhcp "github.com/openshift/dpu-operator/internal/daemon/vendor-specific-plugins/synaxg/dhcp"
	sgpb "github.com/openshift/dpu-operator/internal/daemon/vendor-specific-plugins/synaxg/protos/gen"
	"github.com/openshift/dpu-operator/internal/utils"
	opi "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"github.com/vishvananda/netlink"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
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
	DataPlaneType  string = "debug"
	NumPFs         int    = 1
	PFID           int    = 0
	isDPDK         bool   = false
	HostVFDeviceID string = "b903"
	filePathUnbind string = "/sys/bus/pci/drivers/octeon_ep/unbind"
	filePathBind   string = "/sys/bus/pci/drivers/octeon_ep/bind"
)

const (
	vduPort     int           = 50051
	oprPort     int           = 50601
	loadTime    time.Duration = 15
	upgradeTime time.Duration = 1200
	chunkSize   int           = 1024 * 1024
)

const (
	FAIL_LOAD_STATUS int = 0xFF
	FAIL_STATUS      int = 1
	SUCC_STATUS      int = 0
	NOT_NEED_STATUS  int = 2
)

// multiple dataplane can be added using mrvldp interface functions
type sgDeviceInfo struct {
	secInterfaceName string
	dpInterfaceName  string
	dpMAC            string
	portType         string
	health           string
	pciAddress       string
}
type sgVspServer struct {
	pb.UnimplementedLifeCycleServiceServer
	pb.UnimplementedManualOperationServiceServer
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
	deviceStore   map[string]sgDeviceInfo
	portType      string
	bridgeName    string
}
type CardInfo struct {
	SerialNum string
	CardIp    string
}

type HelloServiceImpl struct {
	sgpb.UnimplementedHelloServiceServer
}

type CardRequest struct {
	PciAddr    string
	StatusCode int
}

type PcieDriverInfo struct {
	pfName   string
	deviceID string
	deviceIp string
}

var (
	cardInfo     CardInfo
	cardReqList  CardRequest
	driverInfo   PcieDriverInfo
	hbInterval   int32 = 60
	l1HbInterval int32 = 10
)
// createVethPair function to create a veth pair with the given index and InterfaceInfo

// Init function to initialize the Marvell VSP Server with the given context and InitRequest
// It will return the IpPort and error
func (vsp *sgVspServer) Init(ctx context.Context, in *pb.InitRequest) (*pb.IpPort, error) {
	klog.Infof("Received Init() request: DpuMode: %v", in.DpuMode)
	vsp.isDPUMode = in.DpuMode
	ipPort, err := vsp.configureIP(in.DpuMode)
	if vsp.deviceStore == nil {
		vsp.deviceStore = make(map[string]sgDeviceInfo)
	}

	vsp.portType = "sriov"
	VfsPCI, err := mrvlutils.GetAllVfsByDeviceID(HostVFDeviceID)
	if err != nil {
		return nil, err
	}
	for _, vfpci := range VfsPCI {
		health := vsp.GetDeviceHealth(vfpci)
		vsp.deviceStore[vfpci] = sgDeviceInfo{
			pciAddress: vfpci,
			health:     health,
			portType:   "sriov",
		}
	}
	return &pb.IpPort{
		Ip:   ipPort.Ip,
		Port: ipPort.Port,
	}, err
}

func (vsp *sgVspServer) configureIP(dpuMode bool) (pb.IpPort, error) {
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

	// Ensure to set addrgenmode and toggle link state (which can result in creating
	// the IPv6 link local address. Ignore errors here.
	exec.Command("ip", "link", "set", interfaceName, "addrgenmode", "eui64").Run()
	exec.Command("ip", "link", "set", interfaceName, "down").Run()

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

// GetDeviceHealth function to get the health of the device based on the given secInterfaceName
func (vsp *sgVspServer) GetDeviceHealth(secInterfaceName string) string {
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

func execCommand(command string) ([]byte, error) {
	var (
		err error
		out []byte
	)
	cmd := exec.Command("sh", "-c", command)
	out, err = cmd.CombinedOutput()
	if err != nil {
		log.Printf("Error: %v\n", err)
		fmt.Printf("Output: %s\n", out)
		return nil, err
	}
	return out, nil
}

func Reboot(deviceID string) error {
	var err error

	commd := fmt.Sprintf(`echo "%s" > %s`, deviceID, filePathUnbind)
	_, err = execCommand(commd)
	if err != nil {
		return err
	}

	log.Println("command unbind executed successfully.")

	log.Println("sleep 120 seconds...")
	time.Sleep(120 * time.Second)

	commd = fmt.Sprintf(`echo "%s" > %s`, deviceID, filePathBind)
	_, err = execCommand(commd)
	if err != nil {
		return err
	}
	log.Println("command bind executed successfully.")

	return nil
}

func (vsp *sgVspServer) ManualRebootDpuFunction(ctx context.Context, in *pb.ManualOperationRequest) (*pb.ManualOperationResponse, error){
		err := Reboot(in.PciAddress)
		if err != nil{
			return &pb.ManualOperationResponse{Status: "fail", Message: err.Error()}, err
		}

		return &pb.ManualOperationResponse{Status: "Success", Message: "reboot success"}, nil

}

// Pulls a file from a Docker image in a registry
func pullFileFromImage(image, filePath string) (outputPath string, error) {
	// Parse the image reference
	ref, err := name.ParseReference(image)
	if err != nil {
		return nil, fmt.Errorf("parsing reference: %w", err)
	}

	// Get the image from the registry
	img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return nil, fmt.Errorf("getting image: %w", err)
	}

	// Get the image layers
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("getting layers: %w", err)
	}

	// Iterate through layers to find the file
	for _, layer := range layers {
		r, err := layer.Uncompressed()
		if err != nil {
			return nil, fmt.Errorf("reading layer: %w", err)
		}
		defer r.Close()

		tr := tar.NewReader(r)

		// Search for the file that we want
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("reading tar: %w", err)
			}
			fmt.Println("Extracting file from tar:", hdr.Name)
            
			if strings.HasSuffix(hdr.Name, filePath) {
				outputPath := hdr.Name
				outFile, err := os.Create(outputPath)
				if err != nil {
					return outputPath, fmt.Errorf("creating output file: %w", err)
				}
				// Ensure the output file is closed properly
				defer outFile.Close()

				written, err := io.Copy(outFile, tr)
				if err != nil {
					return outputPath, fmt.Errorf("writing output file: %w", err)
				}

				fmt.Println("File successfully extracted:", outputPath)
				//fmt.Println("File contents:", fileContent.String())
				fmt.Println("File bytes written:", written)
				return outputPath, nil
			}
		}
	}

	return outputPath, fmt.Errorf("file not found in image: %s", filePath)
}

func GetSdkFileFromRemote(sdkImagePath string) (string, error) {
	fileSuffix := ".tar.gz"

	outputPath, err := pullFileFromImage(sdkImagePath, fileSuffix)
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("File saved to:", outputPath)
	}

	return outputPath, nil
}

func sendHeartbeatLoop(addr *string) {
	log.Printf("send loop Heartbeat to card oam entry...")
	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("did not connect: %v", err)
		return
	}
	defer conn.Close()
	client := sgpb.NewHeartbeatServiceClient(conn)

	for {
		// Contact the server and print out its response.
		ctx, cancel := context.WithCancel(context.Background())
		// ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		r, err := client.Heartbeat(ctx, &sgpb.HeartbeatRequest{NotifyInterval: 60})
		if err != nil {
			log.Printf("send heartbeat error: %v", err)
			return
		}
		log.Printf("heartbeat result: %d", r.GetResult())
		time.Sleep(60 * time.Second)
	}
}
func SoftwareUpgrade(ctx context.Context, client sgpb.SoftwareManagementServiceClient, filePath string) (*sgpb.SoftwareUpgradeResponse, error) {
	log.Println("open file.")
	f, err := os.Open(filePath)
	if err != nil {
		log.Printf("open failed: %v", err)
		return nil, err
	}
	defer f.Close()

	stream, err := client.SoftwareUpgradeStream(ctx)
	if err != nil {
		return nil, err
	}
	for {
		buf := make([]byte, chunkSize)
		n, err := f.Read(buf)
		if err != nil && err != io.EOF {
			log.Printf("read failed: %v", err)
			return nil, err
		}
		if err == io.EOF {
			log.Printf("file upload done")
			break
		}

		req := &sgpb.SoftwareUpgradeStreamRequest{
			RemoteFile: filePath,
			ChunkData:  buf[:n],
		}

		if err := stream.Send(req); err != nil {
			log.Printf("send failed: %v", err)
			if err == io.EOF {
				break
			}
			return nil, err
		}
	}

	log.Printf("software upgrade begin...")
	res, err := stream.CloseAndRecv()
	if err != nil && err != io.EOF {
		return nil, err
	}
	log.Println("software upgrade done.")
	fmt.Println(res.ErrorMessage)
	return res, nil
}

func WorkerUpgrade(addr string, filePath string, statusCode *int, wg *sync.WaitGroup) {
	defer wg.Done()
	// Set up a connection to the bbu server.
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))

	if err != nil {
		log.Printf("did not connect: %v", err)
		return
	}
	defer conn.Close()
	client := sgpb.NewSoftwareManagementServiceClient(conn)

	// Contact the server and print out its response.
	ctxUpload, cancel_upload := context.WithTimeout(context.Background(), upgradeTime*time.Second)
	defer cancel_upload()

	res, err := SoftwareUpgrade(ctxUpload, client, filePath)
	if err != nil {
		log.Printf("could not upload file: %v", err)
		return
	}
	if res.Result == sgpb.UpgradeResultStatus_UPG_NOT_RUNNING {
		*statusCode = NOT_NEED_STATUS
	} else if res.Result == sgpb.UpgradeResultStatus_UPG_SUCCESS {
		*statusCode = SUCC_STATUS
	} else {
		*statusCode = FAIL_STATUS
	}

	log.Printf("response %d %s", res.Result, res.ErrorMessage)
}

func (s *HelloServiceImpl) SayHello(ctx context.Context, in *sgpb.HelloRequest) (*sgpb.HelloResponse, error) {
	log.Printf("received request Serial Number %s", in.GetSerialNumber())
	log.Printf("received request Card IP %s", in.GetCardIp())

	cardInfo = CardInfo{in.GetSerialNumber(), in.GetCardIp()}

	return &sgpb.HelloResponse{Result: sgpb.ResultStatus_SUCCESS, ErrorMessage: "", HbInterval: &hbInterval, L1HbInterval: &l1HbInterval}, nil
}

func WorkerHello(ctx context.Context) {

	var (
		err error
		lis net.Listener
		s   *grpc.Server
	)

	// Set up a connection to the server.
	lis, err = net.Listen("tcp", fmt.Sprintf(":%d", oprPort))
	if err != nil {
		log.Printf("failed to listen: %v", err)
	}
	s = grpc.NewServer()
	func(ctx context.Context, s *grpc.Server) {
		<-ctx.Done()
		s.GracefulStop()
	}(ctx, s)
	sgpb.RegisterHelloServiceServer(s, &HelloServiceImpl{})

	if err := s.Serve(lis); err != nil {
		log.Printf("failed to serve: %v", err)
	}
}

func CheckSdkVersion(sdkVersionNew, addr string)(upgradeFlag bool, error) {
	var (
		targetVersion string 
	    currentVersion string
	)
	// Set up a connection to the bbu server.
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))

	if err != nil {
		log.Printf("did not connect: %v", err)
		return false, err
	}
	defer conn.Close()
	client := sgpb.NewSystemManagementServiceClient(conn)

	// Contact the server and print out its response.
	ctx, cancel_upload := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel_upload()

	res, err := client.GetSystemBasicInfo(ctx, &pb.GetSystemBasicInfoRequest{})
	if err != nil {
		log.Printf("could not sync file: %v", err)
		return false, err
	}
	currentVersion = res.SystemInfo.FirmwareVersion
	log.Printf("CARD: FirmwareVersion: %s", currentVersion)

	parts := strings.Split(filename, "-")
	if len(parts) > 1 {
		// get the second part
		targetVersion = parts[1]
		log.Printf("TARGET: FirmwareVersion: %s", targetVersion)
	}
	else{
		log.Printf("SDK package name error")
		return false, errors.New("SDK package name error")
	}
     
	if currentVersion == targetVersion {
		log.Printf("SDK version not changed, no need to upgrade")
		return false, errors.New("SDK version not changed, no need to upgrade")
	}
    
	reture true, nil
}

func (vsp *sgVspServer) ManualUpgradeSdkFunction(ctx context.Context, in *pb.ManualOperationRequest) (*pb.ManualOperationResponse, error){
	var cardReqElem CardRequest
	var wg_update sync.WaitGroup

	isOk, err := dhcp.StartDhcpServer()
	if err != nil || isOk !=0{
		log.Printf("Utility update-oam's  dhcp server init failed")
		return &pb.ManualOperationResponse{Status: "fail", Message: err.Error()}, err
	}

	//Step1: listen the Hello Message from SG3 OAM
	ctx1, cancelHello := context.WithTimeout(context.Background(), loadTime*time.Second)
	defer cancelHello()
	wg_update.Add(1)
	log.Printf("In loading serial number and card ip ...")
	go WorkerHello(ctx1)
	wg_update.Wait()

	//send heartbeat to card oam
	cardAddr := fmt.Sprintf("%s:%d", cardInfo.CardIp, vduPort)
	go sendHeartbeatLoop(&cardAddr)
	log.Printf("In updating card ...")
	filePath, err := GetSdkFileFromRemote()
	if err != nil {
		log.Printf("error in getting upload file ...")
		return &pb.ManualOperationResponse{Status: "fail", Message: err.Error()}, err
	}
	//there is only one pci Addr in update
	cardReqElem.PciAddr = in.PciAddress
	cardReqElem.StatusCode = FAIL_STATUS
	
	upgradeFlag, err :=CheckSdkVersion(filePath, cardAddr)
	if err != nil || upgradeFlag == false{
		log.Printf("error in upgrade")
		return &pb.ManualOperationResponse{Status: "fail", Message: err.Error()}, err
	}

	wg_update.Add(1)
	go WorkerUpgrade(cardAddr, filePath, &cardReqElem.StatusCode, &wg_update)

	wg_update.Wait()

	log.Printf("status code is %d", cardReqElem.StatusCode)
	exitCode = exitCode + cardReqElem.StatusCode

	return &pb.ManualOperationResponse{Status: "success", Message: "upgrade success"}, err

}
// Listen function to listen on the UNIX domain socket
// It will return the Listener and error
func (vsp *sgVspServer) Listen() (net.Listener, error) {
	err := vsp.pathManager.EnsureSocketDirExists(vsp.pathManager.VendorPluginSocket())
	if err != nil {
		return nil, fmt.Errorf("failed to create run directory for vendor plugin socket: %v", err)
	}
	listener, err := net.Listen("unix", vsp.pathManager.VendorPluginSocket())
	if err != nil {
		return nil, fmt.Errorf("failed to listen on the vendor plugin socket: %v", err)
	}
	vsp.grpcServer = grpc.NewServer()
	pb.RegisterManualOperationServiceServer(vsp.grpcServer, vsp)
	pb.RegisterLifeCycleServiceServer(vsp.grpcServer, vsp)
	klog.Infof("gRPC server is listening on %v", listener.Addr())

	return listener, nil
}

// Serve function to serve the gRPC server on the given listener
// It will return the error
func (vsp *sgVspServer) Serve(listener net.Listener) error {
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

func (vsp *sgVspServer) Stop() {
	vsp.grpcServer.Stop()
	vsp.done <- nil
	vsp.startedWg.Wait()
}
func WithPathManager(pathManager utils.PathManager) func(*sgVspServer) {
	return func(vsp *sgVspServer) {
		vsp.pathManager = pathManager
	}
}

func NewSynaxgVspServer(opts ...func(*sgVspServer)) *sgVspServer {
	var mode string
	flag.StringVar(&mode, "mode", "", "Mode for the daemon, can be either host or dpu")
	options := zap.Options{
		Development: true,
		Level:       zapcore.DebugLevel,
	}
	options.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&options)))
	vsp := &sgVspServer{
		log:         ctrl.Log.WithName("MarvellVsp"),
		pathManager: *utils.NewPathManager("/"),
		deviceStore: make(map[string]sgDeviceInfo),
		done:        make(chan error),
	}

	for _, opt := range opts {
		opt(vsp)
	}

	return vsp
}

func main() {
	sgVspServer := NewSynaxgVspServer()
	listener, err := sgVspServer.Listen()

	if err != nil {
		sgVspServer.log.Error(err, "Failed to Listen Marvell VSP server")
		return
	}
	err = sgVspServer.Serve(listener)
	if err != nil {
		sgVspServer.log.Error(err, "Failed to serve Marvell VSP server")
		return
	}
}
