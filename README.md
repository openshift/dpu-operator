# DPU Operator

This operator will manage and configure data processing unit (DPUs) to be used in accelerating/offloading k8s networking functions.

## Description

The DPU operator adds support for DPUs to OpenShift in a vendor-agnostic way, using (soon to be) standard APIs. The main goal is for users to be able to run their workloads leveraging DPU resources without requiring an expert understanding of vendor-specific details.

## Getting Started

Depending on the type of testing you are doing, you will either need real hardware with real DPUs (e2e tests), or you will test against a Kind cluster (integration tests).

### Manually deploying and testing on a cluster

If you want to start from source, follow the steps below to build containers, push them to a local registry, and deploy to a cluster. Prerequisite is to install taskfile (https://taskfile.dev/installation/), an modern more flexible alternative to makefile. The easiest way to set up taskfile is to install it using the following command:

```sh
env GOBIN=/bin go install github.com/go-task/task/v3/cmd/task@latest
```

1. **Configure and Build Containers**

Run the following command to configure and build all the containers. The images will be built, and will be in the container image store. In other words, the images are not yet pushed to a registry.
```sh
task build-image-all
```

3. **Deploy to the Clusters**

Run the following command to deploy the YAML files to the two clusters defined by /root/kubeconfig.ocpcluster and /root/kubeconfig.microshift. Make sure you have set up an image registry as the deploy target will both push all the images and start the main operator image which, in turn, will start the other containers.

```sh
task deploy
```

For 1 cluster deployment, you will only need /root/kubeconfig.ocpcluster. 1 cluster deployment means both Host(s) and DPU(s) are in the same cluster. 

```sh
task deploy-1c
```

4. **Undeploy**

Undoes what deploying did:

```sh
task undeploy
```

5. **Clean up images**
The build target will use previously built images if they are available to speed compilation up. To clear the previously built images, use the following command:

```sh
task clean-image-all
```

6. **Alternative: Remote Registry**

Alternatively, if you set up a registry remotely, define the `REGISTERY` variable. Note that you need to do this for the build step and the push/run step:

```sh
REGISTERY=... task ...
```

7. **Alternative: Deploy only one of the cluster**

You can deploy part of the env (host or dpu) using either `-host` or `-dpu`. For example, for the host side cluster, use this:

```sh
task deploy-cluster-host
```

8. **Alternative: Workflow of redeploying to test local changes**
A common way to test out changes is to combine `build-image-all` followed by `undeploy` and `deploy`. A target that combines these three steps is provided.

```sh
task redeploy
```

### End-to-end testing

The DPU operator also integrates with CDA (https://github.com/bn222/cluster-deployment-automation) used to set up a complete OpenShift cluster before tests are ran against it. For that, you can use the following makefile target:
```sh
task e2e-test
```

### End-to-end testing on any cluster
It is possible to run the e2e test suite separately by using the 'e2e-test-suite' target. This will execute the tests on two clusters, which are referenced by the configuration files located at `/root/kubeconfig.ocpcluster` and `/root/kubeconfig.microshift`. This target will skip re-deploying the clusters. It's also used at the end of the e2e-test target.
```sh
task fast-e2e-test
```
### Integration testing

Using the following makefile target, a Kind cluster will be set up against which some tests are ran. This is mainly used during development.
```sh
make test
```

### How to start DPU Operator components

The DPU Operator relies on a combination of label matching and custom resources. Assuming the DPU Operator is installed, the following steps will deploy daemon pods and vendor specific pods on hosts and DPUs.

Firstly, mark all nodes with the label `dpu=true` that are DPUs or Hosts with DPU. In a 2 cluster deployment, you will need to label the nodes with 2 seperate kubeconfigs. In a 1 cluster deployment, currently Hosts with DPU and DPUs are labeled with the same value. An example of doing this would be `kubectl label node worker1 dpu=true` & `kubectl label node worker-dpu1 dpu=true`.

To tell the operator to start components the `DpuOperatorConfig` needs to be created.

For 1 cluster deployment and 2 cluster deployment on the host cluster:

```sh
kubectl create -f examples/host.yaml
```

For 2 cluster deployment on the DPU cluster:

```sh
kubectl create -f examples/dpu.yaml
```

After creating the `DpuOperatorConfig` CR, you should see the following pods:
```sh
oc get pods -n openshift-dpu-operator -o wide
NAME                                              READY   STATUS    RESTARTS   AGE   IP                NODE             NOMINATED NODE   READINESS GATES
dpu-daemon-rn6mc                                  1/1     Running   0          22h   192.168.122.218   worker-229       <none>           <none>
dpu-daemon-xrrlg                                  1/1     Running   0          22h   192.168.122.90    worker-229-ptl   <none>           <none>
dpu-operator-controller-manager-68bdf56c8-4tddp   1/1     Running   0          22h   10.128.2.133      worker-229       <none>           <none>
network-resources-injector-699988f484-4m2df       1/1     Running   0          22h   10.128.2.134      worker-229       <none>           <none>
vsp-rdbk6                                         1/1     Running   0          22h   192.168.122.218   worker-229       <none>           <none>
vsp-x4bdh                                         1/1     Running   0          22h   192.168.122.90    worker-229-ptl   <none>           <none>

```

### Developer Workflow

A typical developer would run the following commands to test changes to the operator:

```sh
# Label nodes if not done priorly
kubectl label node worker1 dpu=true
kubectl label node worker-dpu1 dpu=true
# For two cluster deployment the DPU nodes are done using the second cluster's kubeconfig.
task build-image-all
task deploy # or `task deploy-1c` for single cluster deployment
kubectl create -f examples/host.yaml
# For two cluster deployment, the `examples/dpu.yaml` is created on the DPU cluster.
# Wait for daemon/vsp pods to settle
# Create pods and/or ServiceFunctionChain yaml
kubectl create -f examples/my-pod.yaml
```

### Network Functions

Currently the `ServiceFunctionChain` will deploy a pod with the provided image onto the DPU.

However there is an example network function provided in the path `examples/sfc-pod.yaml` that could be used to deploy to the DPU directly via the dpu-cni. The ServiceFunctionChain CR is currently optional.

## Contributing

TODO(user): Add detailed information on how you would like others to contribute to this project.

### How it works

This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/). It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/), which provide a reconcile function responsible for synchronizing resources until the desired state is reached on the cluster.

**NOTE:** You can also run this in one step by running: `make install run`

### Modifying the API definitions

If you are editing the API definitions, generate the manifests such as CRs or CRDs using:
```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets.

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html).

## License

Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
