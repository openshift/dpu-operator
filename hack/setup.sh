#!/usr/bin/env bash

# ------------------------------------------------------------------------------
# Variables
# ------------------------------------------------------------------------------

# Kubernetes CLI tool (oc or kubectl)
K8S_CLI="${K8S_CLI:-oc}"

# Kubeconfig file paths
KUBECONFIG_OCP="${KUBECONFIG_HOST:-/root/kubeconfig.ocpcluster}"
KUBECONFIG_MICROSHIFT="${KUBECONFIG_DPU:-/root/kubeconfig.microshift}"

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

# Function to create config with retry logic to handle webhook certificate race condition.
# Occationally, the webhook certificate is not ready yet when the config is created.
# An error similar to: Error from server (InternalError): error when creating "examples/config.yaml": Internal error occurred:
#     failed calling webhook "vdpuoperatorconfig.kb.io": failed to call webhook: Post "https://dpu-operator-webhook-service.openshift
#     -dpu-operator.svc:443/validate-config-openshift-io-v1-dpuoperatorconfig?timeout=10s": tls: failed to verify certificate: x509:
#     certificate signed by unknown authority
# Would occur. This function retries creating the config, checking every 1 second for 30 seconds max.
create_config_with_retry() {
  local k8scli_func=$1
  local config_file=$2
  local max_attempts=30
  local attempt=1
  local error_output

  echo "Creating config from $config_file..."

  while [[ $attempt -le $max_attempts ]]; do
    error_output=$($k8scli_func create -f "$config_file" 2>&1)
    if [[ $? -eq 0 ]]; then
      echo "$error_output"
      echo "Config created successfully!"
      return 0
    else
      if [[ $attempt -eq $max_attempts ]]; then
        echo "Error: Failed to create config after $max_attempts attempts" >&2
        echo "Last error message:" >&2
        echo "$error_output" >&2
        return 1
      fi
      echo "Attempt $attempt failed (webhook certificate not ready yet). Retrying in ${wait_time}s..."
      sleep 1
      attempt=$((attempt + 1))
    fi
  done
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

    # Create config in OCP cluster only with retry logic
    create_config_with_retry k8scli_ocp examples/config.yaml || exit 1

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

    # Create config on both clusters with retry logic
    create_config_with_retry k8scli_microshift examples/config.yaml || exit 1
    create_config_with_retry k8scli_ocp examples/config.yaml || exit 1

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
