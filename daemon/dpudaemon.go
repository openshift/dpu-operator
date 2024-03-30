package main

import (
	"context"
	"fmt"
	"net"

	"github.com/go-logr/logr"
	"github.com/openshift/dpu-operator/daemon/plugin"
	pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"google.golang.org/grpc"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
	ctrl "sigs.k8s.io/controller-runtime"
)

type DpuDaemon struct {
	pb.UnimplementedBridgePortServiceServer
	vsp    plugin.VendorPlugin
	log    logr.Logger
	server *grpc.Server
}

func (s *DpuDaemon) CreateBridgePort(context context.Context, bpr *pb.CreateBridgePortRequest) (*pb.BridgePort, error) {
	err := s.vsp.CreateBridgePort(bpr)
	return nil, err
}

func (s *DpuDaemon) DeleteBridgePort(context context.Context, bpr *pb.DeleteBridgePortRequest) (*emptypb.Empty, error) {
	err := s.vsp.DeleteBridgePort(bpr)
	return nil, err
}

func NewDpuDaemon(vsp plugin.VendorPlugin) *DpuDaemon {
	return &DpuDaemon{
		vsp: vsp,
		log: ctrl.Log.WithName("DpuDaemon"),
	}
}

func (d *DpuDaemon) Start() {
	d.log.Info("starting DpuDaemon")
	addr, port, err := d.vsp.Start()
	if err != nil {
		d.log.Error(err, "Failed to get addr:port from VendorPlugin")
	}
	d.server = grpc.NewServer()
	pb.RegisterBridgePortServiceServer(d.server, d)

	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", addr, port))
	if err != nil {
		d.log.Error(err, "Failed to start listening")
	}

	go func() {
		d.log.Info("server listening", "address", lis.Addr())

		if err := d.server.Serve(lis); err != nil {
			d.log.Error(err, "Failed to start serving")
			panic("Failed to listen")
		}
	}()
}

func (d *DpuDaemon) Stop() {
	if d.server != nil {
		d.server.GracefulStop()
		d.server = nil
	}
}
