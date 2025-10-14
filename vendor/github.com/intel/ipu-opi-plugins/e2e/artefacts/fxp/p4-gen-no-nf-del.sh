#!/bin/bash
#Copyright (C) 2023 Intel Corporation
#SPDX-License-Identifier: Apache-2.0

# Delete the point-to-point rules

# Array of VFs addresses on the host
vf_mac_addresses=(
#"00:15:00:00:03:14"
#"00:16:00:00:03:14"
#"00:13:00:00:03:14"
#"00:14:00:00:03:14"
#"00:17:00:00:03:14"
#"00:18:00:00:03:14"
#"00:19:00:00:03:14"
#"00:1a:00:00:03:14"
)

# Retrieve VF mac addresses from the IMC
IFS=$'\n' read -r -d '' -a vf_mac_addresses < <(ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 root@192.168.0.1 "/usr/bin/cli_client -cq" \
   | awk '{if(($4 == "0x0") && ($6 == "yes")) {print $17}}')


# Iterate over the array of MAC addresses
for src_mac in "${vf_mac_addresses[@]}"; do

    vsi=$(echo "$src_mac" | awk -F: '{print $2}')

    for dst_mac in "${vf_mac_addresses[@]}"; do

        smac=$(echo "$src_mac" | tr -d ':')
        dmac=$(echo "$dst_mac" | tr -d ':')

        if [ "$smac" != "$dmac" ]; then

            target_vsi=$(echo "$dst_mac" | awk -F: '{print $2}')

            echo "\$P4CP_INSTALL/bin/p4rt-ctl del-entry br0 rh_mvp_control.ingress_loopback_table \"vsi=0x$vsi,target_vsi=0x$target_vsi\""

            echo "\$P4CP_INSTALL/bin/p4rt-ctl del-entry br0 rh_mvp_control.vport_egress_dmac_vsi_table \"vsi=0x$vsi,dmac=0x$dmac\""
        fi
    done
done