#!/bin/bash
#Copyright (C) 2023 Intel Corporation
#SPDX-License-Identifier: Apache-2.0

# Generate the arp rules rules

# APF used as portmux
APF_VSI="e"

# Array of VFs addresses on the host
vf_mac_addresses=(
# "00:15:00:00:04:15"
# "00:16:00:00:04:15"
# "00:13:00:00:04:15"
# "00:14:00:00:04:15"
# "00:17:00:00:04:15"
# "00:18:00:00:04:15"
# "00:19:00:00:04:15"
# "00:1a:00:00:04:15"
)

# Retrieve VF mac addresses from the IMC
IFS=$'\n' read -r -d '' -a vf_mac_addresses < <(ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 root@192.168.0.1 "/usr/bin/cli_client -cq" \
    | awk '{if(($4 == "0x0") && ($6 == "yes")) {print $17}}')

mod_ptr=1
ctag=300

# Iterate over the array of MAC addresses
for vf_mac in "${vf_mac_addresses[@]}"; do

    vsi=$(echo "$vf_mac" | awk -F: '{print $2}')
    mod_ptr=$((mod_ptr + 1))
    ctag=$((ctag + 1))
    dmac=$(echo "$vf_mac" | tr -d ':')

    echo "\$P4CP_INSTALL/bin/p4rt-ctl del-entry br0 rh_mvp_control.vport_arp_egress_table \"vsi=0x$vsi,bit32_zeros=0x0000\""

    echo "\$P4CP_INSTALL/bin/p4rt-ctl del-entry br0 rh_mvp_control.vlan_push_ctag_stag_mod_table \"meta.common.mod_blob_ptr=$mod_ptr\""

    echo "\$P4CP_INSTALL/bin/p4rt-ctl del-entry br0 rh_mvp_control.portmux_egress_req_table \"vsi=0x$APF_VSI,vid=$ctag\""

    echo "\$P4CP_INSTALL/bin/p4rt-ctl del-entry br0 rh_mvp_control.ingress_loopback_table \"vsi=0x$APF_VSI,target_vsi=0x$vsi\""

    echo "\$P4CP_INSTALL/bin/p4rt-ctl del-entry br0 rh_mvp_control.portmux_egress_resp_dmac_vsi_table \"vsi=0x$APF_VSI,dmac=0x$dmac\""
done

echo "\$P4CP_INSTALL/bin/p4rt-ctl del-entry br0 rh_mvp_control.portmux_ingress_loopback_table \"bit32_zeros=0x0000\""

echo "\$P4CP_INSTALL/bin/p4rt-ctl del-entry br0 rh_mvp_control.vlan_pop_ctag_stag_mod_table \"meta.common.mod_blob_ptr=1\""