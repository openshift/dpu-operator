package cniserver_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCniserver(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cniserver Suite")
}
