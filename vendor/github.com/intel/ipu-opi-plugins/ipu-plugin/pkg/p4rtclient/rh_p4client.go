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

package p4rtclient

import (
	"fmt"
	"strings"

	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/types"
	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/utils"
	log "github.com/sirupsen/logrus"
)

const (
	stag          = 0 // Using single flat L2 network with vlan 0
	portMuxModPtr = 1
)

type rhP4Client struct {
	p4rtBin    string
	p4rtIpPort string
	portMuxVsi int
	p4br       string
	bridgeType types.BridgeType
}

func NewRHP4Client(p4rtBin string, p4rtIpPort string, portMuxVsi int, p4BridgeName string, brType types.BridgeType) types.P4RTClient {
	log.Debug("Creating Redhat P4Client instance")
	return &rhP4Client{
		p4rtBin:    p4rtBin,
		p4rtIpPort: p4rtIpPort,
		portMuxVsi: portMuxVsi,
		p4br:       p4BridgeName,
		bridgeType: brType,
	}
}

func (p *rhP4Client) GetBin() string {
	return p.p4rtBin
}
func (p *rhP4Client) GetIpPort() string {
	return p.p4rtIpPort
}
func (p *rhP4Client) ProgramFXPP4Rules(ruleSets []types.FxpRuleBuilder) error {
	// Dummy func for interface
	return nil
}

func (p *rhP4Client) AddRules(macAddr []byte, vlan int) {
	// For all rules  in RuleSets call
	// P4CP_INSTALL/bin/p4rt-ctl add-entry br0

	ruleSets := p.getAddRuleSets(macAddr, vlan)
	log.WithField("number of rules", len(ruleSets)).Debug("adding FXP rules")

	for _, r := range ruleSets {
		if err := utils.RunP4rtCtlCommand(p.p4rtBin, p.p4rtIpPort, r...); err != nil {
			log.WithField("error", err).Errorf("error executing add rule command")
		}
	}
	log.Info("FXP rules were added")
}

func (p *rhP4Client) DeleteRules(macAddr []byte, vlan int) {
	// For all rules  in RuleSets call
	// P4CP_INSTALL/bin/p4rt-ctl del-entry br0

	ruleSets := p.getDelRuleSets(macAddr, vlan)
	log.WithField("number of rules", len(ruleSets)).Debug("deleting FXP rules")

	for _, r := range ruleSets {
		if err := utils.RunP4rtCtlCommand(p.p4rtBin, p.p4rtIpPort, r...); err != nil {
			log.WithField("error", err).Errorf("error executing del rule command")
		}
	}
	log.Info("FXP rules were delete")
}

