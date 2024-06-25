package cniserver_test

import (
	"net"
	"os"
	"path/filepath"

	"github.com/containernetworking/cni/pkg/skel"
	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cni"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cniserver"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"

	current "github.com/containernetworking/cni/pkg/types/100"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
	utiltesting "k8s.io/client-go/util/testing"
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
		tmpDir           string
		plugin           *cni.Plugin
		cniServer        *cniserver.Server
		serverSocketPath string
		listener         net.Listener
		addHandlerCalled bool
		delHandlerCalled bool
	)

	g.Context("CNI Server APIs", func() {
		g.BeforeEach(func() {
			var err error
			// Create a tmp directory in the test container
			tmpDir, err = utiltesting.MkTmpdir("cniserver")
			o.Expect(err).NotTo(o.HaveOccurred())

			serverSocketPath = filepath.Join(tmpDir, filepath.Base(cnitypes.ServerSocketPath))
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

			cniServer = cniserver.NewCNIServer(addHandler, delHandler,
				cniserver.WithSocketPath(serverSocketPath))
			listener, err = cniServer.Listen()
			o.Expect(err).NotTo(o.HaveOccurred())
			go utilwait.Forever(func() {
				cniServer.Serve(listener)
			}, 0)

			plugin = &cni.Plugin{SocketPath: serverSocketPath}
		})

		g.AfterEach(func() {
			listener.Close()
			os.RemoveAll(tmpDir)
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
