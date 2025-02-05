#!/usr/bin/env bash

set -e

bash hack/prepare-venv.sh

cd ocp-traffic-flow-tests
source /tmp/tft-venv/bin/activate

export KUBECONFIG=/root/kubeconfig.ocpcluster
nodes=$(oc get nodes)
export worker=$(echo "$nodes" | grep -oP '^worker-[^\s]*')
if [ -z "$worker" ]; then
  echo "Error: worker is empty"
  exit 1
fi

export KUBECONFIG=/root/kubeconfig.microshift
nodes=$(oc get nodes)
export acc=$(echo "$nodes" | grep -oP '^\d{3}-acc')
if [ -z "$acc" ]; then
  echo "Error: acc is empty"
  exit 1
fi

envsubst < ../hack/cluster-configs/ocp-tft-config.yaml > tft_config.yaml

# Give dpu operator pods time to settle to ensure pods will successfully create
sleep 100

python main.py tft_config.yaml
