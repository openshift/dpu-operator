package main

import (
	"context"
	"errors"
	"fmt"

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
	dev bool
	log logr.Logger
	conn *grpc.ClientConn
	client pb.BridgePortServiceClient
	vsp plugin.VendorPlugin
	addr string
	port int32
	cniServerPath string
	cniserver *cniserver.Server
}

func (d *HostDaemon) CreateBridgePort(pf int, vf int, vlan int, mac string) (error) {
	d.ensureConnected()
	createRequest := &pb.CreateBridgePortRequest{
		BridgePort: &pb.BridgePort{
			Name: fmt.Sprintf("%d-%d-%d-%s", pf, vf, vlan, mac),
			Spec: &pb.BridgePortSpec{
				Ptype:          1,
				MacAddress:     []byte{},
				LogicalBridges: []string{},
			},
		},
	}

	_, err := d.client.CreateBridgePort(context.TODO(), createRequest)
	return err
}

func (d *HostDaemon) DeleteBridgePort(pf int, vf int, vlan int, mac string) (error) {
	d.ensureConnected()
	req := &pb.DeleteBridgePortRequest{
		    Name: fmt.Sprintf("%d-%d-%d-%s", pf, vf, vlan, mac),
	}

	_, err := d.client.DeleteBridgePort(context.TODO(), req)
	return err
}

func NewHostDaemon(vsp plugin.VendorPlugin) *HostDaemon {
	return &HostDaemon{
		vsp: vsp,
		log: ctrl.Log.WithName("HostDaemon"),
		cniServerPath: "/var/", // TODO
	}
}

func (d *HostDaemon) WithCniServerPath(serverPath string) *HostDaemon {
	d.cniServerPath = serverPath
	return d
}

func (d *HostDaemon) ensureConnected() {
	if d.conn != nil {
		return
	}
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

	conn, err := grpc.Dial(fmt.Sprintf("%s:%d", d.addr , d.port), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultServiceConfig(retryPolicy))
	if err != nil {
		d.log.Error(err, "did not connect")
	}
	d.conn = conn
	d.client = pb.NewBridgePortServiceClient(conn)
}

func (d *HostDaemon) addHandler(req *cnitypes.PodRequest) (*cni100.Result, error) {	
	pf := 0
	vf := req.CNIConf.VFID
	mac := req.CNIConf.MAC
	vlan := 7
	d.CreateBridgePort(pf, vf, vlan, mac)

	sm := sriov.NewSriovManager()
	res, err := sm.CmdAdd(req)
	if err != nil {
	    return nil, errors.New("SRIOV manager falied in add handler")
	}
	return res, nil
}

func (d *HostDaemon) delHandler(req *cnitypes.PodRequest) (*cni100.Result, error) {
	pf := 0
	vf := req.CNIConf.VFID
	mac := req.CNIConf.MAC
	vlan := 7
	d.DeleteBridgePort(pf, vf, vlan, mac)

	sm := sriov.NewSriovManager()
	err := sm.CmdDel(req)
	if err != nil {
	    return nil, errors.New("SRIOV manager falied in del handler")
	}
	return nil, nil
}

func (d *HostDaemon) Start() {
	d.log.Info("starting HostDaemon", "devflag", d.dev)

	addr, port, err := d.vsp.Start()
	if err != nil {
		d.log.Error(err, "VSP init returned error")
	}
	d.addr = addr
	d.port = port

	add := func(r *cnitypes.PodRequest) (*cni100.Result, error) {
		return d.addHandler(r)
	}
	del := func(r *cnitypes.PodRequest) (*cni100.Result, error) {
		return d.delHandler(r)
	}

	server := cniserver.NewCNIServer(add, del)
	err = server.Start()

	if err != nil {
		d.log.Error(err, "Error starting CNI server for shim")
	}

	d.cniserver = server
	go func() {	
		d.cniserver.Start()
	}()
}

func (d *HostDaemon) Stop() {

}
