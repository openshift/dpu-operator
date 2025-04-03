// Copyright (c) 2024 Intel Corporation.  All Rights Reserved.
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
	"crypto/md5"
	"encoding/hex"
	"fmt"

	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/p4rtclient"
	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/types"
	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/utils"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	PRS_LEN = 2
)

type NetworkFunctionServiceServer struct {
	pb.UnimplementedNetworkFunctionServiceServer
	Ports      map[string]*types.BridgePortInfo
	bridgeCtlr types.BridgeController
	p4rtClient types.P4RTClient
	nfReqMap   map[string][PRS_LEN]uint
}

func NewNetworkFunctionService(ports map[string]*types.BridgePortInfo, brCtlr types.BridgeController, p4Client types.P4RTClient) *NetworkFunctionServiceServer {
	return &NetworkFunctionServiceServer{
		Ports:      ports,
		bridgeCtlr: brCtlr,
		p4rtClient: p4Client,
		nfReqMap:   make(map[string][PRS_LEN]uint),
	}
}

func AllocateAccInterfaceForNF() ([PRS_LEN]uint, error) {
	var intfIds [PRS_LEN]uint

	log.Debugf("AllocateAccInterfaceForNF\n")
	for i := 0; i < PRS_LEN; i++ {
		intfId, err := AllocateAccInterface(types.NfPr)
		if err != nil {
			return intfIds, fmt.Errorf("error from AllocateAccInterface->%v", err)
		}
		intfIds[i] = intfId
	}
	log.Infof("AllocateAccInterfaceForNF: Interfaces allocated->%v\n", intfIds)
	return intfIds, nil
}

func FreeAccInterfaceForNF(intfIds [PRS_LEN]uint) error {

	log.Debugf("FreeAccInterfaceForNF, intfIds->%v\n", intfIds)
	for i := 0; i < PRS_LEN; i++ {
		err := FreeAccInterface(intfIds[i])
		if err != nil {
			log.Errorf("error from AllocateAccInterface->%v", err)
		}
	}
	return nil
}

func deriveKey(in *pb.NFRequest) string {
	nfReqHash := md5.Sum([]byte(in.Input + in.Output))
	nfReqHashStr := hex.EncodeToString(nfReqHash[:])
	log.Infof("deriveKey->%s", nfReqHashStr)
	return nfReqHashStr
}

func (s *NetworkFunctionServiceServer) CreateNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {

	mapKey := deriveKey(in)
	_, ok := s.nfReqMap[mapKey]
	if ok {
		log.Errorf("CNF:in->%s, out->%s, key->%v, exists in map", in.Input, in.Output, mapKey)
		return nil, fmt.Errorf("CNF:in->%s, out->%s, key->%v, exists in map", in.Input, in.Output, mapKey)
	}

	vfMacList, err := utils.GetVfMacList()
	if err != nil {
		log.Errorf("CreateNetworkFunction: Error-> %v", err)
		return nil, status.Errorf(codes.Internal, "Error-> %v", err)
	}

	CheckAndAddPeerToPeerP4Rules(s.p4rtClient)

	intfIds, err := AllocateAccInterfaceForNF()
	if err != nil {
		log.Errorf("error from AllocateAccInterfaceForNF: %v, intfIds->%v", err, intfIds)
		return nil, fmt.Errorf("error from AllocateAccInterfaceForNF: %v, intfIds->%v", err, intfIds)
	}
	NF_IN_PR := intfIds[0]
	NF_OUT_PR := intfIds[1]
	log.Infof("CNF: allocated NF PRs index (IN)->%v, OUT->%v", NF_IN_PR, NF_OUT_PR)

	if err := s.bridgeCtlr.AddPort(AccApfInfo[NF_IN_PR].Name); err != nil {
		log.Errorf("failed to add port to bridge: %v, for interface->%v", err, AccApfInfo[NF_IN_PR].Name)
		FreeAccInterfaceForNF(intfIds)
		return nil, fmt.Errorf("failed to add port to bridge: %v, for interface->%v", err, AccApfInfo[NF_IN_PR].Name)
	}
	if err := s.bridgeCtlr.AddPort(AccApfInfo[NF_OUT_PR].Name); err != nil {
		log.Errorf("failed to add port to bridge: %v, for interface->%v", err, AccApfInfo[NF_OUT_PR].Name)
		DeletePortWrapper(s.bridgeCtlr, NF_IN_PR)
		FreeAccInterfaceForNF(intfIds)
		return nil, fmt.Errorf("failed to add port to bridge: %v, for interface->%v", err, AccApfInfo[NF_OUT_PR].Name)
	}
	log.Infof("added interfaces:inPR->%s, outPR->%s", AccApfInfo[NF_IN_PR].Name, AccApfInfo[NF_OUT_PR].Name)
	/*Note: Currently this API does not have host-VF info, since there is no reference to what was passed by DPU in CreateBridgePort.
	As a work-around, we take full vfMacList, and write P4 rules, to connect all host VFs to NF.	*/
	// Generate the P4 rules and program the FXP with NF comms
	log.Infof("AddNFP4Rules, path->%s, 1-%v, 2-%v, 3-%v, 4-%v, 5-%v",
		s.p4rtClient.GetBin(), vfMacList, in.Input, in.Output, AccApfInfo[NF_IN_PR].Mac, AccApfInfo[NF_OUT_PR].Mac)
	err = p4rtclient.AddNFP4Rules(s.p4rtClient, vfMacList, in.Input, in.Output, AccApfInfo[NF_IN_PR].Mac, AccApfInfo[NF_OUT_PR].Mac)
	if err != nil {
		log.Errorf("err-> %v, from AddNFP4Rules", err)
		DeletePortWrapper(s.bridgeCtlr, NF_IN_PR)
		DeletePortWrapper(s.bridgeCtlr, NF_OUT_PR)
		FreeAccInterfaceForNF(intfIds)
		return nil, fmt.Errorf("err-> %v, from AddNFP4Rules", err)
	}

	s.nfReqMap[deriveKey(in)] = intfIds

	return &pb.Empty{}, nil
}

