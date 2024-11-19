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

	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/types"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

var (
	// Abstract netlink functions for unit tests
	linkByNameFn      = netlink.LinkByName
	linkAddFn         = netlink.LinkAdd
	linkDelFn         = netlink.LinkDel
	linkSetUpFn       = netlink.LinkSetUp
	linkSetDownFn     = netlink.LinkSetDown
	linkSetMasterFn   = netlink.LinkSetMaster
	linkSetNoMasterFn = netlink.LinkSetNoMaster
)

type linuxBridge struct {
	brName string
}

func NewLinuxBridgeController(bridge string) types.BridgeController {
	return &linuxBridge{
		brName: bridge,
	}
}

// Note:: This is expected to be called, when plugin exits(Stop),
func (b *linuxBridge) DeleteBridges() error {
	if b.brName == "" {
		return fmt.Errorf("bridge name is empty")
	}
	br, err := linkByNameFn(b.brName)
	if err == nil {
		if br.Type() == "bridge" {
			// Bridge exists
			log.Infof("bridge %s found", b.brName)
			if err := linkDelFn(br); err != nil {
				return fmt.Errorf("error deleting bridge %s: %s", b.brName, err.Error())
			}
		} else {
			// a link is found with same name but not a bridge
			return fmt.Errorf("a link is found with name %s but link type is not bridge", b.brName)
		}
	}
	return fmt.Errorf("bridge-> %s does not exist, error->%v", b.brName, err)
}

func (b *linuxBridge) EnsureBridgeExists() error {
	if b.brName == "" {
		return fmt.Errorf("bridge name is empty")
	}
	br, err := linkByNameFn(b.brName)
	if err == nil {
		if br.Type() == "bridge" {
			// Bridge exists; nothing to do
			log.Infof("bridge %s found", b.brName)
			return nil
		} else {
			// a link is found with same name but not a bridge
			return fmt.Errorf("a link is found with name %s but link type is not bridge", b.brName)
		}
	} else {
		// Create new bridge
		log.Infof("bridge %s is not found. creating one", b.brName)
		return b.createBridge()
	}
}

func (b *linuxBridge) createBridge() error {

	br := &netlink.Bridge{}
	br.Name = b.brName

	if err := linkAddFn(br); err != nil {
		return fmt.Errorf("error creating bridge %s: %s", b.brName, err.Error())
	}

	if err := linkSetUpFn(br); err != nil {
		return fmt.Errorf("error bringing bridge %s up: %s", b.brName, err.Error())
	}

	return nil
}

func (b *linuxBridge) AddPort(portName string) error {
	link, err := linkByNameFn(portName)
	if err != nil {
		return fmt.Errorf("unable to find vlan interface: %s, because: %w", portName, err)
	}

	vLink, ok := link.(*netlink.Vlan)
	if !ok {
		return fmt.Errorf("interface %s type is not vlan type", portName)
	}

	br, err := linkByNameFn(b.brName)
	if err != nil {
		return fmt.Errorf("unable to find bridge %s: %w", b.brName, err)
	}

	if err := linkSetMasterFn(vLink, br); err != nil {
		return fmt.Errorf("error adding vlan interface %s to bridge %s: %s", portName, b.brName, err.Error())
	}

	if err := linkSetUpFn(vLink); err != nil {
		return fmt.Errorf("error bringing interface %s up: %s", portName, err.Error())
	}
	log.WithField("portName", portName).Infof("port added to linux bridge %s", b.brName)

	return nil
}

func (b *linuxBridge) DeletePort(portName string) error {

	link, err := linkByNameFn(portName)
	if err != nil {
		return fmt.Errorf("unable to find vlan interface: %s, because: %w", portName, err)
	}

	vLink, ok := link.(*netlink.Vlan)
	if !ok {
		return fmt.Errorf("interface %s type is not vlan type", portName)
	}

	if err := linkSetNoMasterFn(vLink); err != nil {
		return fmt.Errorf("error removing vlan interface %s from bridge %s: %s", portName, b.brName, err.Error())
	}

	if err := linkSetDownFn(vLink); err != nil {
		return fmt.Errorf("error bringing interface %s down: %s", portName, err.Error())
	}
	log.WithField("portName", portName).Infof("port deleted from linux bridge %s", b.brName)

	return nil
}
