package cniserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"syscall"

	cni100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/gorilla/mux"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnihelper"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	"k8s.io/klog/v2"
)

// This server implementations is only temporary for testing when the DPU Daemon
// code has not been implemented.

type processRequestFunc func(request *cnitypes.Request) error
type Server struct {
	http.Server
	processRequest processRequestFunc
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

func processRequest(request *cnitypes.Request) error {
	// FIXME: Do actual work here.
	klog.Infof("DEBUG: %v", request)
	return nil
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

	s.processRequest(&cniRq)

	conf, err := cnihelper.ReadCNIConfig(cniRq.Config)
	if err != nil {
		return nil, err
	}

	// FIXME: Dummy response for CMDADD, Get most of result contents from "processRequest()"
	response := &cnitypes.Response{}
	result := &cni100.Result{
		CNIVersion: conf.CNIVersion,
	}
	response.Result = result

	return json.Marshal(&response)
}

// HttpCNIPost is a callback functions to handle "/cni" requests.
func (s *Server) HttpCNIPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, fmt.Sprintf("Method not allowed"), http.StatusMethodNotAllowed)
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
func (s *Server) Start(runDir string, serverSocketPath string) {
	klog.Infof("DPU CNI Server getting listener.")
	listener, err := getListener(runDir, serverSocketPath)
	if err != nil {
		klog.Errorf("Failed to start the CNI server using socket %s. Reason: %+v", cnitypes.ServerSocketPath, err)
	}

	klog.Infof("DPU CNI Server is now serving requests.")
	if err := s.Serve(listener); err != nil {
		klog.Errorf("DPU CNI server Serve() failed: %v", err)
	}
}

// NewCNIServer creates a new HTTP router instances to handle the CNI server requests.
func NewCNIServer(rqFunc processRequestFunc) (*Server, error) {
	klog.Infof("DPU CNI Server creating new router.")
	router := mux.NewRouter()
	s := &Server{
		Server: http.Server{
			Handler: router,
		},
		processRequest: rqFunc,
	}

	router.NotFoundHandler = http.HandlerFunc(http.NotFound)
	router.HandleFunc("/cni", http.HandlerFunc(s.HttpCNIPost))

	return s, nil
}

// startCNIServer is the entry point to start the HTTP "unix" socket to listen for DPU CNI shim requests.
func StartCNIServer() (*Server, error) {
	klog.Infof("DPU CNI Server creating new CNI server.")
	cniServer, err := NewCNIServer(processRequest)
	if err != nil {
		return nil, err
	}

	klog.Infof("DPU CNI Server starting listener.")
	cniServer.Start(cnitypes.ServerRunDir, cnitypes.ServerSocketPath)

	return cniServer, nil
}
