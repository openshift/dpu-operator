package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	cni100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/go-logr/logr"
	pb2 "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cniserver"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/networkfn"
	deviceplugin "github.com/openshift/dpu-operator/internal/daemon/device-plugin"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	sfcreconciler "github.com/openshift/dpu-operator/internal/daemon/sfc-reconciler"
	"github.com/openshift/dpu-operator/internal/scheme"
	"github.com/openshift/dpu-operator/internal/utils"
	"github.com/openshift/dpu-operator/pkgs/vars"
	pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"google.golang.org/grpc"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

type DpuSideManager struct {
	pb.UnimplementedBridgePortServiceServer
	pb2.UnimplementedDeviceServiceServer

	vsp         plugin.VendorPlugin
	dp          deviceplugin.DevicePlugin
	addr        string
	port        int32
	log         logr.Logger
	server      *grpc.Server
	cniserver   *cniserver.Server
	manager     ctrl.Manager
	macStore    map[string][]string
	startedWg   sync.WaitGroup
	config      *rest.Config
	pathManager utils.PathManager
}

func (s *DpuSideManager) CreateBridgePort(context context.Context, bpr *pb.CreateBridgePortRequest) (*pb.BridgePort, error) {
	s.log.Info("Passing CreateBridgePort", "name", bpr.BridgePort.Name)
	return s.vsp.CreateBridgePort(bpr)
}

func (s *DpuSideManager) DeleteBridgePort(context context.Context, bpr *pb.DeleteBridgePortRequest) (*emptypb.Empty, error) {
	s.log.Info("Passing DeleteBridgePort", "name", bpr.Name)
	err := s.vsp.DeleteBridgePort(bpr)
	return &emptypb.Empty{}, err
}

func NewDpuSideManager(vsp plugin.VendorPlugin, config *rest.Config, opts ...func(*DpuSideManager)) (*DpuSideManager, error) {
	d := &DpuSideManager{
		vsp:         vsp,
		pathManager: *utils.NewPathManager("/"),
		log:         ctrl.Log.WithName("DpuSideManager"),
		macStore:    make(map[string][]string),
		config:      config,
	}

	for _, opt := range opts {
		opt(d)
	}

	d.dp = deviceplugin.NewDevicePlugin(vsp, true, d.pathManager)

	return d, nil
}

func WithPathManager(pathManager utils.PathManager) func(*DpuSideManager) {
	return func(d *DpuSideManager) {
		d.pathManager = pathManager
	}
}

func (d *DpuSideManager) StartVsp(ctx context.Context) error {
	addr, port, err := d.vsp.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed calling VSP Start() from DpuSideManager: %v", err)
	}
	d.addr = addr
	d.port = port
	return nil
}

func (d *DpuSideManager) SetupDevices() error {
	err := d.dp.SetupDevices()
	if err != nil {
		return fmt.Errorf("failed calling SetupDevices from DpuSideManager: %v", err)
	}
	return nil
}

func (d *DpuSideManager) cniCmdNfAddHandler(req *cnitypes.PodRequest) (*cni100.Result, error) {
	d.log.Info("cniCmdNfAddHandler")
	res, err := networkfn.CmdAdd(req)
	if err != nil {
		return nil, fmt.Errorf("SRIOV manager failed in add handler: %v", err)
	}

	d.macStore[req.Netns] = append(d.macStore[req.Netns], req.CNIConf.MAC)
	if len(d.macStore[req.Netns]) == 2 {
		d.log.Info("cniCmdNfAddHandler", "req.Netns", req.Netns)
		macs := d.macStore[req.Netns]
		d.vsp.CreateNetworkFunction(macs[0], macs[1])
	}
	d.log.Info("cniCmdNfAddHandler CmdAdd succeeded")
	return res, nil
}

func (d *DpuSideManager) cniCmdNfDelHandler(req *cnitypes.PodRequest) (*cni100.Result, error) {
	d.log.Info("cniCmdNfDelHandler")
	err := networkfn.CmdDel(req)
	if err != nil {
		return nil, errors.New("SRIOV manager failed in del handler")
	}

	macs := d.macStore[req.Netns]

	if len(macs) == 2 {
		d.log.Info("cniCmdNfDelHandler", "req.Netns", req.Netns)
		d.vsp.DeleteNetworkFunction(macs[0], macs[1])
	}

	d.macStore[req.Netns] = macs[:len(macs)-1]

	d.log.Info("cniCmdNfDelHandler CmdDel succeeded")
	return nil, nil
}

