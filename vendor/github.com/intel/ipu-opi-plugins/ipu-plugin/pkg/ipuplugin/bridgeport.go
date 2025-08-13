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
	"sync"

	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/p4rtclient"
	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/types"

	pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

/*
interfaces slice will be populated with PortIds on ACC(port representators->PRs on ACC, for Host VFs),
for example, if ACC interface name is enp0s1f0d4, PortId(vportId) will be 4.
Will also include port representators->PRs needed for Network Functions.
interfaces = {HOST_VF_START_ID(22) to HOST_VF_END_ID(37), NF_PR_START_ID(6) to NF_PR_END_ID(13)}
intfMap is a map, that has key:value, between interfaceId and whether it is available(true or false) for use.
*/
var interfaces []uint
var intfMap map[uint]bool
var intfMapInit bool = false

// through bridgeport and network function service APIs(grpc),
// resource allocation apis can get invoked concurrently.
// serialize access to resources-> intfMap and interfaces
var ResourceMutex sync.Mutex

func initMap() error {
	var index uint
	if !intfMapInit {
		for index = HOST_VF_START_ID; index <= HOST_VF_END_ID; index = index + 1 {
			interfaces = append(interfaces, index)
		}
		for index = NF_PR_START_ID; index <= NF_PR_END_ID; index = index + 1 {
			interfaces = append(interfaces, index)
		}
		intfMap = make(map[uint]bool)
		for _, intf := range interfaces {
			intfMap[intf] = false
		}
		if len(interfaces) != len(intfMap) {
			log.Errorf("initMap setup error")
			return fmt.Errorf("initMap setup error")
		}
		intfMapInit = true
	}
	return nil
}

// in-order(sorted by interface IDs) allocation. Based on available ACC interfaces(for Host VF
// and NF PRs). Currently there are 2 ranges, first range(sorted) is for available Host-VF interface IDs
// (HOST_VF_START_ID to HOST_VF_END_ID) and second range(sorted) for NF PRs(NF_PR_START_ID to NF_PR_END_ID)
func AllocateAccInterface(allocPr string) (uint, error) {
	var intfId uint = 0
	start, end := 0, 0

	ResourceMutex.Lock()
	defer ResourceMutex.Unlock()

	found := false
	log.Debugf("AllocateAccInterface\n")
	if !intfMapInit {
		initMap()
	}
	if allocPr == types.HostVfPr {
		start = 0
		end = HOST_VF_END_ID - HOST_VF_START_ID
	} else {
		start = HOST_VF_END_ID - HOST_VF_START_ID + 1
		end = start + NF_PR_END_ID - NF_PR_START_ID
	}
	for i := start; i <= end; i++ {
		key := interfaces[i]
		value, present := intfMap[key]
		if present && !value {
			log.Debugf("Found avail Intf->%v: \n", key)
			intfMap[key] = true
			intfId = key
			found = true
			break
		}
	}
	if found {
		return intfId, nil
	}
	log.Errorf("AllocateAccInterface: Interface not available")
	return intfId, fmt.Errorf("AllocateAccInterface: interface not available")
}

func FreeAccInterface(intfId uint) error {
	log.Debugf("FreeAccInterface\n")

	ResourceMutex.Lock()
	defer ResourceMutex.Unlock()

	value, present := intfMap[intfId]
	if present && value {
		log.Debugf("Found allocated Intf->%v: \n", intfId)
		intfMap[intfId] = false
		return nil
	}
	log.Errorf("Interface->%v not found in FreeAccInterface", intfId)
	return fmt.Errorf("interface->%v not found in FreeAccInterface", intfId)
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

	if vfVsi < 1 {
		s.log.WithField("vfVsi", vfVsi).Debug("invalid VSI")
		return nil, fmt.Errorf("invalid VSI:%d in given mac address, the value in 2nd octed must be > 0", vfVsi)
	}

	if isBridgePortPresent(*s, in.BridgePort.Name) {
		return s.Ports[in.BridgePort.Name].PbBrPort, nil
	}

	CheckAndAddPeerToPeerP4Rules(s.p4rtClient)

	intfId, err := AllocateAccInterface(types.HostVfPr)
	if err != nil {
		return nil, fmt.Errorf("error from AllocateAccInterface->%v", err)
	}

	log.Infof("intfId->%v, fullIntfName->%v", intfId, AccApfInfo[intfId].Name)

	if err := s.bridgeCtlr.AddPort(AccApfInfo[intfId].Name); err != nil {
		log.Errorf("failed to add port to bridge: %v, for interface->%v", err, AccApfInfo[intfId].Name)
		FreeAccInterface(intfId)
		return nil, fmt.Errorf("failed to add port to bridge: %v, for interface->%v", err, AccApfInfo[intfId].Name)
	}

	// Add FXP rules
	log.Infof("AddHostVfP4Rules, path->%s, 1->%v, 2->%v", s.p4rtClient.GetBin(), in.BridgePort.Spec.MacAddress, AccApfInfo[intfId].Mac)
	err = p4rtclient.AddHostVfP4Rules(s.p4rtClient, in.BridgePort.Spec.MacAddress, AccApfInfo[intfId].Mac)

	if err != nil {
		log.Errorf("CBP: err-> %v, from AddHostVfP4Rules", err)
		DeletePortWrapper(s.bridgeCtlr, intfId)
		FreeAccInterface(intfId)
		return nil, fmt.Errorf("CBP: err-> %v, from AddHostVfP4Rules", err)
	}

	resp := proto.Clone(in.BridgePort).(*pb.BridgePort)
	resp.Status = &pb.BridgePortStatus{OperStatus: pb.BPOperStatus_BP_OPER_STATUS_UP}
	pbBridgePortInfo := &types.BridgePortInfo{PbBrPort: resp, PortId: intfId}
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

	FreeAccInterface(brPortInfo.PortId)
	delete(s.Ports, in.Name)

	portInfo = brPortInfo.PbBrPort

	intfId := brPortInfo.PortId
	log.Infof("intfIndex->%v, fullIntfName->%v", intfId, AccApfInfo[intfId].Name)

	if err := s.bridgeCtlr.DeletePort(AccApfInfo[intfId].Name); err != nil {
		log.Errorf("unable to delete port from bridge: %v, for interface->%v", err, AccApfInfo[intfId].Name)
		return nil, fmt.Errorf("unable to delete port from bridge: %v, for interface->%v", err, AccApfInfo[intfId].Name)
	}

	// Delete FXP rules
	log.Infof("DeleteHostVfP4Rules, path->%s, 1->%v, 2->%v", s.p4rtClient.GetBin(), portInfo.Spec.MacAddress, AccApfInfo[intfId].Mac)
	p4rtclient.DeleteHostVfP4Rules(s.p4rtClient, portInfo.Spec.MacAddress, AccApfInfo[intfId].Mac)

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

func DeletePortWrapper(bridgeCtlr types.BridgeController, intfId uint) error {
	if err := bridgeCtlr.DeletePort(AccApfInfo[intfId].Name); err != nil {
		log.Errorf("deletePortWrapper:failed to delete port to bridge: %v, for interface->%v", err, AccApfInfo[intfId].Name)
		return err
	}
	return nil
}
