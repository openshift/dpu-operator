package main

import (
	"context"
	"errors"
	"fmt"
	"net"

	cni100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/go-logr/logr"
	deviceplugin "github.com/openshift/dpu-operator/daemon/device-plugin"
	"github.com/openshift/dpu-operator/daemon/plugin"
	pb2 "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cniserver"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/networkfn"
	pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"google.golang.org/grpc"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
	ctrl "sigs.k8s.io/controller-runtime"
)

type DpuDaemon struct {
	pb.UnimplementedBridgePortServiceServer
	pb2.UnimplementedDeviceServiceServer
	vsp           plugin.VendorPlugin
	dp            deviceplugin.DevicePlugin
	log           logr.Logger
	server        *grpc.Server
	cniServerPath string
	cniserver     *cniserver.Server
}

func (s *DpuDaemon) CreateBridgePort(context context.Context, bpr *pb.CreateBridgePortRequest) (*pb.BridgePort, error) {
	s.log.Info("Passing CreateBridgePort", "name", bpr.BridgePort.Name)
	return s.vsp.CreateBridgePort(bpr)
}

func (s *DpuDaemon) DeleteBridgePort(context context.Context, bpr *pb.DeleteBridgePortRequest) (*emptypb.Empty, error) {
	s.log.Info("Passing DeleteBridgePort", "name", bpr.Name)
	err := s.vsp.DeleteBridgePort(bpr)
	return &emptypb.Empty{}, err
}

func NewDpuDaemon(vsp plugin.VendorPlugin, dp deviceplugin.DevicePlugin) *DpuDaemon {
	return &DpuDaemon{
		vsp:           vsp,
		dp:            dp,
		cniServerPath: cnitypes.ServerSocketPath,
		log:           ctrl.Log.WithName("DpuDaemon"),
	}
}

func (d *DpuDaemon) cniCmdNfAddHandler(req *cnitypes.PodRequest) (*cni100.Result, error) {
	d.log.Info("cniCmdNfAddHandler")
	res, err := networkfn.CmdAdd(req)
	if err != nil {
		return nil, fmt.Errorf("SRIOV manager failed in add handler: %v", err)
	}
	d.log.Info("cniCmdNfAddHandler CmdAdd succeeded")
	return res, nil
}

func (d *DpuDaemon) cniCmdNfDelHandler(req *cnitypes.PodRequest) (*cni100.Result, error) {
	d.log.Info("cniCmdNfDelHandler")
	err := networkfn.CmdDel(req)
	if err != nil {
		return nil, errors.New("SRIOV manager failed in del handler")
	}
	d.log.Info("cniCmdNfDelHandler CmdDel succeeded")
	return nil, nil
}

func (d *DpuDaemon) Listen() (net.Listener, error) {
	d.log.Info("starting DpuDaemon")
	addr, port, err := d.vsp.Start()
	if err != nil {
		d.log.Error(err, "Failed to get addr:port from VendorPlugin")
	}

	err = d.dp.Start()
	if err != nil {
		d.log.Error(err, "device plugin call failed")
	}

	d.server = grpc.NewServer()
	pb.RegisterBridgePortServiceServer(d.server, d)

	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", addr, port))
	if err != nil {
		d.log.Error(err, "Failed to start listening on", "addr", addr, "port", port)
		return lis, err
	}
	d.log.Info("server listening", "address", lis.Addr())

	add := func(r *cnitypes.PodRequest) (*cni100.Result, error) {
		return d.cniCmdNfAddHandler(r)
	}
	del := func(r *cnitypes.PodRequest) (*cni100.Result, error) {
		return d.cniCmdNfDelHandler(r)
	}

	d.cniserver = cniserver.NewCNIServer(add, del, cniserver.WithSocketPath(d.cniServerPath))

	return lis, err
}

func (d *DpuDaemon) ListenAndServe() error {
	lis, err := d.Listen()
	if err != nil {
		return err
	}
	return d.Serve(lis)
}

func (d *DpuDaemon) Serve(listen net.Listener) error {
	d.server = grpc.NewServer()
	pb.RegisterBridgePortServiceServer(d.server, d)
	if err := d.server.Serve(listen); err != nil {
		d.log.Error(err, "Failed to start serving")
		return err
	}
	return nil
}

func (d *DpuDaemon) Stop() {
	if d.server != nil {
		d.server.GracefulStop()
		d.server = nil
	}
}
