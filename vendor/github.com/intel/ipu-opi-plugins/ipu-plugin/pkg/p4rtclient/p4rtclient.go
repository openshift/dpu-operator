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
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/types"
	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/utils"
	log "github.com/sirupsen/logrus"
)

const (
	mirror_profile_id = 3
	bridgeId          = 0
	phyPort           = 0
	imcAddress        = "192.168.0.1:22"
	defaultP4rtIp     = "127.0.0.1"
	p4rtPort          = "9559"
)

type fxpRuleParams []string

type p4rtclient struct {
	p4rtBin         string
	p4rtIpPort      string
	p4rtServiceName string
	portMuxVsi      int
	p4br            string
	bridgeType      types.BridgeType
}

func NewP4RtClient(p4rtBin string, p4rtServiceName string, portMuxVsi int, p4BridgeName string, brType types.BridgeType) types.P4RTClient {
	log.Debug("Creating Linux P4Client instance")
	p4rtClient := p4rtclient{
		p4rtBin:         p4rtBin,
		p4rtServiceName: p4rtServiceName,
		portMuxVsi:      portMuxVsi,
		p4br:            p4BridgeName,
		bridgeType:      brType,
	}
	p4rtClient.ResolveServiceIp(false)
	return &p4rtClient
}

// TODO: Move this under utils pkg
func checkMacAddresses(macAddresses ...string) ([]byte, error) {
	for _, mac := range macAddresses {
		hwAddr, err := net.ParseMAC(mac)
		if err != nil {
			return hwAddr, errors.New("Invalid Mac Address format")
		}
	}
	return []byte{}, nil
}

func (p *p4rtclient) ProgramFXPP4Rules(ruleSets []types.FxpRuleBuilder) error {
	for _, r := range ruleSets {
		p4rule := []string{r.Action, r.P4br, r.Control, r.Metadata}
		stderr, stdout, err := utils.RunP4rtCtlCommand(p.p4rtBin, p.p4rtIpPort, p4rule...)
		if err != nil {
			if strings.Contains(stdout, "ALREADY_EXISTS") {
				log.Info("p4rule already exists. Delete and reprogram: ", p4rule)
				metaPrefix := strings.Index(r.Metadata, ",action")
				p4DelR := types.FxpRuleBuilder{Action: "del-entry", P4br: r.P4br, Control: r.Control, Metadata: r.Metadata[:metaPrefix]}
				p4DelRuleStr := []string{p4DelR.Action, p4DelR.P4br, p4DelR.Control, p4DelR.Metadata}
				// Delete the existing rule first
				utils.RunP4rtCtlCommand(p.p4rtBin, p.p4rtIpPort, p4DelRuleStr...)
				time.Sleep(1)
				// Now program the new rule
				utils.RunP4rtCtlCommand(p.p4rtBin, p.p4rtIpPort, p4rule...)
			} else if strings.Contains(stderr, "INVALID_ARGUMENT") {
				// This is to handle possible race condition during the p4rt-ctl add or a delete command execution failure.
				log.Info("p4rule add or a delete operation failed. Attempt once again", p4rule)
				// Sleep for a second and attempt only once to reprogram.
				time.Sleep(1)
				utils.RunP4rtCtlCommand(p.p4rtBin, p.p4rtIpPort, p4rule...)
			} else {
				log.Info("WARNING!: p4rule add or a delete operation failed for an unhandled error scenario")
			}

		}
	}
	return nil
}

func getStrippedMacAndVsi(macAddr string) (int, string, error) {
	vsi, err := utils.ImcQueryfindVsiGivenMacAddr(types.IpuMode, macAddr)
	if err != nil {
		log.Info("programRHPrimarySecondaryVportP4Rules failed. Unable to find Vsi and Vport for PR mac: ", macAddr)
		return 0, "", err
	}
	//skip 0x in front of vsi
	vsi = vsi[2:]

	vsiInt64, err := strconv.ParseInt(vsi, 16, 32)
	if err != nil {
		log.Info("error from ParseInt ", err)
		return 0, "", err
	}
	Vsi := int(vsiInt64)
	macAddrStrip := strings.ReplaceAll(macAddr, ":", "")
	return Vsi, macAddrStrip, nil
}

func programPhyVportP4Rules(p4rtClient types.P4RTClient, phyPort int, prMac string) error {
	vsi, err := utils.ImcQueryfindVsiGivenMacAddr(types.IpuMode, prMac)
	if err != nil {
		log.Info("AddPhyPortRules failed. Unable to find Vsi and Vport for PR mac: ", prMac)
		return err
	}
	//skip 0x in front of vsi
	vsi = vsi[2:]
	vsiInt64, err := strconv.ParseInt(vsi, 16, 32)
	if err != nil {
		log.Info("error from ParseInt ", err)
		return err
	}
	prVsi := int(vsiInt64)

	prVport := utils.GetVportForVsi(prVsi)

	phyVportP4ruleSets := []types.FxpRuleBuilder{
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.rx_source_port",
			Metadata: fmt.Sprintf(
				"vmeta.common.port_id=%d,zero_padding=0,action=linux_networking_control.set_source_port(%d)",
				phyPort, phyPort,
			),
		},
		/* rx_phy_port_to_pr_map rule is commented here as it gets handled at the mirror_profile. keeping the below rule for reference only.
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.rx_phy_port_to_pr_map",
			Metadata: fmt.Sprintf(
				"vmeta.common.port_id=%d,zero_padding=0,action=linux_networking_control.fwd_to_vsi(%d)",
				phyPort, prVport,
			),
		},
		*/
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.source_port_to_pr_map",
			Metadata: fmt.Sprintf(
				"user_meta.cmeta.source_port=%d,zero_padding=0,action=linux_networking_control.fwd_to_vsi(%d)",
				phyPort, prVport,
			),
		},
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.tx_acc_vsi",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,zero_padding=0,action=linux_networking_control.l2_fwd_and_bypass_bridge(%d)",
				prVsi, phyPort,
			),
		},
	}
	return p4rtClient.ProgramFXPP4Rules(phyVportP4ruleSets)
}

