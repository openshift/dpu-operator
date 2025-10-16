// Copyright (c) 2024 Intel Corporation.  All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License")
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package ipuplugin

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/vishvananda/netlink"
)

var _ = Describe("basic functionality", Serial, func() {

	// Initialise the handlers
	fileSystemHandler = &MockFileSystemHandlerImpl{}
	networkHandler = &MockNetworkHandlerImpl{}
	ExecutableHandlerGlobal = &MockExecutableHandlerImpl{}
	fxpHandler = &MockFXPHandlerImpl{}
	fakeP4rtClient := &mockP4rtClient{}

	Describe("pf communication channel setup", Serial, func() {

		Context("when pfs are available", func() {
			It("it should return true when checking valid PF", func() {
				Expect(isPF("enp0s1f0d1")).To(Equal(true))
			})
			It("it should return false when checking an invalid PF", func() {
				Expect(isPF("ens11f1")).To(Equal(false))
			})
			It("it should filter and return the correct number of PFs", func() {
				var list []netlink.Link
				GetFilteredPFs(&list) //nolint:errcheck
				Expect(len(list)).To(Equal(4))
			})
			It("it should identify the correct PF in ipu mode", func() {
				var list []netlink.Link
				GetFilteredPFs(&list) //nolint:errcheck
				link, _ := getCommPf("ipu", list)
				octets := strings.Split(link.Attrs().HardwareAddr.String(), ":")
				Expect(octets[3]).To(Equal(accVportId))
			})
			It("it should identify the correct PF in host mode", func() {
				var list []netlink.Link
				GetFilteredPFs(&list) //nolint:errcheck
				link, _ := getCommPf("host", list)
				octets := strings.Split(link.Attrs().HardwareAddr.String(), ":")
				Expect(octets[3]).To(Equal(hostVportId))
			})
			It("it should set correctly the IP on a PF", func() {
				var list []netlink.Link
				GetFilteredPFs(&list) //nolint:errcheck
				link, _ := getCommPf("host", list)
				err := setIP(link, "192.168.1.1")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Method added for test purposes"))
			})
			It("it should configure the communication channel without any errors", func() {
				err := configureChannel("ipu", "192.168.1.1", "192.168.1.2")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Method added for test purposes"))
			})
			It("it should return nil if the PF address is already set", func() {

				networkHandler = &MockNetworkHandler2Impl{}

				err := configureChannel("host", "192.168.1.1", "192.168.1.2")
				Expect(err).ToNot(HaveOccurred())

				// reset the network handler
				networkHandler = &MockNetworkHandlerImpl{}
			})
		})
	})

	Describe("when running in ipu mode", Serial, func() {

		// Create a valid request
		request := &pb.InitRequest{DpuMode: true}

		Context("and a request is made to a correctly configured LifeCycleService", func() {

			It("the server should return a valid response", func() {

				// create valid licycle service
				service := NewLifeCycleService("192.168.1.1", "192.168.1.2", 50151, "ipu", fakeP4rtClient, nil)

				_, err := service.Init(context.Background(), request)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Method added for test purposes"))
			})

			It("the server should return an error if the plugin runs in a different mode", func() {

				// create valid licycle service
				service := NewLifeCycleService("192.168.1.1", "192.168.1.2", 50151, "host", fakeP4rtClient, nil)

				_, err := service.Init(context.Background(), request)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Ipu plugin running in host mode"))
			})
		})
		Context("and a request is made to a misconfigured LifeCycleService", func() {
			It("the server should return a not a valid IPv4 address when daemonIpuIp is invalid", func() {

				// create invalid licycle service
				service := NewLifeCycleService("", "192.168.1", 50151, "ipu", fakeP4rtClient, nil)

				_, err := service.Init(context.Background(), request)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not a valid IPv4 address"))
			})
		})
	})

	Describe("when running in host mode", Serial, func() {

		// Create a valid request
		request := &pb.InitRequest{DpuMode: false}

		Context("and a request is made to a correctly configured LifeCycleService", func() {

			It("the server should return a valid response", func() {

				// create valid licycle service
				service := NewLifeCycleService("192.168.1.1", "192.168.1.2", 50151, "host", fakeP4rtClient, nil)

				_, err := service.Init(context.Background(), request)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Method added for test purposes"))
			})

			It("the server should return an error if the plugin runs in a different mode", func() {

				// create valid licycle service
				service := NewLifeCycleService("192.168.1.1", "192.168.1.2", 50151, "ipu", fakeP4rtClient, nil)

				_, err := service.Init(context.Background(), request)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Ipu plugin running in ipu mode"))
			})
		})
		Context("and a request is made to a misconfigured LifeCycleService", func() {
			It("the server should return a not a valid IPv4 address as daemonHostIp is invalid", func() {

				// create invalid licycle service
				service := NewLifeCycleService("192.168.1", "", 50151, "host", fakeP4rtClient, nil)

				_, err := service.Init(context.Background(), request)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not a valid IPv4 address"))
			})
		})
	})
})

