package main

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	ctrl "sigs.k8s.io/controller-runtime"
)

type HostDaemon struct {
	dev bool
	log logr.Logger
	client pb.BridgePortServiceClient
	vsp VendorPlugin
}

func (d *HostDaemon) CreateBridgePort(pf int, vf int, vlan int) (error) {
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
	req := &pb.DeleteBridgePortRequest{
		    Name: fmt.Sprintf("%d-%d-%d", pf, vf, vlan),
	}

	_, err := d.client.DeleteBridgePort(context.TODO(), req)
	return err
}

func NewHostDaemon(vsp VendorPlugin) *HostDaemon {
	return &HostDaemon{
		vsp: vsp,
		log: ctrl.Log.WithName("HostDaemon"),
	}
}

func (d *HostDaemon) Start() {
	d.log.Info("starting HostDaemon", "devflag", d.dev)

	addr, port, err := d.vsp.Start()
	if err != nil {
		d.log.Error(err, "VSP init returned error")
	}
	conn, err := grpc.Dial(fmt.Sprintf("%s:%d", addr , port), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		d.log.Error(err, "did not connect")
	}
	defer conn.Close()
	d.client = pb.NewBridgePortServiceClient(conn)

	// TODO: replace this indefinte wait with a service that 
	// listens to requests coming from the cni shim
	select {}
}