func deletePhyVportP4Rules(p4rtClient types.P4RTClient, phyPort int, prMac string) error {
	vsi, err := utils.ImcQueryfindVsiGivenMacAddr(types.IpuMode, prMac)
	if err != nil {
		log.Info("DeletePhyPortRules failed. Unable to find Vsi and Vport for PR mac: ", prMac)
		return err
	}
	//skip 0x in front of vsi
	vsi = vsi[2:]
	vsiInt64, err := strconv.ParseInt(vsi, 16, 32)
	if err != nil {
		log.Info("error from ParseInt ", err)
		return err
	}
	prVsi := int(vsiInt64)

	phyVportP4ruleSets := []types.FxpRuleBuilder{
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.rx_source_port",
			Metadata: fmt.Sprintf(
				"vmeta.common.port_id=%d,zero_padding=0",
				phyPort,
			),
		},
		/* rx_phy_port_to_pr_map rule is commented here as it gets handled at the mirror_profile. keeping the below rule for reference only.
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.rx_phy_port_to_pr_map",
			Metadata: fmt.Sprintf(
				"vmeta.common.port_id=%d,zero_padding=0",
				phyPort,
			),
		},
		*/
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.source_port_to_pr_map",
			Metadata: fmt.Sprintf(
				"user_meta.cmeta.source_port=%d,zero_padding=0",
				phyPort,
			),
		},
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.tx_acc_vsi",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,zero_padding=0",
				prVsi,
			),
		},
	}

	return p4rtClient.ProgramFXPP4Rules(phyVportP4ruleSets)
}

func programPhyVportBridgeId(p4rtClient types.P4RTClient, phyPort, bridgeId int) error {
	phyBridgeIdP4ruleSet := []types.FxpRuleBuilder{
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.source_port_to_bridge_map",
			Metadata: fmt.Sprintf(
				"user_meta.cmeta.source_port=%d/0xffff,hdrs.vlan_ext[vmeta.common.depth].hdr.vid=0/0xfff,priority=1,action=linux_networking_control.set_bridge_id(bridge_id=%d)",
				phyPort, bridgeId,
			),
		},
	}
	return p4rtClient.ProgramFXPP4Rules(phyBridgeIdP4ruleSet)
}

func deletePhyVportBridgeId(p4rtClient types.P4RTClient, phyPort, bridgeId int) error {
	phyBridgeIdP4ruleSet := []types.FxpRuleBuilder{
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.source_port_to_bridge_map",
			Metadata: fmt.Sprintf(
				"user_meta.cmeta.source_port=%d/0xffff,hdrs.vlan_ext[vmeta.common.depth].hdr.vid=0/0xfff,priority=1",
				phyPort,
			),
		},
	}
	return p4rtClient.ProgramFXPP4Rules(phyBridgeIdP4ruleSet)
}

func programNfPrVportP4Rules(p4rtClient types.P4RTClient, ingressMac, egressMac string) error {
	ingressVsi, ingressVport, err := utils.ExtractVsiVportInfo(ingressMac)
	if err != nil {
		return fmt.Errorf("programNfPrVportP4Rules failed. Unable to find Vsi and Vport for NF ingress mac: %v", ingressMac)
	}
	egressVsi, egressVport, err := utils.ExtractVsiVportInfo(egressMac)
	if err != nil {
		return fmt.Errorf("programNfPrVportP4Rules failed. Unable to find Vsi and Vport for NF egress mac: %v", egressMac)
	}

	nfPrVportP4RuleSets := []types.FxpRuleBuilder{
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.tx_source_port",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d/0x7ff,priority=1,action=linux_networking_control.set_source_port(%d)",
				ingressVsi, ingressVport,
			),
		},
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.tx_acc_vsi",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,zero_padding=0,action=linux_networking_control.l2_fwd_and_bypass_bridge(%d)",
				ingressVsi, egressVport,
			),
		},
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.tx_acc_vsi",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,zero_padding=0,action=linux_networking_control.l2_fwd_and_bypass_bridge(%d)",
				egressVsi, ingressVport,
			),
		},
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.vsi_to_vsi_loopback",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,target_vsi=%d,action=linux_networking_control.fwd_to_vsi(%d)",
				egressVsi, ingressVsi, ingressVport,
			),
		},
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.vsi_to_vsi_loopback",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,target_vsi=%d,action=linux_networking_control.fwd_to_vsi(%d)",
				ingressVsi, egressVsi, egressVport,
			),
		},
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.source_port_to_pr_map",
			Metadata: fmt.Sprintf(
				"user_meta.cmeta.source_port=%d,zero_padding=0,action=linux_networking_control.fwd_to_vsi(%d)",
				ingressVport, egressVport,
			),
		},
	}

	return p4rtClient.ProgramFXPP4Rules(nfPrVportP4RuleSets)
}

