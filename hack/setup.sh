#!/usr/bin/env bash


wait_for_dpu() {
  local kubeconfig_path=$1
  if [[ -z "$kubeconfig_path" ]]; then
    echo "Error: Kubeconfig path must be provided as the first argument." >&2
    return 1
  fi

  local start_time=$(date +%s)

  local dpu_name
  while true; do
    dpu_name=$(oc --kubeconfig "$kubeconfig_path" get dpu -o name 2>/dev/null | head -n 1)
    if [[ -n "$dpu_name" ]]; then
      echo -e "\nDPU resource found: $dpu_name"
      break
    fi
    echo -n "."
    sleep 1
  done

  oc --kubeconfig "$kubeconfig_path" wait "$dpu_name" --for=condition=Ready=True --timeout=5m

  local end_time=$(date +%s)
  local duration=$((end_time - start_time))
  echo "Total wait time: ${duration} seconds."
}

KUBECONFIG=/root/kubeconfig.ocpcluster oc get nodes -l node-role.kubernetes.io/master!= -o jsonpath='{.items[*].metadata.name}' | KUBECONFIG=/root/kubeconfig.ocpcluster xargs -I {} oc label node {} dpu=true
KUBECONFIG=/root/kubeconfig.microshift oc label nodes --all dpu=true

KUBECONFIG=/root/kubeconfig.microshift oc create -f examples/config.yaml
KUBECONFIG=/root/kubeconfig.ocpcluster oc create -f examples/config.yaml

echo "Waiting for DpuOperatorConfig to be Ready on OCP cluster..."
KUBECONFIG=/root/kubeconfig.ocpcluster oc wait --for=condition=Ready dpuoperatorconfig/dpu-operator-config -n openshift-dpu-operator --timeout=1m
echo "Waiting for DPU to be Ready on OCP cluster..."
wait_for_dpu /root/kubeconfig.ocpcluster

echo "Waiting for DpuOperatorConfig to be Ready on microshift cluster..."
KUBECONFIG=/root/kubeconfig.microshift oc wait --for=condition=Ready dpuoperatorconfig/dpu-operator-config -n openshift-dpu-operator --timeout=1m
echo "Waiting for DPU to be Ready on microshift cluster..."
wait_for_dpu /root/kubeconfig.microshift

KUBECONFIG=/root/kubeconfig.ocpcluster oc wait --for=condition=Ready pod --all -n openshift-dpu-operator --timeout=5m
KUBECONFIG=/root/kubeconfig.microshift oc wait --for=condition=Ready pod --all -n openshift-dpu-operator --timeout=5m
