package platform

import (
	"github.com/jaypipes/ghw"
	"sync"
)

type Platform interface {
	PciDevices() ([]*ghw.PCIDevice, error)
	Product() (*ghw.ProductInfo, error)
}

type HardwarePlatform struct{}

func NewHardwarePlatform() *HardwarePlatform {
	return &HardwarePlatform{}
}

func (hp *HardwarePlatform) PciDevices() ([]*ghw.PCIDevice, error) {
	pciInfo, err := ghw.PCI()
	if err != nil {
		return nil, err
	}
	return pciInfo.Devices, nil
}

func (hp *HardwarePlatform) Product() (*ghw.ProductInfo, error) {
	return ghw.Product()
}

type FakePlatform struct {
	platformName string
	devices      []*ghw.PCIDevice
	mu           sync.Mutex
}

func NewFakePlatform(platformName string) *FakePlatform {
	return &FakePlatform{
		platformName: platformName,
		devices:      make([]*ghw.PCIDevice, 0),
	}
}

func (p *FakePlatform) PciDevices() ([]*ghw.PCIDevice, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.devices, nil
}

func (p *FakePlatform) Product() (*ghw.ProductInfo, error) {
	return &ghw.ProductInfo{
		Name: p.platformName,
	}, nil
}
func (p *FakePlatform) RemoveAllPciDevices() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.devices = make([]*ghw.PCIDevice, 0)
}
