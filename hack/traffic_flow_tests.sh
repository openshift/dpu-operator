#!/usr/bin/env bash

# The script detects the used parameters, on the assumption the cluster
# was deployed by other scripts and via cluster-deployment-automation.
#
# You can however override this detection by setting the following
# environment variables:
# - TFT_KUBECONFIG (defaults to /root/kubeconfig.ocpcluster)
# - TFT_KUBECONFIG_INFRA (defaults to /root/kubeconfig.microshift)
# - TFT_WORKER (defaults to a tenent worker node named "worker-*" from `oc get nodes`)

set -e

TFT_KUBECONFIG="${TFT_KUBECONFIG:-/root/kubeconfig.ocpcluster}"
TFT_KUBECONFIG_INFRA="${TFT_KUBECONFIG_INFRA:-/root/kubeconfig.microshift}"
TFT_WORKER="${TFT_WORKER:-}"

bash hack/prepare-venv.sh

cd kubernetes-traffic-flow-tests
source /tmp/tft-venv/bin/activate

nodes="$(oc --kubeconfig="$TFT_KUBECONFIG" get nodes)"
worker="$TFT_WORKER"
if [ -z "$worker" ] ; then
    worker=$(echo "$nodes" | grep -oP '^worker-[^\s]*')
    if [ -z "$worker" ]; then
      echo "Error: worker is empty"
      exit 1
    fi
fi
export worker

nodes="$(oc --kubeconfig="$TFT_KUBECONFIG_INFRA" get nodes)"
export acc=$(echo "$nodes" | grep -oP '^\d+-acc')
if [ -z "$acc" ]; then
  echo "Error: acc is empty"
  exit 1
fi

temp_file=$(mktemp)

envsubst < ../hack/cluster-configs/ocp-tft-config.yaml > $temp_file

OUTPUT_BASE="./ft-logs/result-$(date '+%Y%m%d-%H%M%S.%4N-')"

export TFT_MANIFESTS_OVERRIDES=../hack/cluster-configs/traffic_flow_manifests
export TFT_KUBECONFIG
export TFT_KUBECONFIG_INFRA
./tft.py --check -v debug --output-base "$OUTPUT_BASE" "$temp_file"
