package main

import (
	"flag"

	daemon "github.com/openshift/dpu-operator/internal/daemon"
	"go.uber.org/zap/zapcore"

	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"github.com/openshift/dpu-operator/internal/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	config := ctrl.GetConfigOrDie()
	client, err := client.New(config, client.Options{
		Scheme: scheme.Scheme,
	})

	if err != nil {
		log.Error(err, "Failed to create client")
		return
	}

	vspImages := plugin.CreateVspImagesMap(true, log)

	d := daemon.NewDaemon(mode, client, vspImages, config)
	if err := d.Run(); err != nil {
		log.Error(err, "Failed to run daemon")
		panic(err)
	}
}
