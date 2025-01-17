package main

import (
	"github.com/intel/ipu-opi-plugins/ipu-plugin/ipuplugin/cmd"
	"os"
)

func main() {
	os.Setenv("P4_NAME", "fxp-net_linux-networking")
	cmd.Execute()
}
