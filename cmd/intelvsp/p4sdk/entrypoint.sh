#!/bin/sh

# Copyright 2024 Intel Corp. All Rights Reserved.

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#     http://www.apache.org/licenses/LICENSE-2.0

# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


set -ex

export SDE_INSTALL=/opt/p4/p4sde
export P4CP_INSTALL=/opt/p4/p4-cp-nws
export DEPEND_INSTALL=$P4CP_INSTALL
export PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION=python

# Allow set_br_pipe on a separate process so that we can sleep 30s for infrap4d to come up
func_set_br_pipe(){
    # Wait for 45s
    sleep 45
    # Set the Forwarding pipeline
    $P4CP_INSTALL/bin/p4rt-ctl set-pipe br0 "/opt/$P4_NAME/$P4_NAME.pb.bin" "/opt/$P4_NAME/$P4_NAME.p4info.txt"
}

func_start_ovs() {
    mkdir -p /opt/p4/p4-cp-nws/var/run/openvswitch
    mkdir -p /opt/p4/p4-cp-nws/etc/openvswitch
    mkdir -p /opt/p4/p4-cp-nws/share/openvswitch
    rm -rf /opt/p4/p4-cp-nws/etc/openvswitch/conf.db
    export PATH="$PATH:$P4CP_INSTALL/bin:$P4CP_INSTALL/sbin:"
    ovsdb-tool create $P4CP_INSTALL/etc/openvswitch/conf.db $P4CP_INSTALL/share/openvswitch/vswitch.ovsschema
    ovsdb-server --remote=punix:$P4CP_INSTALL/var/run/openvswitch/db.sock --remote=db:Open_vSwitch,Open_vSwitch,manager_options --pidfile --detach
    ovs-vswitchd --pidfile --detach  --log-file=/var/log/ovs-vswitchd.log
}

LOGFILE=/var/log/entrypoint.log;
mkdir -p /var/log


CPF_INFO_FILE=cpf_info_file.txt
CONF_DIR=/usr/share/stratum/es2k
CONF_FILE=es2k_skip_p4.conf
$SDE_INSTALL/bin/vfio_bind.sh 8086:1453 | tail -n 1 > $CPF_INFO_FILE
cat  $CPF_INFO_FILE
IOMMU_GROUP=$(awk '{print $5}' $CPF_INFO_FILE)
CPF_BDF=$(awk '{print $2}' $CPF_INFO_FILE)
export IOMMU_GROUP
export CPF_BDF
# Note that P4_NAME is also envsubst along with above
# which comes from the Dockerfile

#We exclude interfaces D0 to D3. CTRL_MAP will
#include interfaces D4 to D15, that can be added as
#port representors on bridge(in ACC).
IDPF_VF_VPORT0=4 ; CTRL_MAP="" ; \
idpf_ports=$(realpath /sys/class/net/*/dev_port | grep  "$(lspci -nnkd "8086:1452" | awk  "NR==1{print \$1}")") ; \
for port in ${idpf_ports} ; do
     [ "$(head "$port")" -ge "${IDPF_VF_VPORT0}" ]  && netpath="$(dirname "$port")" && \
            IDPF_VPORT_MAC="$(head "$netpath"/address)" && \
            set -- "\"$IDPF_VPORT_MAC\"" && \
            IDPF_VPORT_MAC="$*" && \
            CTRL_MAP=${CTRL_MAP}${IDPF_VPORT_MAC}, > $LOGFILE 2>&1 ;
done;

ACC_PR_CTRL_MAP="$CTRL_MAP"
echo "ctrl_map : [\"NETDEV\",${ACC_PR_CTRL_MAP}1]"
export ACC_PR_CTRL_MAP

mkdir -p $CONF_DIR
envsubst < $CONF_FILE.template > $CONF_DIR/$CONF_FILE

touch "/opt/$P4_NAME/tofino.bin"
$P4CP_INSTALL/bin/tdi_pipeline_builder \
    --p4c_conf_file=/usr/share/stratum/es2k/es2k_skip_p4.conf \
    --tdi_pipeline_config_binary_file="/opt/$P4_NAME/$P4_NAME.pb.bin"


# Copy files required by infrap4d
mkdir -p /usr/share/target_sys/
cp /opt/p4/p4sde/share/target_sys/zlog-cfg /usr/share/target_sys/zlog-cfg
mkdir -p /usr/share/stratum/es2k/
cp /opt/p4/p4-cp-nws/share/stratum/es2k/es2k_port_config.pb.txt /usr/share/stratum/es2k/es2k_port_config.pb.txt
# Create logging directory used by infrap4d
mkdir -p /var/log/stratum
touch /dev/mem
# sleep 5 secs
sleep 5

# Start set pipe in the background
func_set_br_pipe &
# Start ovs switchd and ovs db
func_start_ovs
# Start Infrap4d
/opt/p4/p4-cp-nws/sbin/infrap4d -grpc_open_insecure_mode=true -nodetach -disable_krnlmon=true
