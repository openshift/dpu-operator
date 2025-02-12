package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	cni100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/go-logr/logr"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cniserver"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/sriov"
	deviceplugin "github.com/openshift/dpu-operator/internal/daemon/device-plugin"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	sfcreconciler "github.com/openshift/dpu-operator/internal/daemon/sfc-reconciler"
	"github.com/openshift/dpu-operator/internal/scheme"
	"github.com/openshift/dpu-operator/internal/utils"
	pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
)

type HostSideManager struct {
	dev           bool
	log           logr.Logger
	conn          *grpc.ClientConn
	client        pb.BridgePortServiceClient
	config        *rest.Config
	vsp           plugin.VendorPlugin
	dp            deviceplugin.DevicePlugin
	addr          string
	port          int32
	cniserver     *cniserver.Server
	sm            sriov.Manager
	manager       ctrl.Manager
	startedWg     sync.WaitGroup
	pathManager   utils.PathManager
	stopRequested bool
	dpListener    net.Listener
}

func (d *HostSideManager) CreateBridgePort(pf int, vf int, vlan int, mac string) (*pb.BridgePort, error) {
	err := d.connectWithRetry()
	if err != nil {
		return nil, fmt.Errorf("Failed to connect with retry: %v", err)
	}

	m, err := net.ParseMAC(mac)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse Mac: %v", err)
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

func (d *HostSideManager) DeleteBridgePort(pf int, vf int, vlan int, mac string) error {
	d.connectWithRetry()
	req := &pb.DeleteBridgePortRequest{Name: "host" + fmt.Sprintf("%d-%d", pf, vf)}

	_, err := d.client.DeleteBridgePort(context.TODO(), req)
	return err
}

func NewHostSideManager(vsp plugin.VendorPlugin, opts ...func(*HostSideManager)) *HostSideManager {
	h := &HostSideManager{
		vsp:           vsp,
		log:           ctrl.Log.WithName("HostDaemon"),
		sm:            sriov.NewSriovManager(),
		pathManager:   *utils.NewPathManager("/"),
		stopRequested: false,
	}

	for _, opt := range opts {
		opt(h)
	}

	h.dp = deviceplugin.NewDevicePlugin(vsp, false, h.pathManager)
	if h.config == nil {
		h.config = ctrl.GetConfigOrDie()
	}
	return h
}

func WithPathManager2(pathManager *utils.PathManager) func(*HostSideManager) {
	return func(d *HostSideManager) {
		d.pathManager = *pathManager
	}
}

func WithSriovManager(manager sriov.Manager) func(*HostSideManager) {
	return func(d *HostSideManager) {
		d.sm = manager
	}
}

func WithClient(client *rest.Config) func(*HostSideManager) {
	return func(d *HostSideManager) {
		d.config = client
	}
}

func (d *HostSideManager) WithManager(manager ctrl.Manager) *HostSideManager {
	d.manager = manager
	return d
}

func (d *HostSideManager) connectWithRetry() error {
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
		return fmt.Errorf("connectWithRetry dial failed: %v", err)
	}
	d.log.Info("Dial succeeded", "addr", d.addr, "port", d.port)
	d.conn = conn
	d.client = pb.NewBridgePortServiceClient(conn)
	return nil
}

func (d *HostSideManager) cniCmdAddHandler(req *cnitypes.PodRequest) (*cni100.Result, error) {
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

func (d *HostSideManager) cniCmdDelHandler(req *cnitypes.PodRequest) (*cni100.Result, error) {
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

func (d *HostSideManager) Listen() (net.Listener, error) {
	d.startedWg.Add(1)
	d.log.Info("Starting HostDaemon", "devflag", d.dev, "cniServerPath", d.pathManager.CNIServerPath())

	d.setupReconcilers()
	addr, port, err := d.vsp.Start()
	if err != nil {
		d.log.Error(err, "VSP init returned error")
		return nil, err
	}
	d.addr = addr
	d.port = port

	add := func(r *cnitypes.PodRequest) (*cni100.Result, error) {
		return d.cniCmdAddHandler(r)
	}
	del := func(r *cnitypes.PodRequest) (*cni100.Result, error) {
		return d.cniCmdDelHandler(r)
	}

	d.cniserver = cniserver.NewCNIServer(add, del, cniserver.WithPathManager(d.pathManager))
	d.dpListener, err = d.dp.Listen()
	if err != nil {
		return nil, fmt.Errorf("HostSideManager Failed to Listen while calling device plugin listen: %v", err)
	}

	return d.cniserver.Listen()
}

func (d *HostSideManager) ListenAndServe() error {
	listener, err := d.Listen()

	if err != nil {
		d.log.Error(err, "Failed to listen")
		return err
	}

	return d.Serve(listener)
}

func (d *HostSideManager) Serve(listener net.Listener) error {
	var wg sync.WaitGroup
	var err error
	done := make(chan error, 3)
	wg.Add(1)
	go func() {
		d.log.Info("Starting CNI server")
		if err := d.cniserver.Serve(listener); err != nil {
			done <- err
		} else {
			done <- nil
		}
		d.log.Info("Stopped CNI server")
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		d.log.Info("Starting Device Plugin server")
		if err := d.dp.Serve(d.dpListener); err != nil {
			done <- err
		} else {
			done <- nil
		}
		d.log.Info("Stopped Device Plugin server")
		wg.Done()
	}()

	ctx, cancelManager := utils.CancelFunc()

	wg.Add(1)
	go func() {
		d.log.Info("Starting manager")
		if err := d.manager.Start(ctx); err != nil {
			done <- err
		} else {
			done <- nil
		}
		d.log.Info("Stopped manager")
		wg.Done()
	}()

	// Block on any go routines writing to the done channel when an error occurs or they
	// are forced to exit.
	err = <-done

	d.cniserver.Shutdown(context.TODO())
	d.dp.Stop()
	cancelManager()
	wg.Wait()
	d.startedWg.Done()

	if d.stopRequested {
		err = nil
	}
	return err
}

func (d *HostSideManager) Stop() {
	d.log.Info("Stopping HostSideManager")
	d.stopRequested = true
	if d.cniserver != nil {
		d.cniserver.ShutdownAndWait()
		d.startedWg.Wait()
		d.cniserver = nil
	}
	d.log.Info("Stopped HostSideManager")
}

var (
	setupLog = ctrl.Log.WithName("setup")
)

func (d *HostSideManager) setupReconcilers() {
	d.log.Info("HostSideManager.setupReconcilers()")
	if d.manager == nil {
		mgr, err := ctrl.NewManager(d.config, ctrl.Options{
			Scheme: scheme.Scheme,
			NewCache: func(config *rest.Config, opts cache.Options) (cache.Cache, error) {
				opts.DefaultNamespaces = map[string]cache.Config{
					"openshift-dpu-operator": {},
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