func createNetlinkList() []netlink.Link {
	attr1 := netlink.NewLinkAttrs()
	attr1.Name = "enp0s1f0d1"
	attr1.HardwareAddr, _ = net.ParseMAC("00:0b:00:01:04:19")
	pf1 := &netlink.Dummy{LinkAttrs: attr1}

	attr2 := netlink.NewLinkAttrs()
	attr2.Name = "enp0s1f0d2"
	attr2.HardwareAddr, _ = net.ParseMAC("00:0c:00:02:04:19")
	pf2 := &netlink.Dummy{LinkAttrs: attr2}

	attr3 := netlink.NewLinkAttrs()
	attr3.Name = "enp0s1f0d3"
	attr3.HardwareAddr, _ = net.ParseMAC("00:0d:00:03:04:19")
	pf3 := &netlink.Dummy{LinkAttrs: attr3}

	attr4 := netlink.NewLinkAttrs()
	attr4.Name = "enp0s1f0d4"
	attr4.HardwareAddr, _ = net.ParseMAC("00:0e:00:04:04:19")
	pf4 := &netlink.Dummy{LinkAttrs: attr4}

	attr5 := netlink.NewLinkAttrs()
	attr5.Name = "ens11f1"
	attr5.HardwareAddr, _ = net.ParseMAC("00:00:00:00:00:19")
	pf5 := &netlink.Dummy{LinkAttrs: attr5}

	return []netlink.Link{pf1, pf2, pf3, pf4, pf5}
}

type MockNetworkHandlerImpl struct {
}

func (h *MockNetworkHandlerImpl) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	return fmt.Errorf("Method added for test purposes")
}
func (h *MockNetworkHandlerImpl) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return []netlink.Addr{}, nil
}
func (h *MockNetworkHandlerImpl) LinkList() ([]netlink.Link, error) {
	return createNetlinkList(), nil
}

type MockNetworkHandler2Impl struct {
}

func (h *MockNetworkHandler2Impl) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	return fmt.Errorf("Method added for test purposes")
}
func (h *MockNetworkHandler2Impl) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	ipAddr := net.ParseIP("192.168.1.1")
	// Set the IP address on PF
	addr := &netlink.Addr{IPNet: &net.IPNet{IP: ipAddr, Mask: net.CIDRMask(24, 32)}}
	return []netlink.Addr{*addr}, nil
}
func (h *MockNetworkHandler2Impl) LinkList() ([]netlink.Link, error) {
	return createNetlinkList(), nil
}

type MockFileSystemHandlerImpl struct {
}

func (fs *MockFileSystemHandlerImpl) GetDevice(name string) ([]byte, error) {
	if name == "enp0s1f0d1" || name == "enp0s1f0d2" || name == "enp0s1f0d3" || name == "enp0s1f0d4" {
		return []byte("0x1452\n"), nil
	}
	if name == "ens11f1" {
		return []byte("0x1592\n"), nil
	}
	return nil, fmt.Errorf("mock GetDevice error")
}

func (fs *MockFileSystemHandlerImpl) GetVendor(name string) ([]byte, error) {
	if name == "enp0s1f0d1" || name == "enp0s1f0d2" || name == "enp0s1f0d3" || name == "enp0s1f0d4" || name == "ens11f1" {
		return []byte("0x8086\n"), nil
	}
	return nil, fmt.Errorf("mock GetVendor error")
}

type MockExecutableHandlerImpl struct{}

func (m *MockExecutableHandlerImpl) validate() bool {
	return true
}
func (m *MockExecutableHandlerImpl) SetupAccApfs() error {
	InitAccApfMacs = true
	return nil
}

func (e *MockExecutableHandlerImpl) nmcliSetupIpAddress(link netlink.Link, ipStr string, ipAddr *netlink.Addr) error {
	return fmt.Errorf("Method added for test purposes")
}

type MockFXPHandlerImpl struct{}

func (m *MockFXPHandlerImpl) configureFXP(p4rtClient types.P4RTClient, brCtlr types.BridgeController) error {
	return nil
}
