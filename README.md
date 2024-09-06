# DPU Operator

This operator will manage and configure data processing unit (DPUs) to be used in accelerating/offloading k8s networking functions.

## Description

The DPU operator adds support for DPUs to OpenShift in a vendor-agnostic way, using (soon to be) standard APIs. The main goal is for users to be able to run their workloads leveraging DPU resources without requiring an expert understanding of vendor-specific details.

## Getting Started

Depending on the type of testing you are doing, you will either need real hardware with real DPUs (e2e tests), or you will test against a Kind cluster (integration tests).

### Manually deploying and testing on a cluster

If you want to start from source, follow the steps below to build containers, push them to a local registry, and deploy to a cluster.

1. **Configure and Build Containers**

Run the following command to configure and build all the containers:
```sh
make local-buildx
```

2. **Push Built Images to a Local Registry**

Run the following command to push the built images to a local registry:
```sh
make local-pushx
```

3. **Deploy to the Cluster**

Run the following command to deploy the YAML files to the cluster based on `KUBECONFIG`. Make sure you have set up an image registry on the local host:
```sh
make local-deploy
```

4. **Alternative: Remote Registry**

Alternatively, if you set up a registry remotely, define the `REGISTERY` variable:
```sh
make local-deploy REGISTERY=...
```

### End-to-end testing

The DPU operator also integrates with CDA (https://github.com/bn222/cluster-deployment-automation) used to set up a complete OpenShift cluster before tests are ran against it. For that, you can use the following makefile target:
```sh
make e2e-test
```

### Integration testing

Using the following makefile target, a Kind cluster will be set up against which some tests are ran. This is mainly used during development.
```sh
make test
```

### Undeploy controller

UnDeploy the controller from the cluster:
```sh
make undeploy
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
