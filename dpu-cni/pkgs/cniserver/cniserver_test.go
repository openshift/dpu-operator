package cniserver_test

import (
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
	cniConfig := "{\"cniVersion\": \"" + cniVersion + "\",\"name\": \"dpucni\",\"type\": \"dpucni\"}"
	cmdArgs := &skel.CmdArgs{
		ContainerID: "fakecontainerid",
		Netns:       "fakenetns",
		IfName:      "fakeeth0",
		Args:        "",
		Path:        "fakepath",
		StdinData:   []byte(cniConfig),
	}
	os.Clearenv()
	os.Setenv("CNI_COMMAND", command)
	os.Setenv("CNI_ARGS", "K8S_POD_NAMESPACE=x;K8S_POD_NAME=y;K8S_POD_UID=z")
	os.Setenv("CNI_CONTAINERID", cmdArgs.ContainerID)
	os.Setenv("CNI_NETNS", cmdArgs.Netns)
	os.Setenv("CNI_IFNAME", cmdArgs.IfName)
	os.Setenv("CNI_PATH", cmdArgs.Path)

	return cmdArgs
}

var _ = g.Describe("Cniserver", func() {
	var tmpDir string
	var plugin *cni.Plugin

	var err error
	var addHandlerCalled bool = false
	var delHandlerCalled bool = false

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

	// Create a tmp directory in the test container
	tmpDir, err = utiltesting.MkTmpdir("cniserver")
	defer os.RemoveAll(tmpDir)
	o.Expect(err).NotTo(o.HaveOccurred())
	serverSocketPath := filepath.Join(tmpDir, cnitypes.ServerSocketName)
	cniServer := cniserver.NewCNIServer(
		addHandler, delHandler,
		cniserver.WithSocketPath(tmpDir, serverSocketPath))
	go utilwait.Forever(func() {
		cniServer.Start()
	}, 0)

	plugin = &cni.Plugin{SocketPath: serverSocketPath}

	g.Context("CNI Server APIs", func() {
		g.When("Normal ADD request", func() {
			cniVersion := "0.4.0"
			expectedResult := &current.Result{
				CNIVersion: cniVersion,
			}
			g.It("should get a correct response from the post request", func() {
				resp, ver, err := plugin.PostRequest(PrepArgs(cniVersion, "ADD"))
				o.Expect(err).NotTo(o.HaveOccurred())
				o.Expect(ver).To(o.Equal(cniVersion))
				o.Expect(resp.Result).To(o.Equal(expectedResult))
			})
			g.It("should call add handler when passing in ADD", func() {
				addHandlerCalled = false
				delHandlerCalled = false
				plugin.PostRequest(PrepArgs(cniVersion, "ADD"))
				o.Expect(addHandlerCalled).To(o.Equal(true))
				o.Expect(delHandlerCalled).To(o.Equal(false))
			})
			g.It("should call add handler when passing in DEL", func() {
				addHandlerCalled = false
				delHandlerCalled = false
				plugin.PostRequest(PrepArgs(cniVersion, "DEL"))
				o.Expect(addHandlerCalled).To(o.Equal(false))
				o.Expect(delHandlerCalled).To(o.Equal(true))
			})

		})
	})
})
