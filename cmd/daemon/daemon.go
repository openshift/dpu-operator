package main

import (
	"flag"

	daemon "github.com/openshift/dpu-operator/internal/daemon"
	"go.uber.org/zap/zapcore"

	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"github.com/openshift/dpu-operator/internal/utils"
	"github.com/spf13/afero"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func main() {
	var mode string
	flag.StringVar(&mode, "mode", "", "Mode for the daemon, can be either host or dpu")
	opts := zap.Options{
		Development: true,
		Level:       zapcore.DebugLevel,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	log := ctrl.Log.WithName("Daemon Init")
	log.Info("Daemon init")

	vspImages := plugin.CreateVspImagesMap(true, log)

	d := daemon.NewDaemon(afero.NewOsFs(), mode, ctrl.GetConfigOrDie(), vspImages, utils.NewPathManager("/"))
	if err := d.ListenAndServe(); err != nil {
		log.Error(err, "Failed to run daemon")
		panic(err)
	}
}
