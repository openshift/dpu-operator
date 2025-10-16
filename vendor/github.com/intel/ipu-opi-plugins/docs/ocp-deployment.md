# Intel IPU Plugin deployment and testing on Openshift cluster

## Assumptions:
- Working OCP cluster with at least one Worker node with IPU with MEV-1.2
- RHEL 9.4 is running on IPU ACC and Red Hat subscription is enabled
- ACC has internet access
- An image registry is configured and running to host container images that is accessible from the OCP environment and microshift running on ACC 

## Clone this repository

```
git clone https://github.com/intel/ipu-opi-plugins
```

Set path to directory

```
cd ipu-opi-plugins
export ROOT_DIR=$(pwd)
```

## Export image registry URL

Set IMAGE_REGISTRY env variable with valid Image registry.

```
export IMAGE_REGISTRY=localhost:5000
```

## Run P4-SDK on ACC
Login to ACC on a worker node and then build P4-SDK container image from the source by following the steps descibed [here](../p4sdk/README.md) and then push the p4-sdk container image to the image registry.

Set HugePages if they are not configured:

```
mkdir /dev/hugepages
mount -t hugetlbfs -o pagesize=2M none /dev/hugepages
echo 1024 > /sys/devices/system/node/node0/hugepages/hugepages-2048kB/nr_hugepages
```

Then deploy the P4-SDK container daemonset:
 
Update P4-SDK container image URL in `$ROOT_DIR/e2e/artefacts/k8s/p4sdk-ds.yaml` and then deploy it.
```
oc create -f $ROOT_DIR/e2e/artefacts/k8s/p4sdk-ds.yaml
```

## Run IPU Plugin

### Build IPU Plugin container image:
Since ipu-plugin will be running on both workner node host side as well as on ACC we need to build a multi-arch container image for it so that the same image name can used to deploy it on both locations.

Docker needs to be configured for multi-arch build and correct proxy needs to be set for docker daemon.

```
$ROOT_DIR/ipu-plugin
make imagex
```

### Run IPU Plugin on Host
Update ipu-plugin image reference if required in `$ROOT_DIR/e2e/artefacts/k8s/vsp-ds.yaml` and then deploy it:

```
oc create -f e2e/artefacts/k8s/vsp-ds.yaml
```

### Run IPU Plugin on ACC
Login to ACC in worker node and then deploy the ipu-plugin daemonset in there.

```
oc create -f e2e/artefacts/k8s/vsp-ds.yaml
```

## Create MEV Virtual Functions on worker node

Replace `ens7f0` with actual MEV PF name on the worker node.

```
echo 8 | sudo tee /sys/class/net/ens7f0/device/sriov_numvfs
```



## Deploy DPU Operator

### Deploy DPU Operator on ACC

#### Build container image

```
cd
git clone https://github.com/openshift/dpu-operator.git
cd dpu-operator/
make images-build
make images-push
```

#### Deploy DPU Operator

Update dpu operator image references in `config/dev/local-images.yaml`

```
apiVersion: apps/v1
kind: Deployment
metadata:
  name: controller-manager
  namespace: system
  labels:
    control-plane: controller-manager
    app.kubernetes.io/name: deployment
    app.kubernetes.io/instance: controller-manager
    app.kubernetes.io/component: manager
    app.kubernetes.io/created-by: dpu-operator
    app.kubernetes.io/part-of: dpu-operator
    app.kubernetes.io/managed-by: kustomize
spec:
  template:
    spec:
      containers:
      - command:
        name: manager
        env:
        - name: DPU_DAEMON_IMAGE
          value: {{ Registry URL}}/dpu-daemon:{{tag}}
        - name: IMAGE_PULL_POLICIES
          value: Always
        image: {{ Registry URL}}/dpu-operator:{{tag}}
        imagePullPolicy: Always

```

```
make local-deploy
```

#### Configure DPU Operator for ACC

```
$ cat <<EOF | oc create -f -
apiVersion: config.openshift.io/v1
kind: DpuOperatorConfig
metadata:
  name: dpu-operator-config
  namespace: dpu-operator-system
spec:
  mode: dpu
EOF
```

### Deploy DPU Operator on Host

Follow the same steps to build and deploy DPU operator as done in section [Deploy DPU Operator on ACC](#deploy-dpu-operator-on-acc)

#### Configure DPU Operator for host

```
$ cat <<EOF | oc create -f -
apiVersion: config.openshift.io/v1
kind: DpuOperatorConfig
metadata:
  name: dpu-operator-config
  namespace: dpu-operator-system
spec:
  mode: host
EOF
```

## Check for readiness
Check that all related Pods (DPU Operator, DPU Daemon, IPU Plugin and P4-SDK) are up and running.

## E2E tests

> [ TO-DO ] - Check that DPU Operator deploys and configures SRIOV Network Operator to create and register MEV Virtual Functions on the worker node.

There are sample deployment yaml artifacts available in `e2e/artefacts/k8s/` directory that can be used for e2e tests.
The `pod-tc1.yaml` and `pod-tc2.yaml` Pod specs require MEV Virtual Functions resoruces to be available in a node and Network Attachment Definitions (NAD) is created in the same namespace.

### Without any Network Function

[TO-DO]

### With Network Function

[TO-DO]