func deleteNfPrVportP4Rules(p4rtClient types.P4RTClient, ingressMac, egressMac string) error {
	ingressVsi, ingressVport, err := utils.ExtractVsiVportInfo(ingressMac)
	if err != nil {
		return fmt.Errorf("deleteNfPrVportP4Rules failed. Unable to find Vsi and Vport for NF ingress mac: %v", ingressMac)
	}
	egressVsi, _, err := utils.ExtractVsiVportInfo(egressMac)
	if err != nil {
		return fmt.Errorf("deleteNfPrVportP4Rules failed. Unable to find Vsi and Vport for NF ingress mac: %v", egressMac)
	}

	nfPrVportP4RuleSets := []types.FxpRuleBuilder{
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.tx_source_port",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d/0x7ff,priority=1",
				ingressVsi,
			),
		},
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.tx_acc_vsi",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,zero_padding=0",
				ingressVsi,
			),
		},
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.tx_acc_vsi",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,zero_padding=0",
				egressVsi,
			),
		},
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.vsi_to_vsi_loopback",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,target_vsi=%d",
				egressVsi, ingressVsi,
			),
		},
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.vsi_to_vsi_loopback",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,target_vsi=%d",
				ingressVsi, egressVsi,
			),
		},
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.source_port_to_pr_map",
			Metadata: fmt.Sprintf(
				"user_meta.cmeta.source_port=%d,zero_padding=0",
				ingressVport,
			),
		},
	}

	return p4rtClient.ProgramFXPP4Rules(nfPrVportP4RuleSets)
}

func programVsiToVsiP4Rules(p4rtClient types.P4RTClient, mac1, mac2 string) error {
	mac1Vsi, mac1Vport, err := utils.ExtractVsiVportInfo(mac1)
	if err != nil {
		return fmt.Errorf("programVsiToVsiP4Rules failed. Unable to find Vsi and Vport for mac: %v", mac1)
	}

	mac2Vsi, mac2Vport, err := utils.ExtractVsiVportInfo(mac2)
	if err != nil {
		return fmt.Errorf("programVsiToVsiP4Rules failed. Unable to find Vsi and Vport for mac: %v", mac2)
	}

	VsiToVsip4RuleSets := []types.FxpRuleBuilder{
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.vsi_to_vsi_loopback",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,target_vsi=%d,action=linux_networking_control.fwd_to_vsi(%d)",
				mac1Vsi, mac2Vsi, mac2Vport,
			),
		},
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.vsi_to_vsi_loopback",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,target_vsi=%d,action=linux_networking_control.fwd_to_vsi(%d)",
				mac2Vsi, mac1Vsi, mac1Vport,
			),
		},
	}
	return p4rtClient.ProgramFXPP4Rules(VsiToVsip4RuleSets)
}

func deleteVsiToVsiP4Rules(p4rtClient types.P4RTClient, mac1, mac2 string) error {
	mac1Vsi, _, err := utils.ExtractVsiVportInfo(mac1)
	if err != nil {
		return fmt.Errorf("deleteVsiToVsiP4Rules failed. Unable to find Vsi and Vport for mac: %v", mac1)
	}

	mac2Vsi, _, err := utils.ExtractVsiVportInfo(mac2)
	if err != nil {
		return fmt.Errorf("deleteVsiToVsiP4Rules failed. Unable to find Vsi and Vport for mac: %v", mac2)
	}

	VsiToVsip4RuleSets := []types.FxpRuleBuilder{
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.vsi_to_vsi_loopback",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,target_vsi=%d",
				mac1Vsi, mac2Vsi,
			),
		},
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.vsi_to_vsi_loopback",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,target_vsi=%d",
				mac2Vsi, mac1Vsi,
			),
		},
	}
	return p4rtClient.ProgramFXPP4Rules(VsiToVsip4RuleSets)
}

// In case of failure, revert to using 127.0.0.1:9559 which works for P4 in container
// but not for P4 in pod. In case of P4 in pod in failure case, we will error out in the
// waitForInfraP4d()
func (p *p4rtclient) ResolveServiceIp(inCluster bool) error {
	var err error
	p4rtIp := defaultP4rtIp
	if inCluster {
		ip, err := net.LookupIP(p.p4rtServiceName)
		if err != nil {
			log.Errorf("Couldn't resolve Name %s to IP: err->%s", p.p4rtServiceName, err)
		} else {
			p4rtIp = ip[0].String()
		}
	}
	if err == nil {
		log.Infof("Setting p4runtime Ip to %s", p4rtIp)
		p.p4rtIpPort = p4rtIp + ":" + p4rtPort
	}
	return err
}

func (p *p4rtclient) GetBin() string {
	return p.p4rtBin
}
func (p *p4rtclient) GetIpPort() string {
	return p.p4rtIpPort
}

func (p *p4rtclient) AddRules(macAddr []byte, vlan int) {
	// For all rules  in RuleSets call
	// P4CP_INSTALL/bin/p4rt-ctl add-entry br0

	ruleSets := p.getAddRuleSets(macAddr, vlan)
	log.WithField("number of rules", len(ruleSets)).Debug("adding FXP rules")

	for _, r := range ruleSets {
		_, _, err := utils.RunP4rtCtlCommand(p.p4rtBin, p.p4rtIpPort, r...)
		if err != nil {
			log.WithField("error", err).Errorf("error executing add rule command")
		}
	}
	log.Info("FXP rules were added")
}

