# Marvell Comm Channel

Welcome to the Marvell Comm Channel project! This guide provides a step-by-step walkthrough to compile, build, and launch the components of the code.

## Table of Contents

- [Compilation Steps](#compilation-steps)
- [Docker Build Steps](#docker-build-steps)
- [Launch Steps](#launch-steps)
- [Steps to Enable IPv6 Link Local](#steps-to-enable-ipv6-link-local)
- [License](#license)

## Compilation Steps

Follow these steps to compile the Marvell Comm Channel code:

1. **Install Dependencies:**

    ```sh
    go mod vendor
    ```

2. **Navigate to the Marvell Directory:**

    ```sh
    cd hw-vendor/marvell
    ```

3. **Build the Project:**

    ```sh
    go build -o mrvl-commchannel
    ```

## Docker Build Steps

To create and push a Docker image:

1. **Build the Docker Image:**

    ```sh
    docker build -f Dockerfile.daemon.rhel .
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

### Steps to Enable IPv6 Link Local

1. **Edit Network Connection:**

    ```sh
    nmcli con edit type ethernet con-name ens81
    ```

2. **Set IPv6 Method to Link-Local:**

    ```sh
    set ipv6.method link-local
    ```

3. **Save the Configuration:**

    ```sh
    save (say yes)
    ```

4. **Quit the nmcli Editor:**

    ```sh
    quit
    ```

To launch the Marvell Comm Channel:

1. **Navigate to the Marvell Directory:**

    ```sh
    cd hw-vendor/marvell
    ```

2. **Add Image Name in DaemonSet Configuration:**
    - Edit `bindata/daemonset.yaml` to include your Docker image name.

3. **Apply the DaemonSet Configuration:**

    ```sh
    kubectl apply -f bindata/daemonset.yaml
    ```



## License

Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the License. You may obtain a copy of the License at

<http://www.apache.org/licenses/LICENSE-2.0>

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific language governing permissions and limitations under the License.

---

Thank you for using the Marvell Comm Channel project! If you have any questions or run into issues, feel free to open an issue or contact our support team.
