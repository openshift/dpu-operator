package cnihelper_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCnihelper(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cnihelper Suite")
}
