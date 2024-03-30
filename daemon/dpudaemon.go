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
	vsp plugin.VendorPlugin
	log logr.Logger
}

func (s *DpuDaemon) CreateBridgePort(context context.Context, bpr *pb.CreateBridgePortRequest) (*pb.BridgePort, error) {
	s.log.Info("Create Bridge Port", "req", bpr.BridgePort.Name)
	return &pb.BridgePort{}, nil
}

func (s *DpuDaemon) DeleteBridgePort(context context.Context, bpr *pb.DeleteBridgePortRequest) (*emptypb.Empty, error) {
	s.log.Info("Delete Bridge Port", "req", bpr.Name)
	return nil, nil
}

func NewDpuDaemon(vsp plugin.VendorPlugin) *DpuDaemon {
	return &DpuDaemon{
		vsp: vsp,
		log: ctrl.Log.WithName("DpuDaemon"),
	}
}

func (d *DpuDaemon) Start() {
	var err error
	d.log.Info("starting DpuDaemon")
	addr, port, err := d.vsp.Start()

	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", addr, port))
	if err != nil {
		d.log.Error(err, "Failed to start listening")
	}
	server := grpc.NewServer()
	pb.RegisterBridgePortServiceServer(server, d)
	d.log.Info("server listening", "address", lis.Addr())

	if err := server.Serve(lis); err != nil {
		d.log.Error(err, "Failed to start serving")
	}
}
