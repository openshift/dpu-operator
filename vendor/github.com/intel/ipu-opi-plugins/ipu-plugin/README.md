# IPU Plugin

IPU plugin implements OPI API for OPI DPU Daemon to communicate for Pod interface attachment.


## Build
```
make
```

After a successful build the binaries will be available in target platform specific directory:

```
# tree bin/
bin/
├── linux-amd64
│   └── ipuplugin
└── linux-arm64
    └── ipuplugin

```

## Run

```
./bin/linux-amd64/ipuplugin
```

## Configuration options
```
./bin/linux-amd64/ipuplugin --help
IPU Manager is a daemon that exposes a CNI gRPC backend for SR-IOV networking offload to Intel MEV.

Usage:
  ipuplugin [flags]

Flags:
      --bridge string         The bridge name that IPU manager will manage (default "br-tenant")
      --bridgeType string     The bridge type that IPU manager will manage (default "linux")
      --config string         config file (default is /etc/ipu/ipuplugin.yaml)
      --daemonHostIp string   Daemon address on host (default "192.168.1.1")
      --daemonIpuIp string    Daemon address on ipu (default "192.168.1.2")
      --daemonPort int        Daemon port port (default 50151)
  -h, --help                  help for ipuplugin
      --host string           IPU Manager serving host (default "localhost")
      --interface string      The uplink network interface name
      --logDir string         IPU Manager log directory (default "/var/log/ipuplugin")
      --ovsCliDir string      The directory where the ovs-vsctl is located (default "/opt/p4/p4-cp-nws/bin")
      --p4rtbin string        The directory where the p4rt-ctl binary is located (default "/opt/p4/p4-cp-nws/bin/p4rt-ctl")
      --port int              IPU Manager serving TCP port (default 50152)
      --portMuxVsi int        The port mux VSI number. This must be for the same interface from --interface flags (default 10)
  -v, --verbosity string      Log level (debug, info, warn, error, fatal, panic (default "info")
```