package main

import (
	"fmt"
	"go/types"
	"os"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/version"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cni"
	"github.com/urfave/cli/v2"
)

const cniName string = "dpu-cni"

func main() {
	c := cli.NewApp()
	c.Name = cniName
	c.Usage = "a CNI plugin to set up or tear down a container's network with DPUs"
	c.Version = "0.0.2"

	p := cni.NewCNIPlugin()
	c.Action = func(ctx *cli.Context) error {
		skel.PluginMain(
			p.CmdAdd,
			p.CmdCheck,
			p.CmdDel,
			version.All,
			bv.BuildString(cniName))
		return nil
	}

	if err := c.Run(os.Args); err != nil {
		// Print the error to stdout in conformance with the CNI spec
		e, ok := err.(*types.Error)
		if !ok {
			e = &types.Error{Msg: err.Error()}
		}
		fmt.Printf("e: %v\n", e)
	}
}
