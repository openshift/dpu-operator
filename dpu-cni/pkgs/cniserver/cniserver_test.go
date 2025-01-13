package cniserver_test

import (
	"net"
	"os"

	"github.com/containernetworking/cni/pkg/skel"
	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cni"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cniserver"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
	"github.com/openshift/dpu-operator/internal/testutils"
	"github.com/openshift/dpu-operator/internal/utils"

	current "github.com/containernetworking/cni/pkg/types/100"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
)

func PrepArgs(cniVersion string, command string) *skel.CmdArgs {
	cniConfig := "{\"cniVersion\": \"" + cniVersion + "\",\"name\": \"dpucni\",\"type\": \"dpucni\", \"Mac\": \"00:11:22:33:44:55\"}"
	cmdArgs := &skel.CmdArgs{
		ContainerID: "fakecontainerid",
		Netns:       "fakenetns",
		IfName:      "fakeeth0",
		Args:        "",
		Path:        "fakepath",
		StdinData:   []byte(cniConfig),
	}
	os.Setenv("CNI_COMMAND", command)
	os.Setenv("CNI_ARGS", "K8S_POD_NAMESPACE=x;K8S_POD_NAME=y;K8S_POD_UID=z")
	os.Setenv("CNI_CONTAINERID", cmdArgs.ContainerID)
	os.Setenv("CNI_NETNS", cmdArgs.Netns)
	os.Setenv("CNI_IFNAME", cmdArgs.IfName)
	os.Setenv("CNI_PATH", cmdArgs.Path)

	return cmdArgs
}

var _ = g.Describe("Cniserver", func() {
	var (
		plugin           *cni.Plugin
		cniServer        *cniserver.Server
		listener         net.Listener
		addHandlerCalled bool
		delHandlerCalled bool
		testCluster      testutils.KindCluster
	)

	g.BeforeEach(func() {
		testCluster = testutils.KindCluster{Name: "dpu-operator-test-cluster"}
		testCluster.EnsureExists()
	})

	g.AfterEach(func() {
		if os.Getenv("FAST_TEST") == "false" {
			testCluster.EnsureDeleted()
		}
	})
	g.Context("CNI Server APIs", func() {
		g.BeforeEach(func() {
			var err error
			addHandlerCalled = false
			delHandlerCalled = false
			addHandler := func(request *cnitypes.PodRequest) (*current.Result, error) {
				result := &current.Result{
					CNIVersion: request.CNIConf.CNIVersion,
				}

				addHandlerCalled = true

				return result, nil
			}

			delHandler := func(request *cnitypes.PodRequest) (*current.Result, error) {
				result := &current.Result{
					CNIVersion: request.CNIConf.CNIVersion,
				}

				delHandlerCalled = true
				return result, nil
			}

			pathManager := utils.NewPathManager(testCluster.TempDirPath())
			cniServer = cniserver.NewCNIServer(addHandler, delHandler,
				cniserver.WithPathManager(*pathManager))
			listener, err = cniServer.Listen()
			o.Expect(err).NotTo(o.HaveOccurred())
			go utilwait.Forever(func() {
				cniServer.Serve(listener)
			}, 0)

			plugin = &cni.Plugin{SocketPath: pathManager.CNIServerPath()}
		})

		g.AfterEach(func() {
			listener.Close()
		})

		g.Context("CNI Server APIs", func() {
			g.When("Normal ADD request", func() {
				cniVersion := "0.4.0"
				g.It("should call add handler when passing in ADD", func() {
					addHandlerCalled = false
					delHandlerCalled = false
					plugin.PostRequest(PrepArgs(cniVersion, cnitypes.CNIAdd))
					o.Expect(addHandlerCalled).To(o.Equal(true))
					o.Expect(delHandlerCalled).To(o.Equal(false))
				})
				g.It("should call add handler when passing in DEL", func() {
					addHandlerCalled = false
					delHandlerCalled = false
					plugin.PostRequest(PrepArgs(cniVersion, cnitypes.CNIDel))
					o.Expect(addHandlerCalled).To(o.Equal(false))
					o.Expect(delHandlerCalled).To(o.Equal(true))
				})

			})
		})
	})
})