func (p *rhP4Client) getAddRuleSets(macAddr []byte, vlan int) []fxpRuleParams {

	macAddrSize := len(macAddr)
	if macAddrSize < 1 || macAddrSize > 6 {
		// We do not have a valid mac address
		log.WithField("mac address", macAddr).Error("Invalid mac address")
		return []fxpRuleParams{}
	}
	vfVsi := int(macAddr[1])

	vfVport := utils.GetVportForVsi(vfVsi)
	portMuxVport := utils.GetVportForVsi(p.portMuxVsi)

	ruleSets := []fxpRuleParams{
		// $P4CP_INSTALL/bin/p4rt-ctl add-entry br0 rh_mvp_control.vport_arp_egress_table "vsi=0x15,bit32_zeros=0x0000,action=rh_mvp_control.send_to_port_mux(2,30)"
		[]string{"add-entry", p.p4br, "rh_mvp_control.vport_arp_egress_table", fmt.Sprintf("vsi=%d,bit32_zeros=0x0000,action=rh_mvp_control.send_to_port_mux(%d,%d)", vfVsi, vfVsi, portMuxVport)},
		// $P4CP_INSTALL/bin/p4rt-ctl add-entry br0 rh_mvp_control.vlan_push_ctag_stag_mod_table "meta.common.mod_blob_ptr=2,action=rh_mvp_control.mod_vlan_push_ctag_stag(1,1,301,1,1,300)"
		[]string{"add-entry", p.p4br, "rh_mvp_control.vlan_push_ctag_stag_mod_table", fmt.Sprintf("meta.common.mod_blob_ptr=%d,action=rh_mvp_control.mod_vlan_push_ctag_stag(1,1,%d,1,1,%d)", vfVsi, vlan, stag)},
		// $P4CP_INSTALL/bin/p4rt-ctl add-entry br0 rh_mvp_control.portmux_egress_req_table "vsi=0xe,vid=301,action=rh_mvp_control.vlan_pop_ctag_stag(5,37)"
		[]string{"add-entry", p.p4br, "rh_mvp_control.portmux_egress_req_table", fmt.Sprintf("vsi=%d,vid=%d,action=rh_mvp_control.vlan_pop_ctag_stag(%d,%d)", p.portMuxVsi, vlan, portMuxModPtr, vfVport)},
		// $P4CP_INSTALL/bin/p4rt-ctl add-entry br0 rh_mvp_control.ingress_loopback_table "vsi=0xe,target_vsi=0x15,action=rh_mvp_control.fwd_to_port(37)"
		[]string{"add-entry", p.p4br, "rh_mvp_control.ingress_loopback_table", fmt.Sprintf("vsi=%d,target_vsi=%d,action=rh_mvp_control.fwd_to_port(%d)", p.portMuxVsi, vfVsi, vfVport)},
		// $P4CP_INSTALL/bin/p4rt-ctl add-entry br0 rh_mvp_control.portmux_egress_resp_dmac_vsi_table "vsi=0xe,dmac=0x001500000314,action=rh_mvp_control.vlan_pop_ctag_stag(5,37)"
		[]string{"add-entry", p.p4br, "rh_mvp_control.portmux_egress_resp_dmac_vsi_table", fmt.Sprintf("vsi=%d,dmac=0x%X,action=rh_mvp_control.vlan_pop_ctag_stag(%d,%d)", p.portMuxVsi, string(macAddr), portMuxModPtr, vfVport)},

		// Common Rules: These are not deleted on each DeleteBridgePort call.
		// $P4CP_INSTALL/bin/p4rt-ctl add-entry br0 rh_mvp_control.portmux_ingress_loopback_table "bit32_zeros=0x0000,action=rh_mvp_control.fwd_to_port(30)"
		[]string{"add-entry", p.p4br, "rh_mvp_control.portmux_ingress_loopback_table", fmt.Sprintf("bit32_zeros=0x0000,action=rh_mvp_control.fwd_to_port(%d)", portMuxVport)},
		// $P4CP_INSTALL/bin/p4rt-ctl add-entry br0 rh_mvp_control.vlan_pop_ctag_stag_mod_table "meta.common.mod_blob_ptr=5,action=rh_mvp_control.mod_vlan_pop_ctag_stag"
		[]string{"add-entry", p.p4br, "rh_mvp_control.vlan_pop_ctag_stag_mod_table", fmt.Sprintf("meta.common.mod_blob_ptr=%d,action=rh_mvp_control.mod_vlan_pop_ctag_stag", portMuxModPtr)},
	}

	return ruleSets
}

func (p *rhP4Client) getDelRuleSets(macAddr []byte, vlan int) []fxpRuleParams {

	macAddrSize := len(macAddr)
	if macAddrSize < 1 || macAddrSize > 6 {
		// We do not have a valid mac address
		log.WithField("mac address", macAddr).Error("Invalid mac address")
		return []fxpRuleParams{}
	}
	vfVsi := int(macAddr[1])

	ruleSets := []fxpRuleParams{
		// $P4CP_INSTALL/bin/p4rt-ctl del-entry br0 rh_mvp_control.vport_arp_egress_table "vsi=0x15,bit32_zeros=0x0000"
		[]string{"del-entry", p.p4br, "rh_mvp_control.vport_arp_egress_table", fmt.Sprintf("vsi=%d,bit32_zeros=0x0000", vfVsi)},
		// $P4CP_INSTALL/bin/p4rt-ctl del-entry br0 rh_mvp_control.vlan_push_ctag_stag_mod_table "meta.common.mod_blob_ptr=2"
		[]string{"del-entry", p.p4br, "rh_mvp_control.vlan_push_ctag_stag_mod_table", fmt.Sprintf("meta.common.mod_blob_ptr=%d", vfVsi)},
		// $P4CP_INSTALL/bin/p4rt-ctl del-entry br0 rh_mvp_control.portmux_egress_req_table "vsi=0xe,vid=301"
		[]string{"del-entry", p.p4br, "rh_mvp_control.portmux_egress_req_table", fmt.Sprintf("vsi=%d,vid=%d", p.portMuxVsi, vlan)},
		// $P4CP_INSTALL/bin/p4rt-ctl del-entry br0 rh_mvp_control.ingress_loopback_table "vsi=0xe,target_vsi=0x15"
		[]string{"del-entry", p.p4br, "rh_mvp_control.ingress_loopback_table", fmt.Sprintf("vsi=%d,target_vsi=%d", p.portMuxVsi, vfVsi)},
		// $P4CP_INSTALL/bin/p4rt-ctl del-entry br0 rh_mvp_control.portmux_egress_resp_dmac_vsi_table "vsi=0xe,dmac=0x001500000314"
		[]string{"del-entry", p.p4br, "rh_mvp_control.portmux_egress_resp_dmac_vsi_table", fmt.Sprintf("vsi=%d,dmac=0x%X", p.portMuxVsi, string(macAddr))},
	}

	return ruleSets
}

