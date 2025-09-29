#!/usr/bin/env bash

# ------------------------------------------------------------------------------
# Variables
# ------------------------------------------------------------------------------

# Kubernetes CLI tool (oc or kubectl)
K8S_CLI="${K8S_CLI:-oc}"

# Kubeconfig file paths
KUBECONFIG_OCP="/root/kubeconfig.ocpcluster"
KUBECONFIG_MICROSHIFT="/root/kubeconfig.microshift"

# ------------------------------------------------------------------------------
# Functions
# ------------------------------------------------------------------------------

# Helper function to run Kubernetes CLI commands with OCP cluster kubeconfig
k8scli_ocp() {
    KUBECONFIG="$KUBECONFIG_OCP" "$K8S_CLI" "$@"
}

# Helper function to run Kubernetes CLI commands with MicroShift cluster kubeconfig
k8scli_microshift() {
    KUBECONFIG="$KUBECONFIG_MICROSHIFT" "$K8S_CLI" "$@"
}

# Function to wait for DPU to be Ready via dpu CR
wait_for_dpu() {
  local kubeconfig_path=$1
  if [[ -z "$kubeconfig_path" ]]; then
    echo "Error: Kubeconfig path must be provided as the first argument." >&2
    return 1
  fi

  local start_time=$(date +%s)

  local dpu_name
  while true; do
    dpu_name=$("$K8S_CLI" --kubeconfig "$kubeconfig_path" get dpu -o name 2>/dev/null | head -n 1)
    if [[ -n "$dpu_name" ]]; then
      echo -e "\nDPU resource found: $dpu_name"
      break
    fi
    echo -n "."
    sleep 1
  done

  "$K8S_CLI" --kubeconfig "$kubeconfig_path" wait "$dpu_name" --for=condition=Ready=True --timeout=5m

  local end_time=$(date +%s)
  local duration=$((end_time - start_time))
  echo "Total wait time: ${duration} seconds."
}

# Function to check kubeconfig files and determine execution path
check_kubeconfig_files() {
    if [[ -f "$KUBECONFIG_OCP" && -f "$KUBECONFIG_MICROSHIFT" ]]; then
        echo "Both OCP and MicroShift clusters kubeconfig files found:"
        echo "  - $KUBECONFIG_OCP"
        echo "  - $KUBECONFIG_MICROSHIFT"
        return 0
    elif [[ -f "$KUBECONFIG_OCP" && ! -f "$KUBECONFIG_MICROSHIFT" ]]; then
        echo "Only OCP cluster kubeconfig found:"
        echo "  - $KUBECONFIG_OCP (found)"
        echo "  - $KUBECONFIG_MICROSHIFT (not found)"
        return 2
    else
        echo "Missing required kubeconfig file(s):"
        [[ ! -f "$KUBECONFIG_OCP" ]] && echo "  - $KUBECONFIG_OCP (not found)"
        [[ ! -f "$KUBECONFIG_MICROSHIFT" ]] && echo "  - $KUBECONFIG_MICROSHIFT (not found)"
        return 1
    fi
}

# Function for two clusters setup when only OCP kubeconfig file exist.
setup_1_cluster() {
    echo "Executing 1 cluster setup..."

    # Label nodes in OCP cluster only
    for node in $(k8scli_ocp get nodes --selector='!node-role.kubernetes.io/master' -o name); do
        k8scli_ocp label "$node" dpu=true --overwrite
    done

    # Create config in OCP cluster only
    k8scli_ocp create -f examples/config.yaml

    # Wait for DpuOperatorConfig to be Ready on OCP cluster only
    echo "Waiting for DpuOperatorConfig to be Ready on OCP cluster..."
    k8scli_ocp wait --for=condition=Ready dpuoperatorconfig/dpu-operator-config -n openshift-dpu-operator --timeout=1m

    # Wait for DPU on OCP cluster only
    wait_for_dpu "$KUBECONFIG_OCP"

    # Wait for pods on OCP cluster only
    k8scli_ocp wait --for=condition=Ready pod --all -n openshift-dpu-operator --timeout=5m

    echo "1 cluster setup completed!"
}

# Function for two clusters setup when both kubeconfig files exist.
# Please note that some DPUs (e.g. Intel IPU) need a specific order.
setup_2_cluster() {
    echo "Executing 2 cluster setup..."

    # Label nodes on both clusters
    for node in $(k8scli_ocp get nodes --selector='!node-role.kubernetes.io/master' -o name); do
        k8scli_ocp label "$node" dpu=true --overwrite
    done
    k8scli_microshift label nodes --all dpu=true

    # Create config on both clusters
    k8scli_microshift create -f examples/config.yaml
    k8scli_ocp create -f examples/config.yaml

    # Wait for DpuOperatorConfig to be Ready on both clusters
    echo "Waiting for DpuOperatorConfig to be Ready on microshift cluster..."
    k8scli_microshift wait --for=condition=Ready dpuoperatorconfig/dpu-operator-config -n openshift-dpu-operator --timeout=1m
    echo "Waiting for DpuOperatorConfig to be Ready on OCP cluster..."
    k8scli_ocp wait --for=condition=Ready dpuoperatorconfig/dpu-operator-config -n openshift-dpu-operator --timeout=1m

    # Wait for DPU on both clusters
    wait_for_dpu "$KUBECONFIG_MICROSHIFT"
    wait_for_dpu "$KUBECONFIG_OCP"

    # Wait for pods on both clusters
    k8scli_ocp wait --for=condition=Ready pod --all -n openshift-dpu-operator --timeout=5m
    k8scli_microshift wait --for=condition=Ready pod --all -n openshift-dpu-operator --timeout=5m

    echo "2 cluster setup completed!"
}


# ------------------------------------------------------------------------------
# Script starts here
# ------------------------------------------------------------------------------

echo "Using Kubernetes CLI: $K8S_CLI"
echo "Checking host system for kubeconfig files..."
check_kubeconfig_files
check_result=$?

case $check_result in
    0)
        echo "All prerequisites met. Proceeding with full setup..."
        setup_2_cluster
        ;;
    2)
        echo "Only OCP cluster kubeconfig available. Proceeding with OCP-only setup..."
        setup_1_cluster
        ;;
    *)
        echo "Prerequisites are not met. Cannot proceed with setup!"
        echo "Please ensure at least the OCP cluster kubeconfig exists: $KUBECONFIG_OCP and optionally microshift kubeconfig exists: $KUBECONFIG_MICROSHIFT"
        exit 1
        ;;
esac
