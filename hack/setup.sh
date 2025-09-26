#!/usr/bin/env bash


wait_for_dpu() {
  local kubeconfig_path=$1
  if [[ -z "$kubeconfig_path" ]]; then
    echo "Error: Kubeconfig path must be provided as the first argument." >&2
    return 1
  fi

  echo "Checking for DPU resources..."

  # Check for DPU count immediately
  local dpu_count
  dpu_count=$(oc --kubeconfig "$kubeconfig_path" get dpu -o name 2>/dev/null | wc -l)

  if [[ "$dpu_count" -lt 1 ]]; then
    echo "Error: No DPU resources found. Expected at least 1 DPU." >&2
    return 1
  fi

  echo "Found $dpu_count DPU resource(s)"

  # Wait for all DPUs to be ready
  oc --kubeconfig "$kubeconfig_path" wait dpu --all --for=condition=Ready=True --timeout=5m

  echo "All DPU resources are ready."
}

KUBECONFIG=/root/kubeconfig.ocpcluster oc get nodes -l node-role.kubernetes.io/master!= -o jsonpath='{.items[*].metadata.name}' | KUBECONFIG=/root/kubeconfig.ocpcluster xargs -I {} oc label node {} dpu=true
KUBECONFIG=/root/kubeconfig.microshift oc label nodes --all dpu=true

KUBECONFIG=/root/kubeconfig.microshift oc create -f examples/config.yaml
KUBECONFIG=/root/kubeconfig.ocpcluster oc create -f examples/config.yaml

# Wait for DpuOperatorConfig to be Ready on both clusters
echo "Waiting for DpuOperatorConfig to be Ready on microshift cluster..."
KUBECONFIG=/root/kubeconfig.microshift oc wait --for=condition=Ready dpuoperatorconfig/dpu-operator-config -n openshift-dpu-operator --timeout=1m
echo "Waiting for DpuOperatorConfig to be Ready on OCP cluster..."
KUBECONFIG=/root/kubeconfig.ocpcluster oc wait --for=condition=Ready dpuoperatorconfig/dpu-operator-config -n openshift-dpu-operator --timeout=1m

wait_for_dpu /root/kubeconfig.microshift
wait_for_dpu /root/kubeconfig.ocpcluster

KUBECONFIG=/root/kubeconfig.ocpcluster oc wait --for=condition=Ready pod --all -n openshift-dpu-operator --timeout=5m
KUBECONFIG=/root/kubeconfig.microshift oc wait --for=condition=Ready pod --all -n openshift-dpu-operator --timeout=5m
