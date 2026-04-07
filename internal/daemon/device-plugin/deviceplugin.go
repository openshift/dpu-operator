package deviceplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	dh "github.com/openshift/dpu-operator/internal/daemon/device-handler"
	dpudevicehandler "github.com/openshift/dpu-operator/internal/daemon/device-handler/dpu-device-handler"
	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"github.com/openshift/dpu-operator/internal/utils"
	"github.com/openshift/dpu-operator/pkgs/vars"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultDpuResourceName = "openshift.io/dpu"
	AcceleratedResourceName = "openshift.io/dpu-accelerated"

	acceleratedDevicePrefix = "accelerated:"

	dpuDevicePluginConfigMapName = "dpu-device-plugin-config"
	dpuDevicePluginConfigKey     = "config.json"
	configMapPollInterval        = 30 * time.Second
)

// ConfigMap model
type devicePluginConfig struct {
	Resources []devicePluginResource `json:"resources"`
}

type devicePluginResource struct {
	ResourceName   string   `json:"resourceName"`
	DpuNetworkName string   `json:"dpuNetworkName"`
	VfRanges       []string `json:"vfRanges,omitempty"`
	IsAccelerated  bool     `json:"isAccelerated,omitempty"`
}

// DevicePlugin interface — used by host/dpu side managers

type DevicePlugin interface {
	SetupDevices() error
	ListenAndServe() error
	Serve(lis net.Listener) error
	Listen() (net.Listener, error)
	Stop() error
}

// dpServer individual device plugin gRPC server
type dpServer struct {
	devicesMu  sync.RWMutex
	devices    map[string]pluginapi.Device
	grpcServer *grpc.Server
	pluginapi.DevicePluginServer
	log           logr.Logger
	pathManager   utils.PathManager
	deviceHandler dh.DeviceHandler
	startedWg     sync.WaitGroup
	resourceName  string
	drainCh       chan struct{}
	drainDone     chan struct{}
}

func (dp *dpServer) sendDevices(stream pluginapi.DevicePlugin_ListAndWatchServer, devices *dh.DeviceList) error {
	resp := new(pluginapi.ListAndWatchResponse)
	for _, dev := range *devices {
		resp.Devices = append(resp.Devices, &dev)
	}

	dp.log.Info("SendDevices", "resp", resp)
	if err := stream.Send(resp); err != nil {
		dp.log.Error(err, "Cannot send devices to ListAndWatch server")
		if dp.grpcServer != nil {
			dp.grpcServer.Stop()
		}
		return err
	}
	return nil
}

func (dp *dpServer) devicesEqual(d1, d2 *dh.DeviceList) bool {
	if len(*d1) != len(*d2) {
		return false
	}

	for d1key, d1value := range *d1 {
		if d2value, ok := (*d2)[d1key]; !ok || !reflect.DeepEqual(d1value, d2value) {
			return false
		}
	}

	return true
}

func (dp *dpServer) setDeviceCache(devices *dh.DeviceList) {
	dp.devicesMu.Lock()
	defer dp.devicesMu.Unlock()
	dp.devices = *devices
	for id, dev := range dp.devices {
		dp.log.Info("Cached device", "id", id, "dev.ID", dev.ID)
	}
}

func (dp *dpServer) checkCachedDeviceHealth(id string) (bool, error) {
	dp.devicesMu.RLock()
	defer dp.devicesMu.RUnlock()
	dev, ok := dp.devices[id]
	if !ok {
		return false, fmt.Errorf("invalid allocation request with non-existing device: %s", id)
	}
	return dev.Health == pluginapi.Healthy, nil
}

