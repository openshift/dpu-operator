package cnihelper_test

import (
	"github.com/containernetworking/cni/pkg/types"
	g "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	o "github.com/onsi/gomega"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnihelper"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
)

var _ = g.Describe("Cnihelper", func() {
	g.Context("CNI Netconf Parser", func() {
		g.When("DPU-CNI is specified", func() {
			inputConfig := `
    		{
				"cniVersion": "0.4.0",
				"name": "dpu",
				"type": "dpucni"
    		}`
			expectedNetConf := &cnitypes.NetConf{
				NetConf: types.NetConf{Name: "dpu", CNIVersion: "0.4.0", Type: "dpucni"},
			}
			g.It("should parse the Netconf correctly", func() {
				netconf, err := cnihelper.ReadCNIConfig([]byte(inputConfig))
				o.Expect(err).NotTo(o.HaveOccurred())
				o.Expect(netconf).To(gomega.Equal(expectedNetConf))
			})
		})
	})
})
