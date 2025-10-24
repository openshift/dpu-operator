## Bash scripts to program the FXP

This folder consists of the following bash scripts to program the FXP:

- [p4-gen-no-nf.sh](p4-gen-no-nf.sh) - Generates the P4 rules to enable point to point communication between VFs created on the host. 

- [p4-gen-no-nf-del.sh](p4-gen-no-nf-del.sh) - Performs the reverse of [p4-gen-no-nf.sh](p4-gen-no-nf.sh) by generating the delete rules to configure the FXP.

- [p4-gen-with-nf.sh](p4-gen-with-nf.sh) - Generates the P4 rules to configure the FXP with communication through an NF. This script requires two parameters as input: `APF1_VSI` and `APF2_VSI` (e.g., `APF1_VSI="e"` and `APF2_VSI="f"`). These two parameters are the `vsi` of the APFs attached to the NF container.

- [p4-gen-with-nf-del.sh](p4-gen-with-nf-del.sh) - Performs the reverse of [p4-gen-with-nf.sh](p4-gen-with-nf.sh) by generating the delete rules to configure the FXP. The input parameters (i.e.,  `APF1_VSI` and `APF2_VSI`) are still required to correctly generate the delete rules.

- [p4-gen-arp-rules.sh](p4-gen-arp-rules.sh) - Generates the P4 rules to configure dynamic ARP between VFs created on the host. This script requires one input parameter: `APF_VSI` which is the `vsi` of the portmux deployed on the ACC to enable communication between VLAN representors. See [Create VLAN representors](#create-vlan-representors) on how to create these on the ACC.

- [p4-gen-arp-rules-del.sh](p4-gen-arp-rules-del.sh) - Performs the reverse of [p4-gen-arp-rules.sh](p4-gen-arp-rules.sh) by generating the delete rules to configure the FXP. The input parameter (i.e.,  `APF_VSI`) is still required to correctly generate the delete rules.

While each script generates a different type of rules, the common flow of each script is as follow:

1. Create an SSH connection to the IMC to retrieve the VFs currently initialised on the host.
2. Generate the P4 rules as add or delete entry for each VF retrieved at point 1.

### Create VLAN representors <a name="vlan_representors"></a>

The script below shows an example of how to manually generate VLAN representors for 8 VFs created on the host. 

```bash
ip link add name br0 type bridge
ip link set dev br0 up
ip link add link enp0s1f0d3 name d3.300 type vlan id 300 protocol 802.1ad

ip link set dev d3.300 up
ip link set dev d3.300 master br0

ip link add link d3.300 name d3.300.301 type vlan id 301
ip link set dev d3.300.301 up
ip link set dev d3.300.301 master br0

ip link add link d3.300 name d3.300.302 type vlan id 302
ip link set dev d3.300.302 up
ip link set dev d3.300.302 master br0

ip link add link d3.300 name d3.300.303 type vlan id 303
ip link set dev d3.300.303 up
ip link set dev d3.300.303 master br0

ip link add link d3.300 name d3.300.304 type vlan id 304
ip link set dev d3.300.304 up
ip link set dev d3.300.304 master br0

ip link add link d3.300 name d3.300.305 type vlan id 305
ip link set dev d3.300.305 up
ip link set dev d3.300.305 master br0

ip link add link d3.300 name d3.300.306 type vlan id 306
ip link set dev d3.300.306 up
ip link set dev d3.300.306 master br0

ip link add link d3.300 name d3.300.307 type vlan id 307
ip link set dev d3.300.307 up
ip link set dev d3.300.307 master br0

ip link add link d3.300 name d3.300.308 type vlan id 308
ip link set dev d3.300.308 up
ip link set dev d3.300.308 master br0
```

In the example above `enp0s1f0d3` is the portmux allocated for enabling dynamic arp.

```bash
[root@ipu-acc ]# ip -br l
...
enp0s1f0d3       UP             00:0e:00:03:04:19 <BROADCAST,MULTICAST,UP,LOWER_UP>
...
```