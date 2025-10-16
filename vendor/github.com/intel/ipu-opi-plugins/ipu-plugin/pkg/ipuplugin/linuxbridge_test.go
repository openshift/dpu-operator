// Copyright (c) 2023 Intel Corporation.  All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License")
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ipuplugin

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vishvananda/netlink"
)

// Run tests specs in serial as we cannot share netlink function pointers across different specs
var _ = Describe("linuxBridge", Serial, func() {
	Describe("createBridge", func() {

		brCtlr := &linuxBridge{
			brName: "fakeBr",
		}

		Context("when netlink.LinkAdd resulted in error", func() {
			It("should return error", func() {
				linkAddFn = fakeLinkAddWithErr
				linkSetUpFn = fakeLinkSetUpWithErr
				err := brCtlr.createBridge()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("fake LinkByName error"))
				Expect(err.Error()).NotTo(ContainSubstring("fake LinkSetUp error"))
			})
		})

		Context("when netlink.LinkAdd is ok but netlink.LinkSetUp resulted in error", func() {
			It("should return error", func() {
				linkAddFn = fakeLinkAdd
				linkSetUpFn = fakeLinkSetUpWithErr
				err := brCtlr.createBridge()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).NotTo(ContainSubstring("fake LinkByName error"))
				Expect(err.Error()).To(ContainSubstring("fake LinkSetUp error"))
			})
		})
		Context("when LinkAdd and LinkSetUp returned no error", func() {
			It("should return no error", func() {
				linkAddFn = fakeLinkAdd
				linkSetUpFn = fakeLinkSetUp
				err := brCtlr.createBridge()
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})
	Describe("AddPort", Serial, func() {
		brCtlr := &linuxBridge{
			brName: "fakeBr",
		}

		Context("when vport interface look up returns error", func() {
			It("should return error", func() {
				linkByNameFn = fakeLinkByNameWithErr
				Expect(brCtlr.AddPort("fakeVlan")).To(HaveOccurred())
			})
		})

		Context("when the vport interface is not vlan type", func() {
			It("should return error", func() {
				linkByNameFn = func(name string) (netlink.Link, error) {
					link := &netlink.Dummy{} // just a generic link type - not vlan type.
					link.Name = name
					return link, nil
				}
				err := brCtlr.AddPort("fakeVlan")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("type is not vlan type"))
			})
		})

		Context("when unable to add vport to the bridge", func() {
			It("should return error", func() {
				linkByNameFn = func(name string) (netlink.Link, error) {
					link := &netlink.Vlan{}
					link.Name = name
					return link, nil
				}
				linkSetMasterFn = fakeLinkSetMasterWithErr
				err := brCtlr.AddPort("fakeVlan")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("error adding vlan interface fakeVlan to bridge fakeBr"))
			})
		})

		Context("when unable to bring the vport in up state", func() {
			It("should return error", func() {
				linkByNameFn = func(name string) (netlink.Link, error) {
					link := &netlink.Vlan{}
					link.Name = name
					return link, nil
				}
				linkSetMasterFn = fakeLinkSetMaster
				linkSetUpFn = fakeLinkSetUpWithErr
				err := brCtlr.AddPort("fakeVlan")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("error bringing interface fakeVlan up"))
			})
		})

		Context("when all netlink calls succeed", func() {
			It("should return no error", func() {
				linkByNameFn = func(name string) (netlink.Link, error) {
					link := &netlink.Vlan{}
					link.Name = name
					return link, nil
				}
				linkSetMasterFn = fakeLinkSetMaster
				linkSetUpFn = fakeLinkSetUp
				err := brCtlr.AddPort("fakeVlan")
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	Describe("DeletePort", Serial, func() {
		brCtlr := &linuxBridge{
			brName: "fakeBr",
		}

		Context("when vport interface look up returns error", func() {
			It("should return error", func() {
				linkByNameFn = fakeLinkByNameWithErr
				Expect(brCtlr.DeletePort("fakeVlan")).To(HaveOccurred())
			})
		})

		Context("when the vport interface is not vlan type", func() {
			It("should return error", func() {
				linkByNameFn = func(name string) (netlink.Link, error) {
					link := &netlink.Dummy{} // just a generic link type - not vlan type.
					link.Name = name
					return link, nil
				}
				err := brCtlr.DeletePort("fakeVlan")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("type is not vlan type"))
			})
		})

		Context("when unable to remove vport from the bridge", func() {
			It("should return error", func() {
				linkByNameFn = func(name string) (netlink.Link, error) {
					link := &netlink.Vlan{}
					link.Name = name
					return link, nil
				}
				linkSetNoMasterFn = fakeLinkSetNoMasterWithErr
				err := brCtlr.DeletePort("fakeVlan")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("error removing vlan interface fakeVlan from bridge fakeBr"))
			})
		})

		Context("when unable to bring the vport in down state", func() {
			It("should return error", func() {
				linkByNameFn = func(name string) (netlink.Link, error) {
					link := &netlink.Vlan{}
					link.Name = name
					return link, nil
				}
				linkSetNoMasterFn = fakeLinkSetNoMaster
				linkSetDownFn = fakeLinkSetDownWithErr
				err := brCtlr.DeletePort("fakeVlan")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("error bringing interface fakeVlan down"))
			})
		})

		Context("when all netlink calls succeed", func() {
			It("should return no error", func() {
				linkByNameFn = func(name string) (netlink.Link, error) {
					link := &netlink.Vlan{}
					link.Name = name
					return link, nil
				}
				linkSetNoMasterFn = fakeLinkSetNoMaster
				linkSetDownFn = fakeLinkSetDown
				err := brCtlr.DeletePort("fakeVlan")
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	Describe("EnsureBridgeExists", Serial, func() {
		Context("when bridge name is empty", func() {
			brCtlr := &linuxBridge{}
			It("should return error", func() {
				err := brCtlr.EnsureBridgeExists()
				Expect(err).To(HaveOccurred())
			})
		})
		Context("when a link is found with bridge name but not bridge type", func() {
			brCtlr := &linuxBridge{
				brName: "fakeBr",
			}
			It("should return error", func() {
				linkByNameFn = fakeLinkByName
				err := brCtlr.EnsureBridgeExists()
				Expect(err).To(HaveOccurred())
			})
		})
		Context("when a link is found with bridge and of a bridge type", func() {
			brCtlr := &linuxBridge{
				brName: "fakeBr",
			}
			It("should not return error", func() {
				linkByNameFn = func(name string) (netlink.Link, error) {
					link := &netlink.Bridge{}
					link.Name = name
					return link, nil
				}
				err := brCtlr.EnsureBridgeExists()
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when a link is found", func() {
			brCtlr := &linuxBridge{
				brName: "fakeBr",
			}
			It("should create a new bridge and not return any error", func() {
				linkByNameFn = fakeLinkByNameWithErr
				linkAddFn = fakeLinkAdd
				linkSetUpFn = fakeLinkSetUp
				err := brCtlr.EnsureBridgeExists()
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})
})

// Mock netlink functions for testing
func fakeLinkByName(name string) (netlink.Link, error) {
	return &netlink.Dummy{}, nil
}

func fakeLinkByNameWithErr(name string) (netlink.Link, error) {
	return nil, fmt.Errorf("fake LinkByName error")
}

func fakeLinkAdd(link netlink.Link) error {
	return nil
}

func fakeLinkAddWithErr(link netlink.Link) error {
	return fmt.Errorf("fake LinkByName error")
}

// nolint
func fakeLinkDel(link netlink.Link) error {
	return nil
}

// nolint
func fakeLinkDelWithErr(link netlink.Link) error {
	return fmt.Errorf("fake LinkDel error")
}

func fakeLinkSetUp(link netlink.Link) error {
	return nil
}

func fakeLinkSetUpWithErr(link netlink.Link) error {
	return fmt.Errorf("fake LinkSetUp error")
}

func fakeLinkSetDown(link netlink.Link) error {
	return nil
}

func fakeLinkSetDownWithErr(link netlink.Link) error {
	return fmt.Errorf("fake error")
}

func fakeLinkSetMaster(link netlink.Link, master netlink.Link) error {
	return nil
}

func fakeLinkSetMasterWithErr(link netlink.Link, master netlink.Link) error {
	return fmt.Errorf("fake error")
}

func fakeLinkSetNoMaster(link netlink.Link) error {
	return nil
}

func fakeLinkSetNoMasterWithErr(link netlink.Link) error {
	return fmt.Errorf("fake error")
}
