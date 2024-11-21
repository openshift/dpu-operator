package sriovutils

import (
	"fmt"
	"github.com/containernetworking/plugins/pkg/ns"
)

type PCIAllocator struct {
	store KeyValueStore
}

func NewPCIAllocator(store KeyValueStore) *PCIAllocator {
	return &PCIAllocator{
		store: store,
	}
}

func (p *PCIAllocator) SaveAllocatedPCI(pciAddress, ns string) error {
	err := p.store.Set(pciAddress, ns)
	if err != nil {
		return fmt.Errorf("Failed to save PCI address %v with ns %v", pciAddress, ns)
	}
	return nil
}

func (p *PCIAllocator) LoadAllocatedPCI(pciAddress string) (string, error) {
	return p.store.Get(pciAddress)
}

func (p *PCIAllocator) DeleteAllocatedPCI(pciAddress string) error {
	return p.store.Delete(pciAddress)
}

func namespaceExists(namespace string) bool {
	networkNamespace, err := ns.GetNS(namespace)
	if err != nil {
		return false
	}
	defer networkNamespace.Close()
	return true
}

// Check if the device is already allocated. This is to prevent issues
// where kubelet request to delete a pod and in the same time a new pod
// using the same vf is started. we can have an issue where the cmdDel of
// the old pod is called AFTER the cmdAdd of the new one. This will block
// the new pod creation until the cmdDel is done.
func (p *PCIAllocator) Sync(pciAddress string) (bool, error) {
	value, err := p.store.Get(pciAddress)
	if err != nil {
		return false, err
	}

	if !namespaceExists(value) {
		err = p.store.Delete(pciAddress)
		return true, err
	}

	return false, nil
}
