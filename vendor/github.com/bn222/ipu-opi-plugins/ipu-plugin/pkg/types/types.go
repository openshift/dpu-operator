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

package types

import (
	pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
)

type BridgeType int

type BridgePortInfo struct {
	PbBrPort      *pb.BridgePort
	PortInterface string
}

const (
	OvsBridge BridgeType = iota
	LinuxBridge
	HostMode = "host"
	IpuMode  = "ipu"
)

func (b BridgeType) String() string {
	switch b {
	case OvsBridge:
		return "ovs"
	case LinuxBridge:
		return "linux"
	}
	return "unknown"
}

type Runnable interface {
	Run() error
	Stop()
}

// BridgeController is an interface to interact with bridge provider to add remove host interface to it.
type BridgeController interface {
	// EnsureBridgeExists checks for the bridge that the controller is going to manage. It will attempt to
	// create one if it doesn't exist.
	EnsureBridgeExists() error
	// DeleteBridges deletes the bridges managed.
	DeleteBridges() error
	// AddPort will add host interface "portName" to the bridge that this BridgeController is managing
	AddPort(portName string) error
	// DeletePort will remove a port "portName" from the bridge that this BridgeController is managing
	DeletePort(portName string) error
}

type P4RTClient interface {
	AddRules(macAddr []byte, vlan int)
	DeleteRules(macAddr []byte, vlan int)
}