func (p *p4rtclient) DeleteRules(macAddr []byte, vlan int) {
	// For all rules  in RuleSets call
	// P4CP_INSTALL/bin/p4rt-ctl del-entry br0

	ruleSets := p.getDelRuleSets(macAddr, vlan)
	log.WithField("number of rules", len(ruleSets)).Debug("deleting FXP rules")

	for _, r := range ruleSets {
		_, _, err := utils.RunP4rtCtlCommand(p.p4rtBin, p.p4rtIpPort, r...)
		if err != nil {
			log.WithField("error", err).Errorf("error executing del rule command")
		}
	}
	log.Info("FXP rules were deleted")
}

func (p *p4rtclient) getAddRuleSets(macAddr []byte, vlan int) []fxpRuleParams {

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
		// Rules for control packets coming from overlay VF (vfVsi), IPU will add a VLAN tag (vlan) and send to PortMux Vport (portMuxVport)
		[]string{"add-entry", p.p4br, "linux_networking_control.handle_tx_from_host_to_ovs_and_ovs_to_wire_table", fmt.Sprintf("vmeta.common.vsi=%d,user_meta.cmeta.bit32_zeros=0,action=linux_networking_control.add_vlan_and_send_to_port(%d,%d)", vfVsi, vlan, portMuxVport)},
		[]string{"add-entry", p.p4br, "linux_networking_control.handle_tx_from_host_to_ovs_and_ovs_to_wire_table", fmt.Sprintf("vmeta.common.vsi=%d,user_meta.cmeta.bit32_zeros=0,action=linux_networking_control.add_vlan_and_send_to_port(%d,%d)", vfVsi, vlan, portMuxVport)},
		[]string{"add-entry", p.p4br, "linux_networking_control.handle_rx_loopback_from_host_to_ovs_table", fmt.Sprintf("vmeta.common.vsi=%d,user_meta.cmeta.bit32_zeros=0,action=linux_networking_control.set_dest(%d)", vfVsi, portMuxVport)},
		[]string{"add-entry", p.p4br, "linux_networking_control.vlan_push_mod_table", fmt.Sprintf("vmeta.common.mod_blob_ptr=%d,action=linux_networking_control.vlan_push(1,0,%d)", vlan, vlan)},

		// Rules for control packets coming from vlan port via PortMuxVsi(p.portMuxVsi), IPU will remove the VLAN tag (vlan) and send to overlay VF(vfVport)
		[]string{"add-entry", p.p4br, "linux_networking_control.handle_tx_from_ovs_to_host_table", fmt.Sprintf("vmeta.common.vsi=%d,hdrs.dot1q_tag[vmeta.common.depth].hdr.vid=%d,action=linux_networking_control.remove_vlan_and_send_to_port(%d,%d)", p.portMuxVsi, vlan, vlan, vfVport)},
		[]string{"add-entry", p.p4br, "linux_networking_control.handle_rx_loopback_from_ovs_to_host_table", fmt.Sprintf("vmeta.misc_internal.vm_to_vm_or_port_to_port[27:17]=%d,user_meta.cmeta.bit32_zeros=0,action=linux_networking_control.set_dest(%d)", vfVsi, vfVport)},
		[]string{"add-entry", p.p4br, "linux_networking_control.vlan_pop_mod_table", fmt.Sprintf("vmeta.common.mod_blob_ptr=%d,action=linux_networking_control.vlan_pop", vlan)},
	}
	if p.bridgeType == types.LinuxBridge {
		// Add additional add rules
		macToIntValue := utils.GetMacIntValueFromBytes(macAddr)
		ruleSets = append(ruleSets,
			[]string{"add-entry", p.p4br, "linux_networking_control.l2_fwd_tx_table", fmt.Sprintf("dst_mac=0x%x,user_meta.pmeta.tun_flag1_d0=0x00,action=linux_networking_control.l2_fwd(%d)", macToIntValue, vfVport)},
			[]string{"add-entry", p.p4br, "linux_networking_control.sem_bypass", fmt.Sprintf("dst_mac=0x%x,action=linux_networking_control.set_dest(%d)", macToIntValue, vfVport)},
		)

	}
	return ruleSets
}

