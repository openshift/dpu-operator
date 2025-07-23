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

6. **Registry Configuration**

The DPU operator now uses in-cluster registries by default with predictable URLs based on your cluster configuration:

- **Host Registry**: `default-route-openshift-image-registry.apps.ocpcluster.redhat.com`
- **DPU Registry**: `default-route-openshift-image-registry.apps.172.16.3.16.nip.io`

You can check the current configuration:
```sh
task show-registry-config
```

To verify the configuration matches your actual clusters (when they're running):
```sh
task verify-registry-routes
```

**Custom Registry Configuration**: If your cluster domains or IPs differ, you can override them:

```sh
# Override cluster domain (affects host registry)
HOST_CLUSTER_DOMAIN=mycluster.example.com task build-image-all

# Override DPU IP (affects DPU registry)  
DPU_CLUSTER_IP=10.1.2.3 task build-image-all

# Override registry URLs directly
REGISTRY_HOST=my-host-registry.com REGISTRY_DPU=my-dpu-registry.com task build-image-all
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
