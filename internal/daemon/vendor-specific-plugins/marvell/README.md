# Marvell Vendor Specific Plugin

Welcome to the Marvell VSP project! This guide provides a step-by-step walkthrough to compile, build, and launch the components of the code.

## Table of Contents

- [Docker Build Steps](#docker-build-steps)

## Docker Build Steps

To create and push a Docker image of marvell vsp locally Follow the below steps for host and dpu both :

1. **build image locally**

    ```sh
    make local-build #builds all the image required for dpu-operator
    make local-push  #push all images locally required
    ```

2. **deploy marvell vsp**
    marvell vsp is deployed as a daemonset in the dpu-operator namespace. To deploy marvell vsp, follow the below steps:

    Fill namespace, image name, and other required fields in the `internal/daemon/vendor-specific-plugins/marvell/00.daemonset.yaml` file.
    kubectl apply -f internal/daemon/vendor-specific-plugins/marvell/00.daemonset.yaml