func (dp *dpServer) ListAndWatch(empty *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	oldDevices := make(dh.DeviceList)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	first := true
	for {
		if !first {
			select {
			case <-dp.drainCh:
				dp.log.Info("Drain requested; sending empty device list to kubelet", "resourceName", dp.resourceName)
				emptyList := make(dh.DeviceList)
				_ = dp.sendDevices(stream, &emptyList)
				// Keep the stream open briefly so kubelet can process the
				// empty device list before the gRPC connection is torn down.
				time.Sleep(2 * time.Second)
				close(dp.drainDone)
				return nil
			case <-ticker.C:
			}
		}
		first = false

		newDevices, err := dp.deviceHandler.GetDevices()
		if err != nil {
			dp.log.Error(err, "Failed to get Devices")
			return err
		}
		if !dp.devicesEqual(&oldDevices, newDevices) {
			err := dp.sendDevices(stream, newDevices)
			if err != nil {
				dp.log.Error(err, "Failed to send Devices")
				return err
			}
			oldDevices = *newDevices
			dp.setDeviceCache(newDevices)
		}
	}
}

// Allocate passes the dev name as an env variable to the requesting container
func (dp *dpServer) Allocate(ctx context.Context, rqt *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	resp := new(pluginapi.AllocateResponse)
	for _, container := range rqt.ContainerRequests {
		containerResp := new(pluginapi.ContainerAllocateResponse)
		devName := ""
		for _, id := range container.DevicesIDs {
			dp.log.Info("DeviceID in Allocate", "id", id)
			isHealthy, err := dp.checkCachedDeviceHealth(id)
			if err != nil {
				return nil, err
			}
			dp.log.Info("DeviceID Health", "id", id, "isHealthy", isHealthy, "err", err)

			if !isHealthy {
				return nil, fmt.Errorf("invalid allocation request with unhealthy device: %s", id)
			}

			devName = devName + id + ","
		}

		dp.log.Info("Device(s) allocated", "devName", devName)
		envmap := make(map[string]string)
		envmap["NF-DEV"] = devName

		containerResp.Envs = envmap
		resp.ContainerResponses = append(resp.ContainerResponses, containerResp)
	}
	return resp, nil
}

func (dp *dpServer) Listen() (net.Listener, error) {
	pluginEndpoint := dp.pathManager.PluginEndpoint()

	err := dp.cleanup()
	if err != nil {
		return nil, fmt.Errorf("failed to cleanup Device Plugin server endpoint: %v", err)
	}

	dp.log.Info("Starting Device Plugin server at", "pluginEndpoint", pluginEndpoint)
	lis, err := net.Listen("unix", pluginEndpoint)
	if err != nil {
		return nil, fmt.Errorf("resource %s failed to listen to Device Plugin server: %v", dp.resourceName, err)
	}

	pluginapi.RegisterDevicePluginServer(dp.grpcServer, dp)

	dp.startedWg.Add(1)
	return lis, nil
}

func (dp *dpServer) Serve(lis net.Listener) error {
	defer dp.startedWg.Done()
	// EXCEPTIONAL CODE!!! (DO NOT COPY): The issue is that Kubelet was written
	// in a way that uses deprecated gRPC DialOptions specifically "WithBlock".
	// This means that the gRPC Register() function blocks until the device plugin
	// starts serving.
	// References:
	// 	kubernetes/pkg/kubelet/cm/devicemanager/plugin/v1beta1/server.go (Register() func)
	// 	kubernetes/pkg/kubelet/cm/devicemanager/plugin/v1beta1/client.go (dial() func)
	//
	// Therefore we have the following workaround to make sure we start serving which includes trying
	// to connect to ourselves in "ensureDevicePluginServerStarted" before registering with Kubelet.
	done := make(chan error, 1)
	var err error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err = dp.grpcServer.Serve(lis)
		done <- err
		wg.Done()
	}()

	err = dp.ensureDevicePluginServerStarted()
	if err != nil {
		return fmt.Errorf("failed to ensure Device Plugin server started: %v", err)
	}

	err = dp.registerWithKubelet()
	if err != nil {
		return fmt.Errorf("failed to register the Device Plugin server with Kubelet: %v", err)
	}

	err = <-done
	// The "serve" design paradigm must be a blocking call. Thus we wait here.
	wg.Wait()

	if err != nil {
		return fmt.Errorf("serving Device Plugin incoming requests failed: %v", err)
	}
	return nil
}

func (dp *dpServer) SetupDevices() error {
	dp.log.Info("Device Plugin server is setting up devices...")
	if err := dp.deviceHandler.SetupDevices(); err != nil {
		return fmt.Errorf("failed to setup devices: %v", err)
	}
	return nil
}

