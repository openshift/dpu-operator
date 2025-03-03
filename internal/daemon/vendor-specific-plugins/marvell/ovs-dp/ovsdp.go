package ovsdp

import (
	"fmt"
	"os/exec"

	"github.com/go-logr/logr"
	mrvlutils "github.com/openshift/dpu-operator/internal/daemon/vendor-specific-plugins/marvell/mrvl-utils"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	ovsDbPath  = "/var/run/openvswitch/db.sock"
	ovsCliPath = "/usr/local/bin/ovs-vsctl"
	deviceId   = "a063"
)

type OvsDP struct {
	bridgeName string
	ovsDBPath  string
	ovsCLIPath string
	log        logr.Logger
}

func NewOvsDP() *OvsDP {
	return &OvsDP{
		log:        ctrl.Log.WithName("MarvellVSP:OvsDP"),
		bridgeName: "br0",
		ovsDBPath:  ovsDbPath,
		ovsCLIPath: ovsCliPath,
	}
}

// CreateDBParam creates the db parameter for ovs-vsctl command
func createDbParam(ovsDbPath string) string {
	return "--db=unix:" + ovsDbPath
}

// ovs-vsctl command to add dpdk port to bridge
func (ovsdp *OvsDP) AddPortToDataPlane(bridgeName string, portName string, vfPCIAddres string, isDPDK bool) error {
	var cmd *exec.Cmd

	exec.Command("ip", "link", "set", portName, "up").Run()

	if isDPDK {
		ovsdp.log.Info("Adding DPDK Port to Bridge", "PortName", portName, "VFPCIAddress", vfPCIAddres)
		cmd = exec.Command("ovs-vsctl", "--may-exist", "add-port", bridgeName, portName, "--", "set", "Interface", portName, "type=dpdk", fmt.Sprintf("options:dpdk-devargs=%s", vfPCIAddres))

	} else {
		ovsdp.log.Info("Adding Port to Bridge", "PortName", portName)
		cmd = exec.Command("ovs-vsctl", "--may-exist", "add-port", bridgeName, portName)

	}
	return cmd.Run()
}

// ovs-vsctl command to delete dpdk-port from bridge
func (ovsdp *OvsDP) DeletePortFromDataPlane(bridgeName string, portName string) error {
	ovsdp.log.Info("Deleting Port from Bridge", "PortName", portName)
	cmd := exec.Command("ovs-vsctl", "del-port", bridgeName, portName)
	return cmd.Run()
}

// ovs-vsctl command to delete ovs bridge
func (ovsdp *OvsDP) DeleteDataplane(bridgeName string) error {
	cmd := exec.Command("ovs-vsctl", "del-br", bridgeName)
	return cmd.Run()
}

// ovs-vsctl command to create bridge
func createBridge(bridgeName string) error {
	cmd := exec.Command("ovs-vsctl", "--may-exist", "add-br", bridgeName, "--", "set", "bridge", bridgeName, "datapath_type=netdev")
	return cmd.Run()
}

// InitDataPlane initializes the data path in this case it creates an ovs bridge
func (ovsdp *OvsDP) InitDataPlane(bridgeName string) error {
	ovsdp.log.Info("Initializing OVS-DPDK Data Plane")
	ovsdp.bridgeName = bridgeName
	// "br0" For Testing Purpose
	if err := createBridge(bridgeName); err != nil {
		ovsdp.log.Error(err, "Error occurred in creating Bridge")
		return err

	}
	ovsdp.log.Info("OVS-DPDK Bridge Created Successfully", "BridgeName", bridgeName)
	// Get the name of interface from device id with device id as "a063"
	// a063 is the device id of RPM interface
	portName, err := mrvlutils.GetNameByDeviceID(deviceId)
	if err != nil {
		ovsdp.log.Error(err, "Error occurred in getting RPM Interface")
	}
	// Add the port to the bridge
	if err := ovsdp.AddPortToDataPlane(bridgeName, portName, "", false); err != nil {
		ovsdp.log.Error(err, "Error occurred in adding RPM interface to Bridge")
	}
	ovsdp.log.Info("RPM Interface Added to Bridge Successfully", "PortName", portName)
	return nil
}

// ReadPortFromBridge reads all the ports from the bridge
func (ovsdp *OvsDP) ReadAllPortFromDataPlane(bridgeName string) (string, error) {
	cmd := exec.Command("ovs-vsctl", "--may-exist", "list-ports", bridgeName)
	out, err := cmd.Output()
	if err != nil {
		ovsdp.log.Error(err, "Error occurred in reading ports from Bridge")
		return "", err
	}
	return string(out), nil
}
