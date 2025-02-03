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
	"context"
	"fmt"
	"strconv"

	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/p4rtclient"
	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/types"

	pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	outerVlanId = 0 // hardcoded s-tag
)

var intfMapInit bool = false

// Note: 3 reserved(last digit of interface name, for example, enp0s1f0d8, is 8) in exlude list in deviceplugin.
var interfaces [3]string = [3]string{"6", "7", "8"}
var intfMap map[string]bool

func initMap() error {
	if intfMapInit == false {
		intfMap = make(map[string]bool)
		for _, intf := range interfaces {
			intfMap[intf] = false
		}
		if len(interfaces) != len(intfMap) {
			log.Errorf("initMap setup error\n")
			return fmt.Errorf("initMap setup error\n")
		}
		intfMapInit = true
	}
	return nil
}

// in-order(sorted by interface name->interfaces) allocation, based on available ACC interfaces(for Host VF)
func allocateAccInterface() (error, string) {
	var intfName string = ""
	log.Debugf("allocateAccInterface\n")
	if intfMapInit == false {
		initMap()
	}
	for _, key := range interfaces {
		log.Debugf("intfName->%v\n", key)
		value, present := intfMap[key]
		if present == true && value == false {
			log.Debugf("Found avail Intf->%v: \n", key)
			intfMap[key] = true
			intfName = key
			break
		}
	}
	if intfName != "" {
		return nil, intfName
	}
	log.Errorf("Interface not available\n")
	return fmt.Errorf("Interface not available\n"), intfName
}

func freeAccInterface(intfName string) error {
	log.Debugf("freeAccInterface\n")
	value, present := intfMap[intfName]
	if present == true && value == true {
		log.Debugf("Found allocated Intf->%v: \n", intfName)
		intfMap[intfName] = false
		return nil
	}
	log.Errorf("Interface->%s not found in freeAccInterface\n", intfName)
	return fmt.Errorf("Interface->%s not found in freeAccInterface\n", intfName)
}

// CreateBridgePort executes the creation of the port
func (s *server) CreateBridgePort(_ context.Context, in *pb.CreateBridgePortRequest) (*pb.BridgePort, error) {
	s.log.WithField("CreateBridgePortRequest", in).Debug("CreateBridgePort")
	if !InitAccApfMacs {
		log.Errorf("CreateBridgePort: AccApfs info not set, thro-> setupAccApfs")
		return nil, fmt.Errorf("CreateBridgePort: AccApfs info not set, thro-> setupAccApfs")
	}
	// The assumption here is that the second octet is the VSI number.
	// e.g.; a mac address of 00:08:00:00:03:14 the corresponding VSI is 08.
	// VSI = 0 should be invalid and this function will return 0 when there's an error converting
	// this octet to int value
	macAddrSize := len(in.BridgePort.Spec.MacAddress)
	if macAddrSize < 1 || macAddrSize > 6 {
		// We do not have a valid mac address
		return nil, fmt.Errorf("invalid mac address provided")
	}
	vfVsi := int(in.BridgePort.Spec.MacAddress[1])
	if in.BridgePort.Spec.LogicalBridges == nil || len(in.BridgePort.Spec.LogicalBridges) < 1 {
		return nil, fmt.Errorf("vlan id is not provided")
	}
	vlan := s.getFirstVlanID(in.BridgePort.Spec.LogicalBridges)

	if vlan < 2 || vlan > 4094 {
		s.log.WithField("vlan", vlan).Debug("invalid vlan")
		return nil, fmt.Errorf("invalid vlan %d, vlan must be within 2-4094 range", vlan)
	}

	if vfVsi < 1 {
		s.log.WithField("vfVsi", vfVsi).Debug("invalid VSI")
		return nil, fmt.Errorf("invalid VSI:%d in given mac address, the value in 2nd octed must be > 0", vfVsi)
	}

	if isBridgePortPresent(*s, in.BridgePort.Name) {
		return s.Ports[in.BridgePort.Name].PbBrPort, nil
	}

	CheckAndAddPeerToPeerP4Rules(s.p4rtClient)

	err, intfName := allocateAccInterface()
	if err != nil {
		return nil, fmt.Errorf("error from allocateAccInterface->%v", err)
	}

	intIndex, err := strconv.Atoi(string(intfName))
	if err != nil {
		log.Errorf("error->%v converting, intfName->%v", err, intfName)
		return nil, fmt.Errorf("error->%v converting, intfName->%v", err, intfName)
	} else {
		log.Infof("intIndex->%v, fullIntfName->%v", intIndex, AccIntfNames[intIndex])
	}

	if err := s.bridgeCtlr.AddPort(AccIntfNames[intIndex]); err != nil {
		log.Errorf("failed to add port to bridge: %v, for interface->%v", err, AccIntfNames[intIndex])
		freeAccInterface(intfName)
		return nil, fmt.Errorf("failed to add port to bridge: %v, for interface->%v", err, AccIntfNames[intIndex])
	}

	// Add FXP rules
	log.Infof("AddHostVfP4Rules, path->%s, 1->%v, 2->%v", s.p4rtClient.GetBin(), in.BridgePort.Spec.MacAddress, AccApfMacList[intIndex])
	p4rtclient.AddHostVfP4Rules(s.p4rtClient, in.BridgePort.Spec.MacAddress, AccApfMacList[intIndex])

	resp := proto.Clone(in.BridgePort).(*pb.BridgePort)
	resp.Status = &pb.BridgePortStatus{OperStatus: pb.BPOperStatus_BP_OPER_STATUS_UP}
	pbBridgePortInfo := &types.BridgePortInfo{PbBrPort: resp, PortInterface: intfName}
	s.Ports[in.BridgePort.Name] = pbBridgePortInfo
	return resp, nil
}