func CreateNetworkFunctionRules(p *p4rtclient, vfMacList []string, apf1 string, apf2 string) {

	ruleSets := []fxpRuleParams{}

	for i := range vfMacList {

		vfMac, err := utils.GetMacAsByteArray(vfMacList[i])
		if err != nil {
			fmt.Printf("unable to extract octets from %s: %v", vfMacList[i], err)
			return
		}

		vfDmac := strings.Replace(vfMacList[i], string(':'), "", -1)

		apf1Mac, err := utils.GetMacAsByteArray(apf1)
		if err != nil {
			fmt.Printf("unable to extract octets from apf %s: %v", apf1, err)
			return
		}

		ruleSets = append(ruleSets,
			[]string{"add-entry", "br0", "rh_mvp_control.vport_egress_vsi_table",
				fmt.Sprintf("vsi=0x%X,action=rh_mvp_control.fwd_to_port(%d)", vfMac[1], apf1Mac[1]+16)},
			[]string{"add-entry", "br0", "rh_mvp_control.ingress_loopback_table",
				fmt.Sprintf("vsi=0x%X,target_vsi=0x%X,action=rh_mvp_control.fwd_to_port(%d)", vfMac[1], apf1Mac[1], apf1Mac[1]+16)},
			[]string{"add-entry", "br0", "rh_mvp_control.ingress_loopback_table",
				fmt.Sprintf("vsi=0x%X,target_vsi=0x%X,action=rh_mvp_control.fwd_to_port(%d)", apf1Mac[1], vfMac[1], vfMac[1]+16)},
			[]string{"add-entry", "br0", "rh_mvp_control.vport_egress_dmac_vsi_table",
				fmt.Sprintf("vsi=0x%X,dmac=0x%s,action=rh_mvp_control.fwd_to_port(%d)", apf1Mac[1], vfDmac, vfMac[1]+16)},
		)
	}

	apf2Mac, err := utils.GetMacAsByteArray(apf2)
	if err != nil {
		fmt.Printf("unable to extract octets from apf %s: %v", apf2, err)
		return
	}

	ruleSets = append(ruleSets,
		[]string{"add-entry", "br0", "rh_mvp_control.vport_egress_vsi_table",
			fmt.Sprintf("vsi=0x%X,bit32_zeros=0x0000,action=rh_mvp_control.fwd_to_port(%d)", apf2Mac[1], apf2Mac[1]+16)},
		[]string{"add-entry", "br0", "rh_mvp_control.ingress_loopback_table",
			fmt.Sprintf("vsi=0x%X,target_vsi=0x%X,action=rh_mvp_control.fwd_to_port(%d)", apf2Mac[1], apf2Mac[1], apf2Mac[1]+16)},
	)

	for _, r := range ruleSets {
		if err := utils.RunP4rtCtlCommand(p.p4rtBin, p.p4rtIpPort, r...); err != nil {
			log.WithField("error", err).Errorf("error executing del rule command")
		} else {
			log.Infof("Finished running: %s", p.p4rtBin+" "+strings.Join(r, " "))
		}
	}
}

func DeleteNetworkFunctionRules(p *p4rtclient, vfMacList []string, apf1 string, apf2 string) {

	ruleSets := []fxpRuleParams{}

	for i := range vfMacList {

		vfMac, err := utils.GetMacAsByteArray(vfMacList[i])
		if err != nil {
			fmt.Printf("unable to extract octets from %s: %v", vfMacList[i], err)
			return
		}

		vfDmac := strings.Replace(vfMacList[i], string(':'), "", -1)

		apf1Mac, err := utils.GetMacAsByteArray(apf1)
		if err != nil {
			fmt.Printf("unable to extract octets from apf %s: %v", apf1, err)
			return
		}

		ruleSets = append(ruleSets,
			[]string{"del-entry", "br0", "rh_mvp_control.vport_egress_vsi_table",
				fmt.Sprintf("vsi=0x%X", vfMac[1])},
			[]string{"del-entry", "br0", "rh_mvp_control.ingress_loopback_table",
				fmt.Sprintf("vsi=0x%X,target_vsi=0x%X", vfMac[1], apf1Mac[1])},
			[]string{"del-entry", "br0", "rh_mvp_control.ingress_loopback_table",
				fmt.Sprintf("vsi=0x%X,target_vsi=0x%X", apf1Mac[1], vfMac[1])},
			[]string{"del-entry", "br0", "rh_mvp_control.vport_egress_dmac_vsi_table",
				fmt.Sprintf("vsi=0x%X,dmac=0x%s", apf1Mac[1], vfDmac)},
		)
	}

	apf2Mac, err := utils.GetMacAsByteArray(apf2)
	if err != nil {
		fmt.Printf("unable to extract octets from apf %s: %v", apf2, err)
		return
	}

	ruleSets = append(ruleSets,
		[]string{"del-entry", "br0", "rh_mvp_control.vport_egress_vsi_table",
			fmt.Sprintf("vsi=0x%X,bit32_zeros=0x0000", apf2Mac[1])},
		[]string{"del-entry", "br0", "rh_mvp_control.ingress_loopback_table",
			fmt.Sprintf("vsi=0x%X,target_vsi=0x%X", apf2Mac[1], apf2Mac[1])},
	)

	for _, r := range ruleSets {
		if err := utils.RunP4rtCtlCommand(p.p4rtBin, p.p4rtIpPort, r...); err != nil {
			log.WithField("error", err).Errorf("error executing del rule command")
		} else {
			log.Infof("Finished running: %s", p.p4rtBin+" "+strings.Join(r, " "))
		}
	}
}

