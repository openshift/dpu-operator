# Marvell Comm Channel

Welcome to the Marvell Comm Channel project! This guide provides a step-by-step walkthrough to compile, build, and launch the components of the code.

## Table of Contents

- [Compilation Steps](#compilation-steps)
- [Docker Build Steps](#docker-build-steps)
- [Launch Steps](#launch-steps)

## Compilation Steps

Follow these steps to compile the Marvell Comm Channel code:

1. **Navigate to the Marvell Directory:**

    ```sh
    cd internal/daemon/vendor-specific-plugins/marvell
    ```

2. **Build the Project:**

    ```sh
    go build -o mrvl-commchannel
    ```

## Docker Build Steps

To create and push a Docker image:

1. **Build the Docker Image:**

    ```sh
    docker build -f DockerFile.CommChannel.rhel .
    ```

2. **Tag the Docker Image:**

    ```sh
    docker tag <image_id> <TagName:latest>
    ```

3. **Push the Docker Image:**

    ```sh
    docker push <TagName:latest>
    ```

## Launch Steps

dpu-operator launches Marvell comm Channel as daemonset using internal/controller/bindata/marvell/00.daemonset.yaml.
Docker image name should be updated in this file.

---

Thank you for using the Marvell Comm Channel project! If you have any questions or run into issues, feel free to open an issue or contact our support team.
