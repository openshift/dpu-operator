package main

import (
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cniserver"
	"k8s.io/klog/v2"
)

// This server implementations is only temporary for testing when the DPU Daemon
// code has not been implemented.

// FIXME: This will disappear in the near future
func main() {
	_, err := cniserver.StartCNIServer()
	if err != nil {
		klog.Errorf("DPU CNI server Serve() failed: %v", err)
	}
}
