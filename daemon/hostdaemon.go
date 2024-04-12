package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	cni100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/go-logr/logr"
	"github.com/openshift/dpu-operator/daemon/plugin"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cniserver"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/sriov"
	pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	ctrl "sigs.k8s.io/controller-runtime"
)

type HostDaemon struct {
	dev           bool
	log           logr.Logger
	conn          *grpc.ClientConn
	client        pb.BridgePortServiceClient
	vsp           plugin.VendorPlugin
	addr          string
	port          int32
	cniServerPath string
	cniserver     *cniserver.Server
	sm            sriov.Manager
}

func (d *HostDaemon) CreateBridgePort(pf int, vf int, vlan int, mac string) (*pb.BridgePort, error) {
	err := d.connectWithRetry()
	if err != nil {
		return nil, err
	}

	m, err := net.ParseMAC(mac)
	if err != nil {
		return nil, err
	}

	createRequest := &pb.CreateBridgePortRequest{
		BridgePort: &pb.BridgePort{
			Name: fmt.Sprintf("%d-%d", pf, vf),
			Spec: &pb.BridgePortSpec{
				Ptype:      1,
				MacAddress: m,
				LogicalBridges: []string{
					fmt.Sprintf("%d", vlan),
				},
			},
		},
	}

	return d.client.CreateBridgePort(context.TODO(), createRequest)
}

func (d *HostDaemon) DeleteBridgePort(pf int, vf int, vlan int, mac string) error {
	d.connectWithRetry()
	req := &pb.DeleteBridgePortRequest{
		Name: fmt.Sprintf("%d-%d-%d-%s", pf, vf, vlan, mac),
	}

	_, err := d.client.DeleteBridgePort(context.TODO(), req)
	return err
}

func NewHostDaemon(vsp plugin.VendorPlugin) *HostDaemon {
	return &HostDaemon{
		vsp:           vsp,
		log:           ctrl.Log.WithName("HostDaemon"),
		cniServerPath: cnitypes.ServerSocketPath,
		sm:            sriov.NewSriovManager(),
	}
}

func (d *HostDaemon) WithCniServerPath(serverPath string) *HostDaemon {
	d.cniServerPath = serverPath
	return d
}

func (d *HostDaemon) WithSriovManager(manager sriov.Manager) *HostDaemon {
	d.sm = manager
	return d
}

func (d *HostDaemon) connectWithRetry() error {
	if d.conn != nil {
		return nil
	}
	// Might want to change waitForReady to true to
	// block on connection. Currently, we connect
	// "just in time" so the grpc immediately after
	// the dial will fail if connection failed (after
	// retries)
	retryPolicy := `{
		"methodConfig": [{
		  "waitForReady": false,
		  "retryPolicy": {
			  "MaxAttempts": 40,
			  "InitialBackoff": "1s",
			  "MaxBackoff": "16s",
			  "BackoffMultiplier": 2.0,
			  "RetryableStatusCodes": [ "UNAVAILABLE" ]
		  }
		}]}`

	conn, err := grpc.Dial(fmt.Sprintf("%s:%d", d.addr, d.port), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultServiceConfig(retryPolicy))
	if err != nil {
		d.log.Error(err, "did not connect")
		return err
	}
	d.conn = conn
	d.client = pb.NewBridgePortServiceClient(conn)
	return nil
}

func (d *HostDaemon) cniCmdAddHandler(req *cnitypes.PodRequest) (*cni100.Result, error) {
	res, err := d.sm.CmdAdd(req)
	if err != nil {
		return nil, fmt.Errorf("SRIOV manager failed in add handler: %v", err)
	}
	pf := 0
	vf := req.CNIConf.VFID
	mac := req.CNIConf.OrigVfState.EffectiveMAC
	d.log.Info("addHandler", "CNIConf", req.CNIConf)
	vlan := *req.CNIConf.Vlan
	d.log.Info("addHandler", "pf", pf, "vf", vf, "mac", mac, "vlan", vlan)
	_, err = d.CreateBridgePort(pf, vf, vlan, mac)
	if err != nil {
		return nil, fmt.Errorf("Failed to call CreateBridgePort: %v", err)
	}
	d.log.Info("addHandler CreateBridgePort succeeded")

	d.log.Info("addHandler d.sm.CmdAdd succeeded")
	return res, nil
}

func (d *HostDaemon) cniCmdDelHandler(req *cnitypes.PodRequest) (*cni100.Result, error) {
	err := d.sm.CmdDel(req)
	if err != nil {
		return nil, errors.New("SRIOV manager failed in del handler")
	}
	pf := 0
	vf := req.CNIConf.VFID
	mac := req.CNIConf.OrigVfState.EffectiveMAC
	vlan := *req.CNIConf.Vlan
	d.log.Info("delHandler", "pf", pf, "vf", vf, "mac", mac, "vlan", vlan)
	d.DeleteBridgePort(pf, vf, vlan, mac)
	return nil, nil
}

func (d *HostDaemon) Listen() (net.Listener, error) {
	d.log.Info("starting HostDaemon", "devflag", d.dev, "cniServerPath", d.cniServerPath)

	addr, port, err := d.vsp.Start()
	if err != nil {
		d.log.Error(err, "VSP init returned error")
	}
	d.addr = addr
	d.port = port

	add := func(r *cnitypes.PodRequest) (*cni100.Result, error) {
		return d.cniCmdAddHandler(r)
	}
	del := func(r *cnitypes.PodRequest) (*cni100.Result, error) {
		return d.cniCmdDelHandler(r)
	}

	d.cniserver = cniserver.NewCNIServer(add, del, cniserver.WithSocketPath(d.cniServerPath))

	return d.cniserver.Listen()
}

func (d *HostDaemon) ListenAndServe() error {
	listener, err := d.Listen()

	if err != nil {
		d.log.Error(err, "Failed to listen")
		return err
	}
	return d.Serve(listener)
}

func (d *HostDaemon) Serve(listener net.Listener) error {
	err := d.cniserver.Serve(listener)
	if err != nil {
		d.log.Error(err, "Error starting CNI server for shim")
		return err
	}
	return nil
}

func (d *HostDaemon) Stop() {
	if d.cniserver != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()
		d.cniserver.Shutdown(ctx)
		d.cniserver = nil
	}
}