func (d *DpuSideManager) Listen() (net.Listener, error) {
	d.startedWg.Add(1)
	d.log.Info("Starting DpuDaemon")
	d.server = grpc.NewServer()
	d.setupReconcilers()

	pb.RegisterBridgePortServiceServer(d.server, d)

	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", d.addr, d.port))
	if err != nil {
		return lis, fmt.Errorf("Failed to start listening on %v:%v: %v", d.addr, d.port, err)
	}
	d.log.Info("server listening", "address", lis.Addr())

	add := func(r *cnitypes.PodRequest) (*cni100.Result, error) {
		return d.cniCmdNfAddHandler(r)
	}
	del := func(r *cnitypes.PodRequest) (*cni100.Result, error) {
		return d.cniCmdNfDelHandler(r)
	}

	d.cniserver = cniserver.NewCNIServer(add, del, cniserver.WithPathManager(d.pathManager))

	return lis, err
}

func (d *DpuSideManager) ListenAndServe(ctx context.Context) error {
	listener, err := d.Listen()

	if err != nil {
		return fmt.Errorf("ListenAndServe failed with error: %v", err)
	}
	return d.Serve(ctx, listener)
}

func (d *DpuSideManager) Serve(ctx context.Context, listener net.Listener) error {
	d.log.Info("Serve")
	var wg sync.WaitGroup
	done := make(chan error, 4)

	go func() {
		<-ctx.Done()
		d.log.Info("Context cancelled, shutting down servers")
		d.server.Stop()
		d.dp.Stop()
		d.vsp.Close()
		d.cniserver.ShutdownAndWait()
		listener.Close()
	}()

	wg.Add(1)
	go func() {
		d.log.Info("Starting OPI server")
		if err := d.server.Serve(listener); err != nil {
			done <- fmt.Errorf("Error from OPI server: %v", err)
		} else {
			done <- nil
		}
		d.log.Info("Stopped OPI server")
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		d.log.Info("Starting Device Plugin server")
		if err := d.dp.ListenAndServe(); err != nil {
			done <- fmt.Errorf("Error from Device Plugin server: %v", err)
		} else {
			done <- nil
		}
		d.log.Info("Stopped Device Plugin server")
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		d.log.Info("Starting CNI server")
		if err := d.cniserver.ListenAndServe(); err != nil {
			done <- fmt.Errorf("Error from CNI server: %v", err)
		} else {
			done <- nil
		}
		d.log.Info("Stopped CNI server")
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		d.log.Info("Starting manager")
		if err := d.manager.Start(ctx); err != nil {
			done <- fmt.Errorf("Error from manager: %v", err)
		} else {
			done <- nil
		}
		d.log.Info("Stopped manager")
		wg.Done()
	}()

	// Block on any go routines writing to the done channel when an error occurs or they
	// are forced to exit, or context cancellation
	select {
	case err := <-done:
		if err != nil {
			d.log.Error(err, "one of the go-routines failed")
		}
		wg.Wait()
		d.startedWg.Done()
		return err
	case <-ctx.Done():
		wg.Wait()
		d.startedWg.Done()
		return ctx.Err()
	}
}

func (d *DpuSideManager) setupReconcilers() {
	d.log.Info("DpuSideManager.setupReconcilers() starting")
	if d.manager == nil {
		t := time.Duration(0)

		mgr, err := ctrl.NewManager(d.config, ctrl.Options{
			Scheme: scheme.Scheme,
			NewCache: func(config *rest.Config, opts cache.Options) (cache.Cache, error) {
				// Watch ServiceFunctionChains only in operator namespace, but Pods in both namespaces
				opts.DefaultNamespaces = map[string]cache.Config{
					vars.Namespace: {},
				}
				opts.ByObject = map[client.Object]cache.ByObject{
					&corev1.Pod{}: {
						Namespaces: map[string]cache.Config{
							vars.Namespace: {},
							"default":      {},
						},
					},
				}
				return cache.New(config, opts)
			},
			// A timeout needs to be specified, or else the manager will wait indefinitely on stop()
			GracefulShutdownTimeout: &t,
			Metrics: server.Options{
				BindAddress:    ":18001",
				SecureServing:  true,
				FilterProvider: filters.WithAuthenticationAndAuthorization,
			},
		})
		if err != nil {
			d.log.Error(err, "unable to start manager")
		}

		sfcReconciler := sfcreconciler.NewSfcReconciler(mgr.GetClient(), mgr.GetScheme())

		if err = sfcReconciler.SetupWithManager(mgr); err != nil {
			d.log.Error(err, "unable to create controller", "controller", "ServiceFunctionChain")
		}
		d.manager = mgr
	}
	d.log.Info("DpuSideManager.setupReconcilers() Done")
}
