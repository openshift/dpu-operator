package main

import (
	"context"
	"flag"

	daemon "github.com/openshift/dpu-operator/internal/daemon"
	"github.com/openshift/dpu-operator/internal/platform"
	"go.uber.org/zap/zapcore"

	"github.com/openshift/dpu-operator/internal/images"
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

	imageManager := images.NewEnvImageManager()

	platform := &platform.HardwarePlatform{}
	d := daemon.NewDaemon(afero.NewOsFs(), platform, mode, ctrl.GetConfigOrDie(), imageManager, utils.NewPathManager("/"))
	if err := d.PrepareAndServe(context.Background()); err != nil {
		log.Error(err, "Failed to run daemon")
		panic(err)
	}
}
