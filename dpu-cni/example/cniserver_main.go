package main

import (
	"errors"
	"fmt"

	cni100 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cniserver"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/networkfn"
	"k8s.io/klog/v2"
)

// This server implementations is only temporary for testing when the DPU Daemon
// code has not been implemented.

// FIXME: This will disappear in the near future
func cniCmdNfAddHandler(req *cnitypes.PodRequest) (*cni100.Result, error) {
	klog.Info("cniCmdNfAddHandler")
	res, err := networkfn.CmdAdd(req)
	if err != nil {
		return nil, fmt.Errorf("SRIOV manager failed in add handler: %v", err)
	}
	return res, nil
}

func cniCmdNfDelHandler(req *cnitypes.PodRequest) (*cni100.Result, error) {
	klog.Info("cniCmdNfDelHandler")
	err := networkfn.CmdDel(req)
	if err != nil {
		return nil, errors.New("SRIOV manager failed in del handler")
	}
	return nil, nil
}

func main() {
	host := false
	if !host {
		// Test NF CNI
		err := cniserver.NewCNIServer(cniCmdNfAddHandler, cniCmdNfDelHandler).ListenAndServe()
		if err != nil {
			klog.Errorf("DPU CNI server Serve() failed: %v", err)
		}
	}
}
