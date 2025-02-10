#!/usr/bin/env bash

set -e

check_ocp() {
    local KUBECONFIG_PATH=$1

    export KUBECONFIG=$KUBECONFIG_PATH

    if oc get nodes; then
        return 0
    else
        return 1
    fi
}

deploy_cluster_and_dpu_operator() {
    bash hack/both.sh
}

deploy_local_dpu_operator() {
    bash hack/dpu_host_deploy_post.sh
    bash hack/dpu_deploy_post.sh
}

result_ocp=0
check_ocp "/root/kubeconfig.ocpcluster" || result_ocp=$?

result_microshift=0
check_ocp "/root/kubeconfig.microshift" || result_microshift=$?

# Check if both OCP and MicroShift checks succeeded
if [ $result_ocp -eq 0 ] && [ $result_microshift -eq 0 ]; then
    echo "Both OCP and MicroShift cluster checks succeeded!"
    deploy_local_dpu_operator
else
    echo "Cluster config is not proper... deploying cluster and dpu operator"
    deploy_cluster_and_dpu_operator
fi

bash hack/traffic_flow_tests.sh