func (dp *dpServer) ListenAndServe() error {
	listener, err := dp.Listen()
	if err != nil {
		dp.log.Error(err, "failed to listen on the Device Plugin server.")
		return err
	}

	dp.log.Info("Device Plugin server is now serving requests.")
	if err := dp.Serve(listener); err != nil {
		dp.log.Error(err, "Device Plugin server Serve() failed.")
		return err
	}
	return nil
}

func (dp *dpServer) ensureDevicePluginServerStarted() error {
	pluginEndpoint := dp.pathManager.PluginEndpoint()
	conn, err := dp.connectWithRetry("unix:" + pluginEndpoint)
	if err != nil {
		return fmt.Errorf("resource %s unable to establish test connection with gRPC server: %v", dp.resourceName, err)
	}
	dp.log.Info("Device plugin endpoint started serving", "resourceName", dp.resourceName)
	conn.Close()
	return nil
}

func (dp *dpServer) registerWithKubelet() error {
	kubeletEndpoint := filepath.Join("unix:", dp.pathManager.KubeletEndPoint())
	conn, err := grpc.Dial(kubeletEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("resource %s unable connect to Kubelet: %v", dp.resourceName, err)
	}
	defer conn.Close()

	client := pluginapi.NewRegistrationClient(conn)

	request := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     dp.pathManager.PluginEndpointFilename(),
		ResourceName: dp.resourceName,
	}

	if _, err = client.Register(context.Background(), request); err != nil {
		return fmt.Errorf("unable to register resource %s with Kubelet: %v", dp.resourceName, err)
	}
	dp.log.Info("Device plugin registered with Kubelet", "resourceName", dp.resourceName)

	return nil
}

func (dp *dpServer) connectWithRetry(endpoint string) (*grpc.ClientConn, error) {
	retryPolicy := `{
		"methodConfig": [{
		  "waitForReady": true,
		  "retryPolicy": {
			  "MaxAttempts": 40,
			  "InitialBackoff": "1s",
			  "MaxBackoff": "16s",
			  "BackoffMultiplier": 2.0,
			  "RetryableStatusCodes": [ "UNAVAILABLE" ]
		  }
		}]}`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(
		ctx,
		endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithDefaultServiceConfig(retryPolicy),
	)
	if err != nil {
		dp.log.Error(err, "Failed to establish connection with retry", "endpoint", endpoint)
		return nil, err
	}

	return conn, nil
}

func (dp *dpServer) Stop() error {
	dp.log.Info("Stopping Device Plugin...", "resourceName", dp.resourceName)
	if dp.grpcServer == nil {
		return nil
	}

	select {
	case <-dp.drainCh:
		// already drained
	default:
		close(dp.drainCh)
		select {
		case <-dp.drainDone:
			dp.log.Info("Drain complete; kubelet received empty device list", "resourceName", dp.resourceName)
		case <-time.After(1 * time.Second):
			dp.log.Info("Drain timed out; stopping gRPC server anyway", "resourceName", dp.resourceName)
		}
	}

	dp.grpcServer.Stop()
	dp.startedWg.Wait()
	dp.grpcServer = nil

	return dp.cleanup()
}

