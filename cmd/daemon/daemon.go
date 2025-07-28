package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	daemon "github.com/openshift/dpu-operator/internal/daemon"
	"github.com/openshift/dpu-operator/internal/platform"
	"go.uber.org/zap/zapcore"

	"github.com/openshift/dpu-operator/internal/daemon/plugin"
	"github.com/openshift/dpu-operator/internal/utils"
	"github.com/spf13/afero"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func createImagesFromEnv() (map[string]string, error) {
	imagesMap := make(map[string]string)

	for _, imageName := range plugin.AllImages {
		value := os.Getenv(imageName)
		if value == "" {
			return nil, fmt.Errorf("required environment variable %s is not set", imageName)
		}
		imagesMap[imageName] = value
	}

	return imagesMap, nil
}

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

	vspImages, err := createImagesFromEnv()
	if err != nil {
		log.Error(err, "Failed to create VSP images map")
		panic(err)
	}

	platform := &platform.HardwarePlatform{}
	d := daemon.NewDaemon(afero.NewOsFs(), platform, mode, ctrl.GetConfigOrDie(), vspImages, utils.NewPathManager("/"))
	if err := d.PrepareAndServe(context.Background()); err != nil {
		log.Error(err, "Failed to run daemon")
		panic(err)
	}
}
