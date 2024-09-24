#!/usr/bin/env bash

set -e

source ./.tmp/ocp-venv/bin/activate

cd ocp-traffic-flow-tests

export KUBECONFIG=/root/kubeconfig.ocpcluster
nodes=$(oc get nodes)
export worker=$(echo "$nodes" | grep -oP '^worker-[^\s]*')

export KUBECONFIG=/root/kubeconfig.microshift
nodes=$(oc get nodes)
export acc=$(echo "$nodes" | grep -oP '^\d{3}-acc')

envsubst < ../cluster_configs/config.yaml > tft_config.yaml

python3.11 main.py tft_config.yaml