func (dp *dpServer) cleanup() error {
	pluginEndpoint := dp.pathManager.PluginEndpoint()
	if err := os.Remove(pluginEndpoint); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

func (dp *dpServer) PreStartContainer(ctx context.Context, psRqt *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

func (dp *dpServer) GetDevicePluginOptions(ctx context.Context, empty *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{
		PreStartRequired: false,
	}, nil
}

// filteredDeviceHandler — wraps a real DeviceHandler and filters its output.
// Two modes controlled by acceleratedOnly:
//   - acceleratedOnly=false: returns VF devices (excluding accelerated:-prefixed
//     ones) filtered by positional index from allowedVFs.
//   - acceleratedOnly=true:  returns only accelerated:-prefixed devices
//     (with the prefix stripped so the real interface name is advertised).
type filteredDeviceHandler struct {
	inner           dh.DeviceHandler
	allowedVFs      map[int32]struct{}
	acceleratedOnly bool
	dpuMode         bool
}

func newFilteredDeviceHandler(inner dh.DeviceHandler, vfIDs []int32, dpuMode bool) *filteredDeviceHandler {
	allowed := make(map[int32]struct{}, len(vfIDs))
	for _, id := range vfIDs {
		allowed[id] = struct{}{}
	}
	return &filteredDeviceHandler{inner: inner, allowedVFs: allowed, dpuMode: dpuMode}
}

func newAcceleratedDeviceHandler(inner dh.DeviceHandler) *filteredDeviceHandler {
	return &filteredDeviceHandler{inner: inner, acceleratedOnly: true}
}

func (h *filteredDeviceHandler) SetupDevices() error {
	return nil
}

func (h *filteredDeviceHandler) GetDevices() (*dh.DeviceList, error) {
	all, err := h.inner.GetDevices()
	if err != nil {
		return nil, err
	}

	filtered := make(dh.DeviceList)

	if h.acceleratedOnly {
		for id, dev := range *all {
			if strings.HasPrefix(id, acceleratedDevicePrefix) {
				realName := strings.TrimPrefix(id, acceleratedDevicePrefix)
				dev.ID = realName
				filtered[realName] = dev
			}
		}
		return &filtered, nil
	}

	// VF mode: collect non-accelerated devices and try stable VF-index mapping.
	var nonAccel []string
	for id := range *all {
		if !strings.HasPrefix(id, acceleratedDevicePrefix) {
			nonAccel = append(nonAccel, id)
		}
	}
	sort.Strings(nonAccel)

	// Try suffix-based mapping first (stable when VFs move into pod namespaces).
	// If no device names have a recognisable VF suffix, fall back to positional.
	useSuffix := false
	for _, id := range nonAccel {
		if extractVFIndex(id) >= 0 {
			useSuffix = true
			break
		}
	}

	if useSuffix {
		for _, id := range nonAccel {
			vfID := extractVFIndex(id)
			if vfID < 0 {
				continue
			}
			if _, ok := h.allowedVFs[int32(vfID)]; ok {
				filtered[id] = (*all)[id]
			}
		}
	} else {
		for idx, id := range nonAccel {
			if _, ok := h.allowedVFs[int32(idx)]; ok {
				filtered[id] = (*all)[id]
			}
		}
	}
	return &filtered, nil
}

// extractVFIndex parses a VF interface name like "enP2p1s0v5" and returns a
// 0-based VF index (suffix - 1, because v0 is the management interface).
// Returns -1 if the name doesn't match the expected pattern.
func extractVFIndex(name string) int {
	idx := strings.LastIndex(name, "v")
	if idx < 0 || idx == len(name)-1 {
		return -1
	}
	n, err := strconv.Atoi(name[idx+1:])
	if err != nil || n <= 0 {
		return -1
	}
	return n - 1
}

// VF range helpers
func expandAllRanges(vfRanges []string) []int32 {
	set := map[int32]struct{}{}
	for _, r := range vfRanges {
		for _, id := range expandRange(r) {
			set[id] = struct{}{}
		}
	}
	out := make([]int32, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func expandRange(s string) []int32 {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "-") {
		v, err := strconv.Atoi(s)
		if err != nil {
			return nil
		}
		return []int32{int32(v)}
	}
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return nil
	}
	start, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	end, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return nil
	}
	if end < start {
		start, end = end, start
	}
	out := make([]int32, 0, end-start+1)
	for i := start; i <= end; i++ {
		out = append(out, int32(i))
	}
	return out
}

// DevicePluginManager
// When no ConfigMap exists (or it has zero resources):
//   - Runs the default openshift.io/dpu plugin using VSP-discovered devices.
//
// When the ConfigMap has resources:
//   - Does NOT start the default plugin.
//   - Starts one dpServer per ConfigMap resource entry, each with its own
//     Unix socket, resource name, and filtered view of real VSP devices.
//   - On DPU side with isAccelerated=true, also starts an accelerated device
//     plugin for the accelerated:-prefixed devices from the VSP.
//   - Polls the ConfigMap every 30s and adds/removes plugins as needed.

type DevicePluginManager struct {
	log         logr.Logger
	k8sClient   client.Client
	vsp         plugin.VendorPlugin
	dpuMode     bool
	pathManager utils.PathManager

	defaultPlugin        *dpServer
	networkPlugins       map[string]*dpServer
	networkConfigs       map[string]devicePluginResource
	acceleratedPlugin    *dpServer
	lastAcceleratedMode  *bool
	mu                   sync.Mutex
	cancelWatch          context.CancelFunc
	defaultPluginStarted bool
}

func NewDevicePluginManager(vsp plugin.VendorPlugin, dpuMode bool, pm utils.PathManager, k8sClient client.Client) *DevicePluginManager {
	defaultDH := dpudevicehandler.NewDpuDeviceHandler(vsp, dpudevicehandler.WithDpuMode(dpuMode), dpudevicehandler.WithPathManager(pm))
	defaultPlugin := &dpServer{
		devices:       make(map[string]pluginapi.Device),
		grpcServer:    grpc.NewServer(),
		log:           ctrl.Log.WithName("DevicePlugin"),
		pathManager:   pm,
		deviceHandler: defaultDH,
		resourceName:  DefaultDpuResourceName,
		drainCh:       make(chan struct{}),
		drainDone:     make(chan struct{}),
	}

	return &DevicePluginManager{
		log:            ctrl.Log.WithName("DevicePluginManager"),
		k8sClient:      k8sClient,
		vsp:            vsp,
		dpuMode:        dpuMode,
		pathManager:    pm,
		defaultPlugin:  defaultPlugin,
		networkPlugins: make(map[string]*dpServer),
		networkConfigs: make(map[string]devicePluginResource),
	}
}

func (m *DevicePluginManager) SetupDevices() error {
	return m.defaultPlugin.SetupDevices()
}

func (m *DevicePluginManager) Listen() (net.Listener, error) {
	return m.defaultPlugin.Listen()
}

func (m *DevicePluginManager) Serve(lis net.Listener) error {
	watchCtx, cancel := context.WithCancel(context.Background())
	m.cancelWatch = cancel

	cfg := m.readConfig()
	hasConfigResources := cfg != nil && len(cfg.Resources) > 0

	if hasConfigResources {
		m.log.Info("ConfigMap has resources; starting per-network plugins only (no default plugin)")
		m.syncNetworkPlugins()
		go m.watchConfigMap(watchCtx)
		<-watchCtx.Done()
		return nil
	}

	m.log.Info("No ConfigMap resources found; starting default plugin", "resource", DefaultDpuResourceName)
	m.mu.Lock()
	m.defaultPluginStarted = true
	m.mu.Unlock()
	go m.watchConfigMap(watchCtx)
	return m.defaultPlugin.Serve(lis)
}

func (m *DevicePluginManager) ListenAndServe() error {
	lis, err := m.Listen()
	if err != nil {
		return err
	}
	return m.Serve(lis)
}

func (m *DevicePluginManager) Stop() error {
	if m.cancelWatch != nil {
		m.cancelWatch()
	}

	m.mu.Lock()
	plugins := make([]*dpServer, 0, len(m.networkPlugins))
	for _, dp := range m.networkPlugins {
		plugins = append(plugins, dp)
	}
	m.networkPlugins = make(map[string]*dpServer)
	m.networkConfigs = make(map[string]devicePluginResource)

	accelPlugin := m.acceleratedPlugin
	m.acceleratedPlugin = nil

	stopDefault := m.defaultPluginStarted
	m.defaultPluginStarted = false
	m.mu.Unlock()

	for _, dp := range plugins {
		if err := dp.Stop(); err != nil {
			m.log.Error(err, "Failed to stop per-network device plugin", "resource", dp.resourceName)
		}
	}
	if accelPlugin != nil {
		m.log.Info("Stopping accelerated device plugin")
		if err := accelPlugin.Stop(); err != nil {
			m.log.Error(err, "Failed to stop accelerated device plugin")
		}
	}
	if stopDefault {
		return m.defaultPlugin.Stop()
	}
	return nil
}

func (m *DevicePluginManager) watchConfigMap(ctx context.Context) {
	ticker := time.NewTicker(configMapPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.syncNetworkPlugins()
		}
	}
}

