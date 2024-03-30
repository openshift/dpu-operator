package main

import (
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cniserver"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	cni100 "github.com/containernetworking/cni/pkg/types/100"
	"k8s.io/klog/v2"
)

// This server implementations is only temporary for testing when the DPU Daemon
// code has not been implemented.

// FIXME: This will disappear in the near future
func main() {
	f := func(request *cnitypes.PodRequest) (*cni100.Result, error) {return nil, nil}
	err := cniserver.NewCNIServer(f, f).ListenAndServe()
	if err != nil {
		klog.Errorf("DPU CNI server Serve() failed: %v", err)
	}
}