func (p *p4rtclient) getDelRuleSets(macAddr []byte, vlan int) []fxpRuleParams {

	macAddrSize := len(macAddr)
	if macAddrSize < 1 || macAddrSize > 6 {
		// We do not have a valid mac address
		log.WithField("mac address", macAddr).Error("Invalid mac address")
		return []fxpRuleParams{}
	}
	vfVsi := int(macAddr[1])

	ruleSets := []fxpRuleParams{
		// Rules for control packets coming from overlay VF (vfVsi), IPU will add a VLAN tag (vlan) and send to PortMux Vport (portMuxVport)
		[]string{"del-entry", p.p4br, "linux_networking_control.handle_tx_from_host_to_ovs_and_ovs_to_wire_table", fmt.Sprintf("vmeta.common.vsi=%d,user_meta.cmeta.bit32_zeros=0", vfVsi)},
		[]string{"del-entry", p.p4br, "linux_networking_control.handle_rx_loopback_from_host_to_ovs_table", fmt.Sprintf("vmeta.common.vsi=%d,user_meta.cmeta.bit32_zeros=0", vfVsi)},
		[]string{"del-entry", p.p4br, "linux_networking_control.vlan_push_mod_table", fmt.Sprintf("vmeta.common.mod_blob_ptr=%d", vlan)},

		// Rules for control packets coming from vlan port via PortMuxVsi(p.portMuxVsi), IPU will remove the VLAN tag (vlan) and send to overlay VF(vfVport)
		[]string{"del-entry", p.p4br, "linux_networking_control.handle_tx_from_ovs_to_host_table", fmt.Sprintf("vmeta.common.vsi=%d,hdrs.dot1q_tag[vmeta.common.depth].hdr.vid=%d", p.portMuxVsi, vlan)},
		[]string{"del-entry", p.p4br, "linux_networking_control.handle_rx_loopback_from_ovs_to_host_table", fmt.Sprintf("vmeta.misc_internal.vm_to_vm_or_port_to_port[27:17]=%d,user_meta.cmeta.bit32_zeros=0", vfVsi)},
		[]string{"del-entry", p.p4br, "linux_networking_control.vlan_pop_mod_table", fmt.Sprintf("vmeta.common.mod_blob_ptr=%d", vlan)},
	}
	if p.bridgeType == types.LinuxBridge {
		// Add additional deletion rules
		macToIntValue := utils.GetMacIntValueFromBytes(macAddr)
		ruleSets = append(ruleSets,
			[]string{"del-entry", p.p4br, "linux_networking_control.l2_fwd_tx_table", fmt.Sprintf("dst_mac=0x%x,user_meta.pmeta.tun_flag1_d0=0x00", macToIntValue)},
			[]string{"del-entry", p.p4br, "linux_networking_control.sem_bypass", fmt.Sprintf("dst_mac=0x%x", macToIntValue)},
		)
	}

	return ruleSets
}

// P4 APIs exposed to ipu-opi-plugins

func AddPhyPortRules(p4rtClient types.P4RTClient, prP0mac string, prP1mac string) error {
	macAddr, macErr := checkMacAddresses(prP0mac, prP1mac)
	if macErr != nil {
		// We do not have a valid mac address
		log.WithField("mac address", macAddr).Error("Invalid mac address")
		return errors.New("Invalid Mac Address")
	}
	//Add Port 0 P4 rules
	programPhyVportP4Rules(p4rtClient, 0, prP0mac)
	//Add Port 1 P4 rules
	//programPhyVportP4Rules(p4RtBin, 1, prP1mac)
	//Add bridge id for non P4 OVS bridge ports
	//programPhyVportBridgeId(p4RtBin, 1, 77)

	return nil
}

func DeletePhyPortRules(p4rtClient types.P4RTClient, prP0mac string, prP1mac string) error {
	macAddr, macErr := checkMacAddresses(prP0mac, prP1mac)
	if macErr != nil {
		// We do not have a valid mac address
		log.WithField("mac address", macAddr).Error("Invalid mac address")
		return errors.New("Invalid Mac Address")
	}
	//Add Port 0 P4 rules
	deletePhyVportP4Rules(p4rtClient, 0, prP0mac)
	//Add Port 1 P4 rules
	//deletePhyVportP4Rules(p4RtBin, 1, prP1mac)
	//Add bridge id for non P4 OVS bridge ports
	//deletePhyVportBridgeId(p4RtBin, 1, 77)

	return nil

}

func AddHostVfP4Rules(p4rtClient types.P4RTClient, hostVfMac []byte, accMac string) error {
	hostMacAddr := net.HardwareAddr(hostVfMac)
	vfMac, hostMacErr := checkMacAddresses(hostMacAddr.String())
	if hostMacErr != nil {
		// We do not have a valid mac address for the host or apf interface
		log.WithField("mac address", vfMac).Error("Invalid mac address")
		return errors.New("Invalid Mac Address")
	}

	accAddr, apfMacErr := checkMacAddresses(accMac)
	if apfMacErr != nil {
		// We do not have a valid mac address for the host or apf interface
		log.WithField("mac address", accAddr).Error("Invalid mac address")
		return errors.New("Invalid Mac Address")
	}

	hostVfVsi, hostVfVport, err := utils.ExtractVsiVportInfo(hostMacAddr.String())
	if err != nil {
		return fmt.Errorf("AddHostVfP4Rules failed. Unable to find Vsi and Vport for mac: %v", hostMacAddr.String())
	}

	apfPrVsi, apfPrVport, err := utils.ExtractVsiVportInfo(accMac)
	if err != nil {
		return fmt.Errorf("AddHostVfP4Rules failed. Unable to find Vsi and Vport for mac: %v", accMac)
	}

	hostVfP4ruleSets := []types.FxpRuleBuilder{
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.tx_source_port",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d/0x7ff,priority=1,action=linux_networking_control.set_source_port(%d)",
				hostVfVsi, hostVfVport,
			),
		},
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.tx_acc_vsi",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,zero_padding=0,action=linux_networking_control.l2_fwd_and_bypass_bridge(%d)",
				apfPrVsi, hostVfVport,
			),
		},
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.vsi_to_vsi_loopback",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,target_vsi=%d,action=linux_networking_control.fwd_to_vsi(%d)",
				apfPrVsi, hostVfVsi, hostVfVport,
			),
		},
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.vsi_to_vsi_loopback",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,target_vsi=%d,action=linux_networking_control.fwd_to_vsi(%d)",
				hostVfVsi, apfPrVsi, apfPrVport,
			),
		},
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.source_port_to_pr_map",
			Metadata: fmt.Sprintf(
				"user_meta.cmeta.source_port=%d,zero_padding=0,action=linux_networking_control.fwd_to_vsi(%d)",
				hostVfVport, apfPrVport,
			),
		},
	}

	log.WithField("number of rules", len(hostVfP4ruleSets)).Debug("adding FXP rules")

	err = p4rtClient.ProgramFXPP4Rules(hostVfP4ruleSets)
	if err != nil {
		log.Info("Host VF FXP P4 rules add failed")
		return err
	} else {
		log.Info("Host VF FXP P4 rules were added successfully")
	}
	return nil
}

