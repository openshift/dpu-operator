package main_test

import (
	"flag"
	"os"
	"path/filepath"

	g "github.com/onsi/ginkgo/v2"
	"go.uber.org/zap/zapcore"

	. "github.com/onsi/gomega"
	o "github.com/onsi/gomega"

	. "github.com/openshift/dpu-operator/daemon"
	"github.com/openshift/dpu-operator/daemon/plugin"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cni"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cniserver"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"

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
		dummyPluginDPU := plugin.NewDummyPlugin()
		DpuDaemon := NewDpuDaemon(dummyPluginDPU)

		dummyPluginHost := plugin.NewDummyPlugin()
		HostDaemon := NewHostDaemon(dummyPluginHost)

		g.It("Should connect succesfully if the server is up first", func() {
			DpuDaemon.Start()
			HostDaemon.Start()
			err := HostDaemon.CreateBridgePort(1,1,1)
			Expect(err).ShouldNot(HaveOccurred())

			HostDaemon.Stop()
			DpuDaemon.Stop()
		})
		g.It("Should connect succesfully if the server is up first", func() {
			HostDaemon.Start()
			DpuDaemon.Start()
			err := HostDaemon.CreateBridgePort(1,1,1)
			Expect(err).ShouldNot(HaveOccurred())
			HostDaemon.Stop()
			DpuDaemon.Stop()
		})
	})

	g.Context("Daemon on host should receive request from CNI", func() {
		tmpDir, err = utiltesting.MkTmpdir("cniserver")
		defer os.RemoveAll(tmpDir)
		o.Expect(err).NotTo(o.HaveOccurred())
		serverSocketPath := filepath.Join(tmpDir, cnitypes.ServerSocketName)

		dummyPluginHost := plugin.NewDummyPlugin()
		HostDaemon := NewHostDaemon(dummyPluginHost).WithCniServerPath(serverSocketPath)
		HostDaemon.Start()
	
		plugin = &cni.Plugin{SocketPath: serverSocketPath}

		g.Context("CNI propagetes to vendor plugin on DPU", func() {
			g.When("Normal ADD request", func() {
				cniVersion := "0.4.0"
				expectedResult := &current.Result{
					CNIVersion: cniVersion,
				}
				g.It("should get a correct response from the post request", func() {
					resp, ver, err := plugin.PostRequest(PrepArgs(cniVersion))
					o.Expect(err).NotTo(o.HaveOccurred())
					o.Expect(ver).To(o.Equal(cniVersion))
					o.Expect(resp.Result).To(o.Equal(expectedResult))
				})
			})
		})

	})
})




