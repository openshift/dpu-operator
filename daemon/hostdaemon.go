package main

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/openshift/dpu-operator/daemon/plugin"
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
}

func (d *HostDaemon) CreateBridgePort(pf int, vf int, vlan int) (error) {
	d.ensureConnected()
	createRequest := &pb.CreateBridgePortRequest{
		BridgePort: &pb.BridgePort{
			Name: fmt.Sprintf("%d-%d-%d", pf, vf, vlan),
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

func (d *HostDaemon) DeleteBridgePort(pf int, vf int, vlan int) (error) {
	d.ensureConnected()
	req := &pb.DeleteBridgePortRequest{
		    Name: fmt.Sprintf("%d-%d-%d", pf, vf, vlan),
	}

	_, err := d.client.DeleteBridgePort(context.TODO(), req)
	return err
}

func NewHostDaemon(vsp plugin.VendorPlugin) *HostDaemon {
	return &HostDaemon{
		vsp: vsp,
		log: ctrl.Log.WithName("HostDaemon"),
	}
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

func (d *HostDaemon) Start() {
	d.log.Info("starting HostDaemon", "devflag", d.dev)

	addr, port, err := d.vsp.Start()
	if err != nil {
		d.log.Error(err, "VSP init returned error")
	}
	d.addr = addr
	d.port = port
}
