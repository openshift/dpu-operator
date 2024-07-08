package mockvsp

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/go-logr/logr"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/openshift/dpu-operator/internal/utils"
	opi "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	ctrl "sigs.k8s.io/controller-runtime"
)

type vspServer struct {
	pb.UnimplementedLifeCycleServiceServer
	pb.UnimplementedNetworkFunctionServiceServer
	pb.UnimplementedDeviceServiceServer
	opi.UnimplementedBridgePortServiceServer
	log         logr.Logger
	wg          sync.WaitGroup
	startedWg   sync.WaitGroup
	done        chan error
	grpcServer  *grpc.Server
	pathManager utils.PathManager
}

func (vsp *vspServer) Init(ctx context.Context, in *pb.InitRequest) (*pb.IpPort, error) {
	vsp.log.Info("Received Init() request", "DpuMode", in.DpuMode)
	return &pb.IpPort{
		Ip:   "127.0.0.1",
		Port: 50051,
	}, nil
}

func (vsp *vspServer) GetDevices(ctx context.Context, in *pb.Empty) (*pb.DeviceListResponse, error) {
	devices := map[string]*pb.Device{
		"ens5f0": {ID: "ens5f0", Health: "Healthy"},
		"ens5f1": {ID: "ens5f1", Health: "Healthy"},
		"ens5f2": {ID: "ens5f2", Health: "Healthy"},
		"ens5f3": {ID: "ens5f3", Health: "Healthy"},
	}

	return &pb.DeviceListResponse{
		Devices: devices,
	}, nil
}

func (vsp *vspServer) CreateBridgePort(ctx context.Context, in *opi.CreateBridgePortRequest) (*opi.BridgePort, error) {
	vsp.log.Info("Received CreateBridgePort() request", "BridgePortId", in.BridgePortId, "BridgePortId", in.BridgePortId)
	return &opi.BridgePort{}, nil
}

func (vsp *vspServer) DeleteBridgePort(ctx context.Context, in *opi.DeleteBridgePortRequest) (*emptypb.Empty, error) {
	vsp.log.Info("Received DeleteBridgePort() request", "Name", in.Name, "AllowMissing", in.AllowMissing)
	return nil, nil
}

func (vsp *vspServer) CreateNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {
	vsp.log.Info("Received CreateNetworkFunction() request", "Input", in.Input, "Output", in.Output)
	return nil, nil
}

func (vsp *vspServer) DeleteNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {
	vsp.log.Info("Received DeleteNetworkFunction() request", "Input", in.Input, "Output", in.Output)
	return nil, nil
}

func (vsp *vspServer) Listen() (net.Listener, error) {
	err := vsp.pathManager.EnsureSocketDirExists(vsp.pathManager.VendorPluginSocket())
	if err != nil {
		return nil, fmt.Errorf("failed to create run directory for vendor plugin socket: %v", err)
	}
	listener, err := net.Listen("unix", vsp.pathManager.VendorPluginSocket())
	if err != nil {
		return nil, fmt.Errorf("failed to listen on the vendor plugin socket: %v", err)
	}

	vsp.grpcServer = grpc.NewServer()
	pb.RegisterNetworkFunctionServiceServer(vsp.grpcServer, &vspServer{})
	pb.RegisterLifeCycleServiceServer(vsp.grpcServer, &vspServer{})
	pb.RegisterDeviceServiceServer(vsp.grpcServer, &vspServer{})
	opi.RegisterBridgePortServiceServer(vsp.grpcServer, &vspServer{})
	vsp.log.Info("gRPC server is listening", "listener.Addr()", listener.Addr())
	return listener, nil
}

func (vsp *vspServer) Serve(listener net.Listener) error {
	vsp.wg.Add(1)
	go func() {
		vsp.log.Info("Starting Mock VSP")
		if err := vsp.grpcServer.Serve(listener); err != nil {
			vsp.done <- err
		} else {
			vsp.done <- nil
		}
		vsp.log.Info("Stopping Mock VSP")
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

func (vsp *vspServer) Stop() {
	vsp.done <- nil
	vsp.startedWg.Wait()
}

func WithPathManager(pathManager utils.PathManager) func(*vspServer) {
	return func(d *vspServer) {
		d.pathManager = pathManager
	}
}

func NewMockVsp(opts ...func(*vspServer)) *vspServer {
	vsp := &vspServer{
		log:         ctrl.Log.WithName("MockVsp"),
		pathManager: *utils.NewPathManager("/"),
	}

	for _, opt := range opts {
		opt(vsp)
	}

	return vsp
}