func (m *DevicePluginManager) readConfig() *devicePluginConfig {
	if m.k8sClient == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cm := &corev1.ConfigMap{}
	key := types.NamespacedName{Name: dpuDevicePluginConfigMapName, Namespace: vars.Namespace}
	if err := m.k8sClient.Get(ctx, key, cm); err != nil {
		if !apierrors.IsNotFound(err) {
			m.log.Error(err, "Failed to read device plugin ConfigMap")
		}
		return nil
	}

	raw := ""
	if cm.Data != nil {
		raw = cm.Data[dpuDevicePluginConfigKey]
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	var cfg devicePluginConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		m.log.Error(err, "Failed to parse device plugin config.json")
		return nil
	}
	return &cfg
}

// syncNetworkPlugins Entry point for reconciling per-network plugins
func (m *DevicePluginManager) syncNetworkPlugins() {
	cfg := m.readConfig()

	toStop, stopDefault := m.reconcilePlugins(cfg)

	for _, dp := range toStop {
		if err := dp.Stop(); err != nil {
			m.log.Error(err, "Failed to stop per-network device plugin", "resource", dp.resourceName)
		}
	}
	if stopDefault {
		if err := m.defaultPlugin.Stop(); err != nil {
			m.log.Error(err, "Failed to stop default plugin")
		}
	}
}

