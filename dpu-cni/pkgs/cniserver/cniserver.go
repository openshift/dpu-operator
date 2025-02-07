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
	"time"

	cni100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/gorilla/mux"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnihelper"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/sriov"
	"github.com/openshift/dpu-operator/internal/utils"
	"k8s.io/klog/v2"
)

// This server implementations is only temporary for testing when the DPU Daemon
// code has not been implemented.

type processRequestFunc func(request *cnitypes.PodRequest) (*cni100.Result, error)

type Server struct {
	http.Server
	cniCmdAddHandler processRequestFunc
	cniCmdDelHandler processRequestFunc
	pathManager      utils.PathManager
}

// Start starts the server and begins serving on the given listener
func (s *Server) ListenAndServe() error {
	klog.Infof("Starting DPU CNI Server")
	listener, err := s.Listen()
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

// Listen creates a listener to a unix socket located in `socketPath`
func (s *Server) Listen() (net.Listener, error) {
	err := s.pathManager.EnsureSocketDirExists(s.pathManager.CNIServerPath())
	if err != nil {
		return nil, fmt.Errorf("failed to create run directory for DPU CNI socket: %v", err)
	}
	listener, err := net.Listen("unix", s.pathManager.CNIServerPath())
	if err != nil {
		return nil, fmt.Errorf("failed to listen on DPU CNI socket: %v", err)
	}
	klog.Info("Listen on socket path: ", s.pathManager.CNIServerPath())
	if err := os.Chmod(s.pathManager.CNIServerPath(), 0o600); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("failed to set file permissions on DPU CNI socket: %v", err)
	}
	return listener, nil
}

func (s *Server) ShutdownAndWait() {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	s.Shutdown(ctx)
}

func processRequest(request *cnitypes.Request) (*cni100.Result, error) {
	// FIXME: Do actual work here.
	klog.Infof("DEBUG: %v", request)

	req, err := cniRequestToPodRequest(request)
	if err != nil {
		return nil, err
	}
	defer req.Cancel()
	defer cniRequestEnvCleanup()

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

// cniRequestSetEnv sets the CNI environment variables. This is needed when delegating IPAM plugins.
// Please see vendor/github.com/containernetworking/cni/pkg/invoke/delegate.go:delegateCommon()
func cniRequestSetEnv(req *cnitypes.PodRequest) {
	os.Setenv("CNI_COMMAND", req.Command)
	os.Setenv("CNI_CONTAINERID", req.ContainerId)
	os.Setenv("CNI_NETNS", req.Netns)
	os.Setenv("CNI_IFNAME", req.IfName)
	os.Setenv("CNI_PATH", req.Path)
}

// cniRequestEnvCleanup cleans up the CNI environment variables once delegating IPAM plugins is done.
func cniRequestEnvCleanup() {
	os.Unsetenv("CNI_COMMAND")
	os.Unsetenv("CNI_CONTAINERID")
	os.Unsetenv("CNI_NETNS")
	os.Unsetenv("CNI_IFNAME")
	os.Unsetenv("CNI_PATH")
}

// cniRequestToPodRequest
func cniRequestToPodRequest(cr *cnitypes.Request) (*cnitypes.PodRequest, error) {
	cmd, ok := cr.Env["CNI_COMMAND"]
	if !ok {
		return nil, fmt.Errorf("missing CNI_COMMAND")
	}

	req := &cnitypes.PodRequest{
		Command: cmd,
	}

	req.ContainerId, ok = cr.Env["CNI_CONTAINERID"]
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

	req.Path, ok = cr.Env["CNI_PATH"]
	if !ok {
		return nil, fmt.Errorf("missing CNI_PATH")
	}

	cniArgs, err := gatherCNIArgs(cr.Env)
	if err != nil {
		return nil, err
	}

	cniRequestSetEnv(req)

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
		// FIXME: Some DeviceIDs are formated differently between CNIs
		// for instance the sriov CNI uses PCI address from the sriov device plugin
		// and the nf CNI uses interface names from our internal device plugin
		/*
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
		*/
	}

	req.CNIConf = conf
	req.DeviceInfo = cr.DeviceInfo
	req.CNIReq = cr
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

	req, err := cniRequestToPodRequest(&cniRq)
	if err != nil {
		return nil, err
	}
	defer req.Cancel()

	var result *cni100.Result = nil
	if req.Command == cnitypes.CNIAdd {
		result, err = s.cniCmdAddHandler(req)
	} else if req.Command == cnitypes.CNIDel {
		result, err = s.cniCmdDelHandler(req)
	}
	if err != nil {
		klog.Errorf("Error occured in handler: %v", err)
		return nil, err
	}

	response := &cnitypes.Response{Result: result}
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

// NewCNIServer creates a new HTTP router instances to handle the CNI server requests.
func NewCNIServer(addHandler processRequestFunc, delHandler processRequestFunc, options ...func(*Server)) *Server {
	klog.Infof("DPU CNI Server creating new router.")
	router := mux.NewRouter()
	s := &Server{
		Server: http.Server{
			Handler: router,
		},
		cniCmdAddHandler: addHandler,
		cniCmdDelHandler: delHandler,
	}

	router.NotFoundHandler = http.HandlerFunc(http.NotFound)
	router.HandleFunc("/cni", http.HandlerFunc(s.HttpCNIPost))

	for _, o := range options {
		o(s)
	}

	return s
}

func WithPathManager(pathManager utils.PathManager) func(*Server) {
	return func(s *Server) {
		s.pathManager = pathManager
	}
}
