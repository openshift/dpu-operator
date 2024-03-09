package main_test

import (
	"flag"

	g "github.com/onsi/ginkgo/v2"
	"go.uber.org/zap/zapcore"

	. "github.com/onsi/gomega"

	. "github.com/openshift/dpu-operator/daemon"
	"github.com/openshift/dpu-operator/daemon/plugin"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var _ = g.BeforeSuite(func() {
		opts := zap.Options{
		    Development: true,
		    Level:       zapcore.DebugLevel,
		}
		opts.BindFlags(flag.CommandLine)
		flag.Parse()
		ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
})

var _ = g.Describe("Main", func() {
	g.Context("Connection", func() {
		dummyPluginDPU  := plugin.NewDummyPlugin()
		DpuDaemon := NewDpuDaemon(dummyPluginDPU)

		dummyPluginHost := plugin.NewDummyPlugin()
		HostDaemon := NewHostDaemon(dummyPluginHost)

		g.It("Should connect succesfully if the server is up first", func() {
			HostDaemon.Start()
			DpuDaemon.Start()
			err := HostDaemon.CreateBridgePort(1,1,1)
			Expect(err).ShouldNot(HaveOccurred())
		})
	})
})