func DeleteHostVfP4Rules(p4rtClient types.P4RTClient, hostVfMac []byte, accMac string) error {
	hostMacAddr := net.HardwareAddr(hostVfMac)
	vfMac, hostMacErr := checkMacAddresses(hostMacAddr.String())
	if hostMacErr != nil {
		// We do not have a valid mac address for the host or apf interface
		log.WithField("mac address", vfMac).Error("Invalid mac address")
		return errors.New("Invalid Mac Address")
	}

	accMacAddr, apfMacErr := checkMacAddresses(accMac)
	if apfMacErr != nil {
		// We do not have a valid mac address for the host or apf interface
		log.WithField("mac address", accMacAddr).Error("Invalid mac address")
		return errors.New("Invalid Mac Address")
	}

	hostVfVsi, hostVfVport, err := utils.ExtractVsiVportInfo(hostMacAddr.String())
	if err != nil {
		return fmt.Errorf("DeleteHostVfP4Rules failed. Unable to find Vsi and Vport for mac: %v", hostMacAddr.String())
	}

	apfPrVsi, _, err := utils.ExtractVsiVportInfo(accMac)
	if err != nil {
		return fmt.Errorf("DeleteHostVfP4Rules failed. Unable to find Vsi and Vport for mac: %v", accMac)
	}

	hostVfP4ruleSets := []types.FxpRuleBuilder{
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.tx_source_port",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d/0x7ff,priority=1",
				hostVfVsi,
			),
		},
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.tx_acc_vsi",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,zero_padding=0",
				apfPrVsi,
			),
		},
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.vsi_to_vsi_loopback",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,target_vsi=%d",
				apfPrVsi, hostVfVsi,
			),
		},
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.vsi_to_vsi_loopback",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,target_vsi=%d",
				hostVfVsi, apfPrVsi,
			),
		},
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.source_port_to_pr_map",
			Metadata: fmt.Sprintf(
				"user_meta.cmeta.source_port=%d,zero_padding=0",
				hostVfVport,
			),
		},
	}

	log.WithField("number of rules", len(hostVfP4ruleSets)).Debug("Deleting FXP rules")

	err = p4rtClient.ProgramFXPP4Rules(hostVfP4ruleSets)
	if err != nil {
		log.Info("Host VF FXP P4 rules delete failed")
		return err
	} else {
		log.Info("Host VF FXP P4 rules were deleted successfully")
	}
	return nil
}

func AddNFP4Rules(p4rtClient types.P4RTClient, vfMacList []string, ingressMac string, egressMac string, ingressPRMac string, egressPRMac string) error {
	for _, vfMac := range vfMacList {
		macAddr, macErr := checkMacAddresses(vfMac)
		if macErr != nil {
			// We do not have a valid mac address for the host or apf interface
			log.WithField("mac address", macAddr).Error("Invalid mac address in the VF Mac list")
			return errors.New("Invalid Mac Address")
		}
	}

	macAddr, macErr := checkMacAddresses(ingressMac, egressMac, ingressPRMac, egressPRMac)
	if macErr != nil {
		// We do not have a valid mac address for the host or apf interface
		log.WithField("mac address", macAddr).Error("Invalid mac address in the VF Mac list")
		return errors.New("Invalid Mac Address")
	}

	err := programNfPrVportP4Rules(p4rtClient, ingressMac, ingressPRMac)
	if err != nil {
		log.Info("Add NF FXP P4 rules add failed for ", ingressMac, ingressPRMac)
		return err
	} else {
		log.Info("Add NF FXP P4 rules were added successfully for ", ingressMac, ingressPRMac)
	}

	err = programNfPrVportP4Rules(p4rtClient, egressMac, egressPRMac)
	if err != nil {
		log.Info("Add NF FXP P4 rules add failed for ", egressMac, egressPRMac)
		return err
	} else {
		log.Info("Add NF FXP P4 rules were added successfully for ", egressMac, egressPRMac)
	}

	for _, vfMacAddr := range vfMacList {
		nfMacList := []string{ingressMac, egressMac}
		for _, nfMacAddr := range nfMacList {
			programVsiToVsiP4Rules(p4rtClient, vfMacAddr, nfMacAddr)
		}
	}
	return nil
}

