#!/usr/bin/env bash

set -e

export KUBECONFIG="${KUBECONFIG_HOST:-/root/kubeconfig.ocpcluster}"

cd kubernetes-traffic-flow-tests
source /tmp/tft-venv/bin/activate

# Get the first worker node with label dpu.config.openshift.io/dpuside=dpu-host
export worker=$(oc get nodes -l "dpu.config.openshift.io/dpuside=dpu-host" -o jsonpath='{.items[0].metadata.name}')
if [ -z "$worker" ]; then
  echo "Error: No worker node found with label dpu.config.openshift.io/dpuside=dpu-host"
  exit 1
fi

temp_file=$(mktemp)

envsubst < ../hack/cluster-configs/ocp-tft-config.yaml > $temp_file

export TFT_MANIFESTS_OVERRIDES=../hack/cluster-configs/traffic_flow_manifests
OUTPUT_BASE="./ft-logs/result-$(date '+%Y%m%d-%H%M%S.%4N-')"

./tft.py --output-base "$OUTPUT_BASE" "$temp_file"

./print_results.py "$OUTPUT_BASE"*.json
