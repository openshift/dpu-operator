KUBECONFIG=/root/kubeconfig.ocpcluster oc get nodes -l node-role.kubernetes.io/master!= -o jsonpath='{.items[*].metadata.name}' | KUBECONFIG=/root/kubeconfig.ocpcluster xargs -I {} oc label node {} dpu=true
sleep 10
KUBECONFIG=/root/kubeconfig.ocpcluster oc wait --for=condition=Ready pod --all -n openshift-dpu-operator --timeout=2m

KUBECONFIG=/root/kubeconfig.microshift oc label nodes --all dpu=true
sleep 10
KUBECONFIG=/root/kubeconfig.microshift oc wait --for=condition=Ready pod --all -n openshift-dpu-operator --timeout=2m


KUBECONFIG=/root/kubeconfig.microshift oc create -f examples/dpu.yaml
KUBECONFIG=/root/kubeconfig.ocpcluster oc create -f examples/host.yaml

# this sleep is currently a workaround until we have DPU CRs, and we can check the state of the CR instead
sleep 120

KUBECONFIG=/root/kubeconfig.ocpcluster oc wait --for=condition=Ready pod --all -n openshift-dpu-operator --timeout=5m
KUBECONFIG=/root/kubeconfig.microshift oc wait --for=condition=Ready pod --all -n openshift-dpu-operator --timeout=5m