func (s *NetworkFunctionServiceServer) DeleteNetworkFunction(ctx context.Context, in *pb.NFRequest) (*pb.Empty, error) {

	mapKey := deriveKey(in)
	intfIds, ok := s.nfReqMap[mapKey]
	if !ok {
		log.Errorf("DNF:in->%s, out->%s, key->%v, not found in map", in.Input, in.Output, mapKey)
		return nil, fmt.Errorf("DNF:in->%s, out->%s, key->%v, not found in map", in.Input, in.Output, mapKey)
	}

	vfMacList, err := utils.GetVfMacList()

	if err != nil {
		log.Errorf("DeleteNetworkFunction: Error-> %v", err)
		return nil, status.Errorf(codes.Internal, "Error-> %v", err)
	}

	NF_IN_PR := intfIds[0]
	NF_OUT_PR := intfIds[1]
	log.Infof("DNF: NF PRs index (IN)->%v, OUT->%v", NF_IN_PR, NF_OUT_PR)

	FreeAccInterfaceForNF(intfIds)
	delete(s.nfReqMap, mapKey)

	if err := s.bridgeCtlr.DeletePort(AccApfInfo[NF_IN_PR].Name); err != nil {
		log.Errorf("failed to delete port to bridge: %v, for interface->%v", err, AccApfInfo[NF_IN_PR].Name)
		return nil, fmt.Errorf("failed to add port to bridge: %v, for interface->%v", err, AccApfInfo[NF_IN_PR].Name)
	}
	if err := s.bridgeCtlr.DeletePort(AccApfInfo[NF_OUT_PR].Name); err != nil {
		log.Errorf("failed to delete port to bridge: %v, for interface->%v", err, AccApfInfo[NF_OUT_PR].Name)
		return nil, fmt.Errorf("failed to add port to bridge: %v, for interface->%v", err, AccApfInfo[NF_OUT_PR].Name)
	}
	log.Infof("deleted interfaces:inPR->%s, outPR->%s", AccApfInfo[NF_IN_PR].Name, AccApfInfo[NF_OUT_PR].Name)

	log.Infof("DeleteNFP4Rules, path->%s, 1-%v, 2-%v, 3-%v, 4-%v, 5-%v",
		s.p4rtClient.GetBin(), vfMacList, in.Input, in.Output, AccApfInfo[NF_IN_PR].Mac, AccApfInfo[NF_OUT_PR].Mac)
	p4rtclient.DeleteNFP4Rules(s.p4rtClient, vfMacList, in.Input, in.Output, AccApfInfo[NF_IN_PR].Mac, AccApfInfo[NF_OUT_PR].Mac)

	return &pb.Empty{}, nil
}