func DeleteNFP4Rules(p4rtClient types.P4RTClient, vfMacList []string, ingressMac string, egressMac string, ingressPRMac string, egressPRMac string) error {
	for _, vfMac := range vfMacList {
		macAddr, macErr := checkMacAddresses(vfMac)
		if macErr != nil {
			// We do not have a valid mac address for the host or apf interface
			log.WithField("mac address", macAddr).Error("Invalid mac address in the VF Mac list")
			return errors.New("Invalid Mac Address")
		}
	}

	macAddr, macErr := checkMacAddresses(ingressMac, egressMac, ingressPRMac, egressPRMac)
	if macErr != nil {
		// We do not have a valid mac address for the host or apf interface
		log.WithField("mac address", macAddr).Error("Invalid mac address in the VF Mac list")
		return errors.New("Invalid Mac Address")
	}

	err := deleteNfPrVportP4Rules(p4rtClient, ingressMac, ingressPRMac)
	if err != nil {
		log.Info("Delete NF FXP P4 rules add failed for ", ingressMac, ingressPRMac)
		return err
	} else {
		log.Info("Delete NF FXP P4 rules were added successfully for ", ingressMac, ingressPRMac)
	}

	err = deleteNfPrVportP4Rules(p4rtClient, egressMac, egressPRMac)
	if err != nil {
		log.Info("Delete NF FXP P4 rules add failed for ", egressMac, egressPRMac)
		return err
	} else {
		log.Info("Delete NF FXP P4 rules were added successfully for ", egressMac, egressPRMac)
	}

	for _, vfMacAddr := range vfMacList {
		nfMacList := []string{ingressMac, egressMac}
		for _, nfMacAddr := range nfMacList {
			deleteVsiToVsiP4Rules(p4rtClient, vfMacAddr, nfMacAddr)
		}
	}
	return nil
}

func AddPeerToPeerP4Rules(p4rtClient types.P4RTClient, vfMacList []string) error {
	for _, vfMac := range vfMacList {
		macAddr, macErr := checkMacAddresses(vfMac)
		if macErr != nil {
			// We do not have a valid mac address for the host VF interface
			log.WithField("mac address", macAddr).Error("Invalid mac address in the VF Mac list")
			return errors.New("Invalid Mac Address")
		}
	}
	for i := 0; i < len(vfMacList); i++ {
		for j := i + 1; j < len(vfMacList); j++ {
			programVsiToVsiP4Rules(p4rtClient, vfMacList[i], vfMacList[j])
		}
	}
	log.Info("AddPeerToPeerP4Rules FXP P4 rules added Successfully")
	return nil
}

func DeletePeerToPeerP4Rules(p4rtClient types.P4RTClient, vfMacList []string) error {
	for _, vfMac := range vfMacList {
		macAddr, macErr := checkMacAddresses(vfMac)
		if macErr != nil {
			// We do not have a valid mac address for the host VF interface
			log.WithField("mac address", macAddr).Error("Invalid mac address in the VF Mac list")
			return errors.New("Invalid Mac Address")
		}
	}
	for i := 0; i < len(vfMacList); i++ {
		for j := i + 1; j < len(vfMacList); j++ {
			deleteVsiToVsiP4Rules(p4rtClient, vfMacList[i], vfMacList[j])
		}
	}
	log.Info("AddPeerToPeerP4Rules FXP P4 rules deleted Successfully for")
	return nil
}

func AddLAGP4Rules(p4rtClient types.P4RTClient) error {
	var LAGP4ruleSets []types.FxpRuleBuilder

	LAGP4ruleSets = append(LAGP4ruleSets,
		types.FxpRuleBuilder{
			Action:   "add-entry",
			P4br:     "br0",
			Control:  "linux_networking_control.ipv4_lpm_root_lut",
			Metadata: "user_meta.cmeta.bit16_zeros=4/65535,priority=2048,action=linux_networking_control.ipv4_lpm_root_lut_action(0)",
		})
	err := p4rtClient.ProgramFXPP4Rules(LAGP4ruleSets)
	if err != nil {
		log.Info("LAG LPM ROOT LUT FXP P4 rules add failed")
	} else {
		log.Info("LAG LPM ROOT LUT FXP P4 rules were added successfully")
	}

	LAGP4ruleSets = []types.FxpRuleBuilder{}

	for idx := 0; idx < 8; idx++ {
		LAGP4ruleSets = append(LAGP4ruleSets,
			types.FxpRuleBuilder{
				Action:   "add-entry",
				P4br:     "br0",
				Control:  "linux_networking_control.tx_lag_table",
				Metadata: fmt.Sprintf("user_meta.cmeta.lag_group_id=0/255,hash=%d/7,priority=1,action=linux_networking_control.bypass", idx),
			},
		)
	}
	err = p4rtClient.ProgramFXPP4Rules(LAGP4ruleSets)
	if err != nil {
		log.Info("AddLAGP4Rules FXP P4 rules add failed")
		return err
	} else {
		log.Info("AddLAGP4Rules FXP P4 rules were added successfully")
	}
	return nil
}

func DeleteLAGP4Rules(p4rtClient types.P4RTClient) error {
	var LAGP4ruleSets []types.FxpRuleBuilder

	LAGP4ruleSets = append(LAGP4ruleSets,
		types.FxpRuleBuilder{
			Action:   "del-entry",
			P4br:     "br0",
			Control:  "linux_networking_control.ipv4_lpm_root_lut",
			Metadata: "user_meta.cmeta.bit16_zeros=4/65535,priority=2048",
		})
	err := p4rtClient.ProgramFXPP4Rules(LAGP4ruleSets)
	if err != nil {
		log.Info("LAG FXP P4 rules delete failed")
	} else {
		log.Info("LAG FXP P4 rules were delete successfully")
	}

	LAGP4ruleSets = []types.FxpRuleBuilder{}

	for idx := 0; idx < 8; idx++ {
		LAGP4ruleSets = append(LAGP4ruleSets,
			types.FxpRuleBuilder{
				Action:   "del-entry",
				P4br:     "br0",
				Control:  "linux_networking_control.tx_lag_table",
				Metadata: fmt.Sprintf("user_meta.cmeta.lag_group_id=0/255,hash=%d/7,priority=1", idx),
			},
		)
	}
	err = p4rtClient.ProgramFXPP4Rules(LAGP4ruleSets)
	if err != nil {
		log.Info("DeleteLAGP4Rules FXP P4 rules delete failed")
		return err
	} else {
		log.Info("DeleteLAGP4Rules FXP P4 rules were delete successfully")
	}
	return nil
}

