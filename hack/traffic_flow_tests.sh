#!/usr/bin/env bash

set -e

bash hack/prepare-venv.sh

cd kubernetes-traffic-flow-tests
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
export acc=$(echo "$nodes" | grep -oP '^\d+-acc')
if [ -z "$acc" ]; then
  echo "Error: acc is empty"
  exit 1
fi

temp_file=$(mktemp)

envsubst < ../hack/cluster-configs/ocp-tft-config.yaml > $temp_file

export TFT_MANIFESTS_OVERRIDES=../hack/cluster-configs/traffic_flow_manifests
OUTPUT_BASE="./ft-logs/result-$(date '+%Y%m%d-%H%M%S.%4N-')"

./tft.py -v debug --output-base "$OUTPUT_BASE" "$temp_file"

./print_results.py "$OUTPUT_BASE"*.json
