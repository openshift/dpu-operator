#!/usr/bin/env bash

cd ocp-traffic-flow-tests
source /tmp/tft-venv/bin/activate

export KUBECONFIG=/root/kubeconfig.ocpcluster
nodes=$(oc get nodes)
export worker=$(echo "$nodes" | grep -oP '^worker-[^\s]*')


export KUBECONFIG=/root/kubeconfig.microshift
nodes=$(oc get nodes)
export acc=$(echo "$nodes" | grep -oP '^\d{3}-acc')

envsubst < ../hack/cluster-configs/ocp-tft-config.yaml > tft_config.yaml

python3.11 main.py tft_config.yaml

 
