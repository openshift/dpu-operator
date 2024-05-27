package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	cni100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/go-logr/logr"
	configv1 "github.com/openshift/dpu-operator/api/v1"
	"github.com/openshift/dpu-operator/daemon/plugin"
	sfcreconciler "github.com/openshift/dpu-operator/daemon/sfc-reconciler"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cniserver"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/sriov"
	pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
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
	manager       ctrl.Manager
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
			Name: "host" + fmt.Sprintf("%d-%d", pf, vf),
			Spec: &pb.BridgePortSpec{
				Ptype:      1,
				MacAddress: m,
				LogicalBridges: []string{
					// TODO: Remove +2
					fmt.Sprintf("%d", vf+2),
				},
			},
		},
	}

	return d.client.CreateBridgePort(context.TODO(), createRequest)
}

func (d *HostDaemon) DeleteBridgePort(pf int, vf int, vlan int, mac string) error {
	d.connectWithRetry()
	req := &pb.DeleteBridgePortRequest{Name: "host" + fmt.Sprintf("%d-%d", pf, vf)}

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

func (d *HostDaemon) WithManager(manager ctrl.Manager) *HostDaemon {
	d.manager = manager
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
	d.log.Info("Dial succeeded", "addr", d.addr, "port", d.port)
	d.conn = conn
	d.client = pb.NewBridgePortServiceClient(conn)
	return nil
}

func (d *HostDaemon) cniCmdAddHandler(req *cnitypes.PodRequest) (*cni100.Result, error) {
	d.log.Info("addHandler")
	res, err := d.sm.CmdAdd(req)
	if err != nil {
		return nil, fmt.Errorf("SRIOV manager failed in add handler: %v", err)
	}
	d.log.Info("addHandler d.sm.CmdAdd succeeded")
	pf := 0
	vf := req.CNIConf.VFID
	mac := req.CNIConf.OrigVfState.EffectiveMAC
	d.log.Info("addHandler", "CNIConf", req.CNIConf)
	// TODO: fix setting Vlan based on network definition in CR
	vlan := 2 // *req.CNIConf.Vlan
	d.log.Info("addHandler", "pf", pf, "vf", vf, "mac", mac, "vlan", vlan)
	_, err = d.CreateBridgePort(pf, vf, vlan, mac)
	if err != nil {
		return nil, fmt.Errorf("Failed to call CreateBridgePort: %v", err)
	}
	d.log.Info("addHandler CreateBridgePort succeeded")

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
	// TODO: fix setting Vlan based on network definition in CR
	vlan := 2 // *req.CNIConf.Vlan
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
	var wg sync.WaitGroup
	done := make(chan error, 1)
	listener, err := d.Listen()

	if err != nil {
		d.log.Error(err, "Failed to listen")
		return err
	}

	wg.Add(1)
	go func() {
		d.log.Info("Starging CNI server")
		if err := d.Serve(listener); err != nil {
			done <- err
		} else {
			done <- nil
		}
		wg.Done()
	}()

	d.setupReconcilers()
	wg.Add(1)

	ctx, cancelManager := context.WithCancel(ctrl.SetupSignalHandler())
	go func() {
		d.log.Info("Starting manager")

		if err := d.manager.Start(ctx); err != nil {
			done <- err
		} else {
			done <- nil
		}
		wg.Done()
	}()

	cancelManager()
	d.cniserver.Shutdown(context.TODO())
	wg.Wait()

	return err
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

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(configv1.AddToScheme(scheme))
}

func (d *HostDaemon) setupReconcilers() {
	if d.manager == nil {
		mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
			Scheme: scheme,
			NewCache: func(config *rest.Config, opts cache.Options) (cache.Cache, error) {
				opts.DefaultNamespaces = map[string]cache.Config{
					"dpu-operator-system": {},
				}
				return cache.New(config, opts)
			},
		})
		if err != nil {
			d.log.Error(err, "unable to start manager")
		}

		sfcReconciler := &sfcreconciler.SfcReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}

		if err = sfcReconciler.SetupWithManager(mgr); err != nil {
			d.log.Error(err, "unable to create controller", "controller", "ServiceFunctionChain")
		}
		d.manager = mgr
	}
}
