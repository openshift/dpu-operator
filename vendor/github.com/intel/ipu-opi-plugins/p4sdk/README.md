# Intel IPU P4 SDK

Download IPU SDK source code from: https://cdrdv2.intel.com/v1/dl/getContent/812084/812081?filename=intel-ipu-sdk-source-code-1.2.0.7550.tar.gz

Plase note you will require Intel RDC access to download this file.

## Pre-requisites
- Docker 

## Initialize Emulation Support

```
docker run --rm --privileged multiarch/qemu-user-static --reset -p yes -c yes

```

### Extract IPU SDK source code
```
cp intel-ipu-sdk-source-code-1.2.0.7550.tar.gz /tmp
cd /tmp
tar zxvf intel-ipu-sdk-source-code-1.2.0.7550.tar.gz
cd Intel_IPU_SDK-7550
```

### Build ACC workbench
This can take couple of hours to build.

```
nohup ./scripts/workbench/build_workbench.sh -t acc > acc_workbench.log 2>&1 &
```
You can watch the logs from:
```
tail -f acc_workbench.log
```

### Build ACC P4 artifacts

Modify `build_all.sh` script to only select ACC core base apps.

```
sed -i 's/\(^.*setup \&\& \\\)/#\1/' build_all.sh
sed -i 's/\(^.*build_pkg_base \&\& \\\)/#\1/' build_all.sh
sed -i 's/\(^.*build_plat_base \&\& \\\)/#\1/' build_all.sh
sed -i 's/\(^.*build_plat_deploy \&\& \\\)/#\1/' build_all.sh
sed -i 's/\(^.*build_plat_signed \&\& \\\)/#\1/' build_all.sh
sed -i 's/\(^.*build_flash \&\& \\\)/#\1/' build_all.sh
sed -i 's/\(^.*build_core_base \&\& \\\)/#\1/' build_all.sh
sed -i 's/\(^.*build_core_deploy\) \(\&\& \\\)/\1/' build_all.sh
sed -i 's/\(^.*build_ssd\)/#\1/' build_all.sh
```

After modifying the `build_all.sh` run the script to complete the build. This can take long time as well.
```
chmod +x ./build_all.sh
./build_all.sh
```

### Build IPU SDK reference applications in container image
Once the build is complete you can copy the `p4.tar.gz` into this directory and build contianer image with it.

```
git clone https://github.com/intel/ipu-opi-plugins.git
cd ipu-opi-plugins/p4sdk
cp /tmp/Intel_IPU_SDK-7550/build/hw/rootfs_acc_runtime/opt/p4.tar.gz .
make image
```


### To run a container with the P4 artifacts


```
podman run -d --privileged -v /lib/modules/5.14.0-427.13.1.el9_4.aarch64:/lib/modules/5.14.0-427.13.1.el9_4.aarch64 -v  /sys:/sys -p 9559:9559 localhost/intel-ipu-p4-sdk:latest

```

**Note**: The host modules are shared because the vfio module is not available in the container.