// isBridgePortPresent checks if the bridge port is present
func isBridgePortPresent(srv server, brPortName string) bool {
	_, ok := srv.Ports[brPortName]
	return ok
}

// DeleteBridgePort deletes a port
func (s *server) DeleteBridgePort(_ context.Context, in *pb.DeleteBridgePortRequest) (*emptypb.Empty, error) {
	s.log.WithField("DeleteBridgePortRequest", in).Info("DeleteBridgePort")

	if !InitAccApfMacs {
		log.Errorf("DeleteBridgePort: AccApfs info not set, thro-> setupAccApfs")
		return nil, fmt.Errorf("DeleteBridgePort: AccApfs info not set, thro-> setupAccApfs")
	}
	var portInfo *pb.BridgePort
	brPortInfo, ok := s.Ports[in.Name]
	if !ok {
		s.log.WithField("interface name", in.Name).Info("port info is not found")
		// in such case avoid delete call loop from CNI Agent which otherwise will repeatedly call DeleteBridgePort as retry
		return &emptypb.Empty{}, nil
	}
	portInfo = brPortInfo.PbBrPort

	intIndex, err := strconv.Atoi(string(brPortInfo.PortInterface))
	if err != nil {
		log.Errorf("error->%v converting, intfName->%v", err, brPortInfo.PortInterface)
		return nil, fmt.Errorf("error->%v converting, intfName->%v", err, brPortInfo.PortInterface)
	} else {
		log.Infof("intIndex->%v, fullIntfName->%v", intIndex, AccIntfNames[intIndex])
	}

	if err := s.bridgeCtlr.DeletePort(AccIntfNames[intIndex]); err != nil {
		log.Errorf("unable to delete port from bridge: %v, for interface->%v", err, AccIntfNames[intIndex])
		return nil, fmt.Errorf("unable to delete port from bridge: %v, for interface->%v", err, AccIntfNames[intIndex])
	}
	freeAccInterface(brPortInfo.PortInterface)
	// Delete FXP rules
	log.Infof("DeleteHostVfP4Rules, path->%s, 1->%v, 2->%v", s.p4rtClient.GetBin(), portInfo.Spec.MacAddress, AccApfMacList[intIndex])
	p4rtclient.DeleteHostVfP4Rules(s.p4rtClient, portInfo.Spec.MacAddress, AccApfMacList[intIndex])

	delete(s.Ports, in.Name)
	return &emptypb.Empty{}, nil
}

// UpdateBridgePort updates an Nvme Subsystem
func (s *server) UpdateBridgePort(_ context.Context, in *pb.UpdateBridgePortRequest) (*pb.BridgePort, error) {
	s.log.WithField("UpdateBridgePortRequest", in).Info("UpdateBridgePort")
	return &pb.BridgePort{}, nil
}

// GetBridgePort gets an BridgePort
func (s *server) GetBridgePort(_ context.Context, in *pb.GetBridgePortRequest) (*pb.BridgePort, error) {
	s.log.WithField("GetBridgePortRequest", in).Info("GetBridgePort")
	return &pb.BridgePort{Name: in.Name, Spec: &pb.BridgePortSpec{}}, nil
}

// GetBridgePort gets an BridgePort
func (s *server) ListBridgePorts(_ context.Context, in *pb.ListBridgePortsRequest) (*pb.ListBridgePortsResponse, error) {
	s.log.WithField("ListBridgePortsRequest", in).Info("ListBridgePorts")
	return &pb.ListBridgePortsResponse{}, nil
}

func (s *server) getFirstVlanID(bridges []string) int {
	vlanId, err := strconv.Atoi(bridges[0])
	if err != nil {
		s.log.Errorf("unable to parse vlan ID %s. conversion error %s", bridges[0], err)
		return 0
	}
	return vlanId
}
