package cniserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	cni100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/gorilla/mux"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnihelper"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/sriov"
	sriovtypes "github.com/openshift/dpu-operator/dpu-cni/pkgs/sriovtypes"
	"k8s.io/klog/v2"
)

// This server implementations is only temporary for testing when the DPU Daemon
// code has not been implemented.

type processRequestFunc func(request *cnitypes.Request) (*cni100.Result, error)
type Server struct {
	http.Server
	processRequest processRequestFunc
	runDir string
	socketPath string
}

// ensureRunDirExists makes sure that the socket being created is only accessible to root.
func ensureRunDirExists(runDir string, socketPath string) error {
	// Remove and re-create the socket directory with root-only permissions
	if err := os.RemoveAll(runDir); err != nil && !os.IsNotExist(err) {
		info, err := os.Stat(runDir)
		if err != nil {
			return fmt.Errorf("failed to stat old pod info socket directory %s: %v", runDir, err)
		}
		// Owner must be root
		tmp := info.Sys()
		statt, ok := tmp.(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("failed to read pod info socket directory stat info: %T", tmp)
		}
		if statt.Uid != 0 {
			return fmt.Errorf("insecure owner of pod info socket directory %s: %v", runDir, statt.Uid)
		}

		// Check permissions
		if info.Mode()&0o777 != 0o700 {
			return fmt.Errorf("insecure permissions on pod info socket directory %s: %v", runDir, info.Mode())
		}

		// Finally remove the socket file so we can re-create it
		if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove old pod info socket %s: %v", socketPath, err)
		}
	}
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return fmt.Errorf("failed to create pod info socket directory %s: %v", runDir, err)
	}
	return nil
}

// getListener creates a listener to a unix socket located in `socketPath`
func getListener(runDir string, serverSocketPath string) (net.Listener, error) {
	err := ensureRunDirExists(runDir, serverSocketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create run directory for DPU CNI socket: %v", err)
	}
	listener, err := net.Listen("unix", serverSocketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on DPU CNI socket: %v", err)
	}
	if err := os.Chmod(serverSocketPath, 0o600); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("failed to set file permissions on DPU CNI socket: %v", err)
	}
	return listener, nil
}

func processRequest(request *cnitypes.Request) (*cni100.Result, error) {
	// FIXME: Do actual work here.
	klog.Infof("DEBUG: %v", request)

	req, err := cniRequestToPodRequest(request)
	if err != nil {
		return nil, err
	}
	defer req.Cancel()

	var res *cni100.Result = nil
	sm := sriov.NewSriovManager()
	if req.Command == cnitypes.CNIAdd {
		res, err = sm.CmdAdd(req)
	} else if req.Command == cnitypes.CNIDel {
		err = sm.CmdDel(req)
	}
	if err != nil {
		return nil, err
	}

	return res, nil
}

