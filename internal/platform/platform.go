package platform

import (
	"strings"

	"github.com/jaypipes/ghw"
	"sigs.k8s.io/kind/pkg/errors"
)

func supportedDpuPlatforms() []string {
	return []string{"IPU Adapter E2100-CCQDA2"}
}

type PlatformInfoProvider interface {
	IsDPU() (bool, error)
}

type PlatformInfo struct{}

func (pi *PlatformInfo) IsDPU() (bool, error) {
	product, err := ghw.Product()
	if err != nil {
		return false, errors.Errorf("Error getting product info: %v", err)
	}

	for _, model := range supportedDpuPlatforms() {
		if strings.Contains(product.Name, model) {
			return true, nil
		}
	}
	return false, nil
}