// reconcilePlugins holds the lock, updates maps, starts new plugins, and
// returns the list of plugins that need to be stopped (outside the lock).
func (m *DevicePluginManager) reconcilePlugins(cfg *devicePluginConfig) (toStop []*dpServer, stopDefault bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	desired := make(map[string]devicePluginResource)
	if cfg != nil {
		for _, r := range cfg.Resources {
			if r.DpuNetworkName != "" && r.ResourceName != "" {
				desired[r.DpuNetworkName] = r
			}
		}
	}

	// Remove plugins for networks no longer in the ConfigMap, or whose
	// config has changed (vfRanges, isAccelerated). Changed plugins will
	// be re-created in the loop below.
	for name, dp := range m.networkPlugins {
		newRes, stillDesired := desired[name]
		if !stillDesired || m.configChanged(name, newRes) {
			m.log.Info("Removing per-network device plugin", "network", name, "resource", dp.resourceName, "reason", m.removeReason(stillDesired))
			toStop = append(toStop, dp)
			delete(m.networkPlugins, name)
			delete(m.networkConfigs, name)
		}
	}

	// Start plugins for new networks (including ones just removed due to config change).
	pluginsStarted := len(m.networkPlugins) > 0
	needsAccelerated := false

	for name, res := range desired {
		// On DPU side, non-accelerated networks are handled by the default plugin.
		if m.dpuMode && !res.IsAccelerated {
			continue
		}

		if res.IsAccelerated {
			needsAccelerated = true
		}

		if _, exists := m.networkPlugins[name]; exists {
			continue
		}

		if started := m.startPlugin(name, res); started {
			pluginsStarted = true
		}
	}

	// DPU-only: manage accelerated device plugin and VSP mode.
	if m.dpuMode {
		m.syncAcceleratedPlugin(needsAccelerated)
		if needsAccelerated {
			pluginsStarted = true
		}
	}

	if pluginsStarted && m.defaultPluginStarted {
		m.log.Info("Per-network plugins running; stopping default plugin")
		stopDefault = true
		m.defaultPluginStarted = false
	}

	if !pluginsStarted && !m.defaultPluginStarted {
		m.log.Info("No per-network plugins active; restarting default plugin", "resource", DefaultDpuResourceName)
		m.defaultPluginStarted = true
		m.defaultPlugin.grpcServer = grpc.NewServer()
		m.defaultPlugin.drainCh = make(chan struct{})
		m.defaultPlugin.drainDone = make(chan struct{})
		go func() {
			lis, err := m.defaultPlugin.Listen()
			if err != nil {
				m.log.Error(err, "Failed to listen for default plugin restart")
				m.mu.Lock()
				m.defaultPluginStarted = false
				m.mu.Unlock()
				return
			}
			if err := m.defaultPlugin.Serve(lis); err != nil {
				m.log.Error(err, "Default plugin serve failed after restart")
				m.mu.Lock()
				m.defaultPluginStarted = false
				m.mu.Unlock()
			}
		}()
	}

	return toStop, stopDefault
}