/*
* The CreatePointToPointVFRules and DeletePointToPointVFRules are two functions added as a workaround
* to configure the FXP with point to point rules between the VFs initialised on a single host.
*
* These rules assume that no NF has been deployed on the FXP.
*
* Function CreatePointToPointVFRules will create all the point to point rules between all the initilised VFs on the host.
* Function DeletePointToPointVFRules will remove all the point to point rules between all the initilised VFs on the host.
 */
func CreatePointToPointVFRules(p *p4rtclient, vfMacList []string) {

	ruleSets := []fxpRuleParams{}

	for i := range vfMacList {
		for j := range vfMacList {
			if i != j {

				srcVfMac, err := utils.GetMacAsByteArray(vfMacList[i])
				if err != nil {
					fmt.Printf("unable to extract octets from %s: %v", vfMacList[i], err)
					return
				}

				dstVfMac, err := utils.GetMacAsByteArray(vfMacList[j])
				if err != nil {
					fmt.Printf("unable to extract octets from %s: %v", vfMacList[j], err)
					return
				}

				dmac := strings.Replace(vfMacList[j], string(':'), "", -1)

				ruleSets = append(ruleSets,
					[]string{"add-entry", "br0", "rh_mvp_control.ingress_loopback_table",
						fmt.Sprintf("vsi=0x%X,target_vsi=0x%X,action=rh_mvp_control.fwd_to_port(%d)", srcVfMac[1], dstVfMac[1], dstVfMac[1]+16)},
					[]string{"add-entry", "br0", "rh_mvp_control.vport_egress_dmac_vsi_table",
						fmt.Sprintf("vsi=0x%X,dmac=0x%s,action=rh_mvp_control.fwd_to_port(%d)", srcVfMac[1], dmac, dstVfMac[1]+16)},
				)
			}
		}
	}

	for _, r := range ruleSets {
		if err := utils.RunP4rtCtlCommand(p.p4rtBin, p.p4rtIpPort, r...); err != nil {
			log.WithField("error", err).Errorf("error executing del rule command")
		} else {
			log.Infof("Finished running: %s", p.p4rtBin+" "+strings.Join(r, " "))
		}
	}
}

func DeletePointToPointVFRules(p *p4rtclient, vfMacList []string) {

	ruleSets := []fxpRuleParams{}

	for i := range vfMacList {
		for j := range vfMacList {
			if i != j {

				srcVfMac, err := utils.GetMacAsByteArray(vfMacList[i])
				if err != nil {
					fmt.Printf("unable to extract octets from %s: %v", vfMacList[i], err)
					return
				}

				dstVfMac, err := utils.GetMacAsByteArray(vfMacList[j])
				if err != nil {
					fmt.Printf("unable to extract octets from %s: %v", vfMacList[j], err)
					return
				}

				dmac := strings.Replace(vfMacList[j], string(':'), "", -1)

				ruleSets = append(ruleSets,
					[]string{"del-entry", "br0", "rh_mvp_control.ingress_loopback_table",
						fmt.Sprintf("vsi=0x%X,target_vsi=0x%X", srcVfMac[1], dstVfMac[1])},
					[]string{"del-entry", "br0", "rh_mvp_control.vport_egress_dmac_vsi_table",
						fmt.Sprintf("vsi=0x%X,dmac=0x%s", srcVfMac[1], dmac)},
				)
			}
		}
	}

	for _, r := range ruleSets {
		if err := utils.RunP4rtCtlCommand(p.p4rtBin, p.p4rtIpPort, r...); err != nil {
			log.WithField("error", err).Errorf("error executing del rule command")
		} else {
			log.Infof("Finished running: %s", p.p4rtBin+" "+strings.Join(r, " "))
		}
	}
}
