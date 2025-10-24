package ipuplugin_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestIpuPlugin(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "IPU Plugin Suite")
}