// Split the "CNI_ARGS" environment variable's value into a map.  CNI_ARGS
// contains arbitrary key/value pairs separated by ';' and is for runtime or
// plugin specific uses.  Kubernetes passes the pod namespace and name in
// CNI_ARGS.
func gatherCNIArgs(env map[string]string) (map[string]string, error) {
	cniArgs, ok := env["CNI_ARGS"]
	if !ok {
		return nil, fmt.Errorf("missing CNI_ARGS: '%s'", env)
	}

	mapArgs := make(map[string]string)
	for _, arg := range strings.Split(cniArgs, ";") {
		parts := strings.Split(arg, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid CNI_ARG '%s'", arg)
		}
		mapArgs[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return mapArgs, nil
}

func cniRequestToPodRequest(cr *cnitypes.Request) (*cnitypes.PodRequest, error) {
	cmd, ok := cr.Env["CNI_COMMAND"]
	if !ok {
		return nil, fmt.Errorf("missing CNI_COMMAND")
	}

	req := &cnitypes.PodRequest{
		Command: cmd,
	}

	req.SandboxID, ok = cr.Env["CNI_CONTAINERID"]
	if !ok {
		return nil, fmt.Errorf("missing CNI_CONTAINERID")
	}

	req.Netns, ok = cr.Env["CNI_NETNS"]
	if !ok {
		return nil, fmt.Errorf("missing CNI_NETNS")
	}

	req.IfName, ok = cr.Env["CNI_IFNAME"]
	if !ok {
		req.IfName = "eth0"
	}

	cniArgs, err := gatherCNIArgs(cr.Env)
	if err != nil {
		return nil, err
	}

	req.PodNamespace, ok = cniArgs["K8S_POD_NAMESPACE"]
	if !ok {
		return nil, fmt.Errorf("missing K8S_POD_NAMESPACE")
	}
	req.PodName, ok = cniArgs["K8S_POD_NAME"]
	if !ok {
		return nil, fmt.Errorf("missing K8S_POD_NAME")
	}

	// UID may not be passed by all runtimes yet. Will be passed
	// by CRIO 1.20+ and containerd 1.5+ soon.
	// CRIO 1.20: https://github.com/cri-o/cri-o/pull/5029
	// CRIO 1.21: https://github.com/cri-o/cri-o/pull/5028
	// CRIO 1.22: https://github.com/cri-o/cri-o/pull/5026
	// containerd 1.6: https://github.com/containerd/containerd/pull/5640
	// containerd 1.5: https://github.com/containerd/containerd/pull/5643
	req.PodUID = cniArgs["K8S_POD_UID"]

	conf, err := cnihelper.ReadCNIConfig(cr.Config)
	if err != nil {
		return nil, fmt.Errorf("broken stdin args")
	}

	req.NetName = conf.Name

	if conf.DeviceID != "" {
		if sriovtypes.IsPCIDeviceName(conf.DeviceID) {
			// DeviceID is a PCI address
		} else if sriovtypes.IsAuxDeviceName(conf.DeviceID) {
			// DeviceID is an Auxiliary device name - <driver_name>.<kind_of_a_type>.<id>
			chunks := strings.Split(conf.DeviceID, ".")
			if chunks[1] != "sf" {
				return nil, fmt.Errorf("only SF auxiliary devices are supported")
			}
		} else {
			return nil, fmt.Errorf("expected PCI or Auxiliary device name, got - %s", conf.DeviceID)
		}
	}

	req.CNIConf = conf
	req.DeviceInfo = cr.DeviceInfo
	req.Timestamp = time.Now()
	// Match the Kubelet default CRI operation timeout of 2m
	req.Ctx, req.Cancel = context.WithTimeout(context.Background(), 2*time.Minute)

	fmt.Printf("%+v\n", req)
	return req, nil
}

// handleCNIRequest will take the CNI request and delegate (TODO) work.
func (s *Server) handleCNIRequest(r *http.Request) ([]byte, error) {
	var cniRq cnitypes.Request
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &cniRq); err != nil {
		return nil, err
	}

	result, err := s.processRequest(&cniRq)
	if err != nil {
		return nil, err
	}

	response := &cnitypes.Response{}
	response.Result = result

	return json.Marshal(&response)
}

// HttpCNIPost is a callback functions to handle "/cni" requests.
func (s *Server) HttpCNIPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, err := s.handleCNIRequest(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("%v", err), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)

	// Empty response JSON means success with no body
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(result); err != nil {
		klog.Errorf("Error writing HTTP response: %v", err)
	}
}

// Start starts the server and begins serving on the given listener
func (s *Server) Start() error {
	klog.Infof("Starting DPU CNI Server")
	listener, err := getListener(s.runDir, s.socketPath)
	if err != nil {
		klog.Errorf("Failed to start the CNI server using socket %s. Reason: %+v", cnitypes.ServerSocketPath, err)
	}

	klog.Infof("DPU CNI Server is now serving requests.")
	if err := s.Serve(listener); err != nil {
		klog.Errorf("DPU CNI server Serve() failed: %v", err)
		return err
	}
	return nil
}

// NewCNIServer creates a new HTTP router instances to handle the CNI server requests.
func NewCNIServer(options ...func(*Server)) *Server {
	klog.Infof("DPU CNI Server creating new router.")
	router := mux.NewRouter()
	s := &Server{
		Server: http.Server{
			Handler: router,
		},
		processRequest: processRequest,
		runDir: cnitypes.ServerRunDir,
		socketPath: cnitypes.ServerSocketPath,
	}

	router.NotFoundHandler = http.HandlerFunc(http.NotFound)
	router.HandleFunc("/cni", http.HandlerFunc(s.HttpCNIPost))

	for _, o := range options {
		o(s)
	}

	return s
}

func WithHandler(rqFunc processRequestFunc) func(*Server) {
	return func(s *Server) {
		s.processRequest = rqFunc
	}
}

func WithSocketPath(runDir string, socketPath string) func(*Server) {
	return func(s *Server) {
		s.runDir = runDir
		s.socketPath = socketPath
	}
}
