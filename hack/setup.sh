KUBECONFIG=/root/kubeconfig.ocpcluster oc get nodes -l node-role.kubernetes.io/master!= -o jsonpath='{.items[*].metadata.name}' | KUBECONFIG=/root/kubeconfig.ocpcluster xargs -I {} oc label node {} dpu=true
KUBECONFIG=/root/kubeconfig.microshift oc label nodes --all dpu=true

KUBECONFIG=/root/kubeconfig.microshift oc create -f examples/config.yaml
KUBECONFIG=/root/kubeconfig.ocpcluster oc create -f examples/config.yaml

# Wait for DpuOperatorConfig to be Ready on both clusters
echo "Waiting for DpuOperatorConfig to be Ready on microshift cluster..."
KUBECONFIG=/root/kubeconfig.microshift oc wait --for=condition=Ready dpuoperatorconfig/dpu-operator-config -n openshift-dpu-operator --timeout=1m
echo "Waiting for DpuOperatorConfig to be Ready on OCP cluster..."
KUBECONFIG=/root/kubeconfig.ocpcluster oc wait --for=condition=Ready dpuoperatorconfig/dpu-operator-config -n openshift-dpu-operator --timeout=1m

# this sleep is currently a workaround until we have DPU CRs, and we can check the state of the CR instead
sleep 120

KUBECONFIG=/root/kubeconfig.ocpcluster oc wait --for=condition=Ready pod --all -n openshift-dpu-operator --timeout=5m
KUBECONFIG=/root/kubeconfig.microshift oc wait --for=condition=Ready pod --all -n openshift-dpu-operator --timeout=5m
