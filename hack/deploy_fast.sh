#!/usr/bin/env bash

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
   bash hack/ipu_host_deploy.sh
   bash hack/ipu_deploy.sh 
   bash hack/deploy_traffic_flow_tests.sh
}

deploy_local_dpu_operator() {
   bash hack/ipu_host_deploy_post.sh
   bash hack/ipu_deploy_post.sh
   bash hack/deploy_traffic_flow_tests.sh
}

check_ocp "/root/kubeconfig.ocpcluster"
result_ocp=$?

check_ocp "/root/kubeconfig.microshift"
result_microshift=$?

# Check if both OCP and MicroShift checks succeeded
if [ $result_ocp -eq 0 ] && [ $result_microshift -eq 0 ]; then
    echo "Both OCP and MicroShift cluster checks succeeded!"
    deploy_local_dpu_operator
else
    echo "Cluster config is not proper... deploying cluster and dpu operator"
    deploy_cluster_and_dpu_operator
fi