func AddRHPrimaryNetworkVportP4Rules(p4rtClient types.P4RTClient, d4Mac string, d1Mac string) error {
	d4Vsi, _, err := getStrippedMacAndVsi(d4Mac)
	if err != nil {
		log.Info("AddRHPrimaryNetworkVportP4Rules failed. Unable to find Vsi and Vport for PR mac: ", d4Mac)
		return err
	}

	d1Vsi, d1MacAddr, err := getStrippedMacAndVsi(d1Mac)
	if err != nil {
		log.Info("AddRHPrimaryNetworkVportP4Rules failed. Unable to find Vsi and Vport for PR mac: ", d1Mac)
		return err
	}

	phyVportP4ruleSets := []types.FxpRuleBuilder{
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.mir_prof",
			Metadata: fmt.Sprintf(
				"mirror_prof_key=%d,action=linux_networking_control.mir_prof_action(vport_id=%d,mode=0,port_dest_type=0,dest_id=%d,func_valid=1,store_vsi=1)",
				mirror_profile_id, d1Vsi, d1Vsi,
			),
		},
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.rx_phy_port_to_pr_map",
			Metadata: fmt.Sprintf(
				"vmeta.common.port_id=0x00,zero_padding=0x0000,action=linux_networking_control.mirror_and_send(%d,%d)",
				d4Vsi+16, mirror_profile_id,
			),
		},
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.tx_acc_vsi",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,zero_padding=0,action=linux_networking_control.l2_fwd_and_bypass_bridge(%d)",
				d1Vsi, phyPort,
			),
		},
		/* rx_source_port rule is commented here as it gets handled at the PhyVportP4Rules. keeping the below rule for reference only.
		   {
		           Action:  "add-entry",
		           P4br:    "br0",
		           Control: "linux_networking_control.rx_source_port",
		           Metadata: fmt.Sprintf(
		                   "vmeta.common.port_id=0,zero_padding=0,action=linux_networking_control.set_source_port(%d)",
		                   phyPort,
		           ),
		   },
		*/
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.source_port_to_bridge_map",
			Metadata: fmt.Sprintf(
				"user_meta.cmeta.source_port=%d/0xffff,hdrs.vlan_ext[vmeta.common.depth].hdr.vid=%d/0xfff,priority=1,action=linux_networking_control.set_bridge_id(bridge_id=%d)",
				phyPort, phyPort, bridgeId,
			),
		},
		{
			Action:  "add-entry",
			P4br:    "br0",
			Control: "linux_networking_control.l2_fwd_rx_table",
			Metadata: fmt.Sprintf(
				"user_meta.pmeta.bridge_id=%d,dst_mac=0x%s,action=linux_networking_control.l2_fwd(%d)",
				bridgeId, d1MacAddr, d1Vsi+16,
			),
		},
	}
	return p4rtClient.ProgramFXPP4Rules(phyVportP4ruleSets)
}

func DeleteRHPrimaryNetworkVportP4Rules(p4rtClient types.P4RTClient, d1Mac string) error {
	d1Vsi, d1MacAddr, err := getStrippedMacAndVsi(d1Mac)
	if err != nil {
		log.Info("programRHPrimarySecondaryVportP4Rules failed. Unable to find Vsi and Vport for PR mac: ", d1Mac)
		return err
	}

	phyVportP4ruleSets := []types.FxpRuleBuilder{
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.mir_prof",
			Metadata: fmt.Sprintf(
				"mirror_prof_key=%d",
				mirror_profile_id,
			),
		},
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.rx_phy_port_to_pr_map",
			Metadata: fmt.Sprintf(
				"vmeta.common.port_id=0x00,zero_padding=0x0000",
			),
		},
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.tx_acc_vsi",
			Metadata: fmt.Sprintf(
				"vmeta.common.vsi=%d,zero_padding=0",
				d1Vsi,
			),
		},
		/* rx_source_port rule is commented here as it gets handled at the PhyVportP4Rules. keeping the below rule for reference only.
		   {
		           Action:  "del-entry",
		           P4br:    "br0",
		           Control: "linux_networking_control.rx_source_port",
		           Metadata: fmt.Sprintf(
		                   "vmeta.common.port_id=0,zero_padding=0",
		           ),
		   },
		*/
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.source_port_to_bridge_map",
			Metadata: fmt.Sprintf(
				"user_meta.cmeta.source_port=%d/0xffff,hdrs.vlan_ext[vmeta.common.depth].hdr.vid=%d/0xfff,priority=1",
				phyPort, phyPort,
			),
		},
		{
			Action:  "del-entry",
			P4br:    "br0",
			Control: "linux_networking_control.l2_fwd_rx_table",
			Metadata: fmt.Sprintf(
				"user_meta.pmeta.bridge_id=%d,dst_mac=0x%s",
				bridgeId, d1MacAddr,
			),
		},
	}
	return p4rtClient.ProgramFXPP4Rules(phyVportP4ruleSets)
}