// startPlugin creates and starts a per-network device plugin for the given
// DpuNetwork resource. Returns true if the plugin was started.
func (m *DevicePluginManager) startPlugin(name string, res devicePluginResource) bool {
	vfIDs := expandAllRanges(res.VfRanges)
	if len(vfIDs) == 0 {
		m.log.Info("Skipping per-network device plugin with no VF IDs", "network", name)
		return false
	}

	networkPM := m.pathManager.PathManagerFor(name)
	dp := &dpServer{
		devices:       make(map[string]pluginapi.Device),
		grpcServer:    grpc.NewServer(),
		log:           ctrl.Log.WithName("DevicePlugin").WithValues("network", name),
		pathManager:   networkPM,
		deviceHandler: newFilteredDeviceHandler(m.defaultPlugin.deviceHandler, vfIDs, m.dpuMode),
		resourceName:  res.ResourceName,
		drainCh:       make(chan struct{}),
		drainDone:     make(chan struct{}),
	}

	m.networkPlugins[name] = dp
	m.networkConfigs[name] = res
	m.log.Info("Starting per-network device plugin", "network", name, "resource", res.ResourceName, "vfIDs", vfIDs, "endpoint", networkPM.PluginEndpoint())

	go func() {
		if err := dp.ListenAndServe(); err != nil {
			m.log.Error(err, "Per-network device plugin failed", "network", name)
		}
	}()

	return true
}

// syncAcceleratedPlugin starts or stops the accelerated device plugin and
// updates the VSP's accelerated mode accordingly.
func (m *DevicePluginManager) syncAcceleratedPlugin(needsAccelerated bool) {
	if m.lastAcceleratedMode == nil || *m.lastAcceleratedMode != needsAccelerated {
		if err := m.vsp.SetDpuNetworkConfig(needsAccelerated); err != nil {
			m.log.Error(err, "Failed to set accelerated mode on VSP; will retry next cycle", "isAccelerated", needsAccelerated)
		} else {
			m.lastAcceleratedMode = &needsAccelerated
		}
	}

	if needsAccelerated && m.acceleratedPlugin == nil {
		acceleratedPM := m.pathManager.PathManagerFor("accelerated")
		m.acceleratedPlugin = &dpServer{
			devices:       make(map[string]pluginapi.Device),
			grpcServer:    grpc.NewServer(),
			log:           ctrl.Log.WithName("DevicePlugin").WithValues("resource", AcceleratedResourceName),
			pathManager:   acceleratedPM,
			deviceHandler: newAcceleratedDeviceHandler(m.defaultPlugin.deviceHandler),
			resourceName:  AcceleratedResourceName,
			drainCh:       make(chan struct{}),
			drainDone:     make(chan struct{}),
		}
		m.log.Info("Starting accelerated device plugin", "resource", AcceleratedResourceName, "endpoint", acceleratedPM.PluginEndpoint())

		go func(dp *dpServer) {
			if err := dp.ListenAndServe(); err != nil {
				m.log.Error(err, "Accelerated device plugin failed")
			}
		}(m.acceleratedPlugin)
	}

	if !needsAccelerated && m.acceleratedPlugin != nil {
		m.log.Info("No accelerated networks; stopping accelerated device plugin")
		m.acceleratedPlugin.Stop()
		m.acceleratedPlugin = nil
	}
}

func (m *DevicePluginManager) configChanged(name string, newRes devicePluginResource) bool {
	oldRes, ok := m.networkConfigs[name]
	if !ok {
		return true
	}
	return oldRes.ResourceName != newRes.ResourceName ||
		oldRes.IsAccelerated != newRes.IsAccelerated ||
		!vfRangesEqual(oldRes.VfRanges, newRes.VfRanges)
}

func (m *DevicePluginManager) removeReason(stillDesired bool) string {
	if !stillDesired {
		return "removed from ConfigMap"
	}
	return "config changed"
}

func vfRangesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

