package ovsdp

import (
	"fmt"
	"os/exec"

	"github.com/go-logr/logr"
	vspnetutils "github.com/openshift/dpu-operator/internal/daemon/vendor-specific-plugins/common"
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

	_ = vspnetutils.LinkSetUpDown(portName, true)

	if isDPDK {
		ovsdp.log.Info("Adding DPDK Port to Bridge", "PortName", portName, "VFPCIAddress", vfPCIAddres)
		cmd = exec.Command("chroot", "/host", "ovs-vsctl", "--may-exist", "add-port", bridgeName, portName, "--", "set", "Interface", portName, "type=dpdk", fmt.Sprintf("options:dpdk-devargs=%s", vfPCIAddres))

	} else {
		ovsdp.log.Info("Adding Port to Bridge", "PortName", portName)
		cmd = exec.Command("chroot", "/host", "ovs-vsctl", "--may-exist", "add-port", bridgeName, portName)

	}
	return cmd.Run()
}

// ovs-vsctl command to delete dpdk-port from bridge
func (ovsdp *OvsDP) DeletePortFromDataPlane(bridgeName string, portName string) error {
	ovsdp.log.Info("Deleting Port from Bridge", "PortName", portName)
	cmd := exec.Command("chroot", "/host", "ovs-vsctl", "del-port", bridgeName, portName)

	err := cmd.Run()

	// Also bring down the interface (best-effort, ignoring errors). See RHEL-108203, where
	// the SDP interfaces are required to be down as long as there is no VF configured on the
	// host side.
	_ = vspnetutils.LinkSetUpDown(portName, false)

	return err
}

// ovs-vsctl command to delete ovs bridge
func (ovsdp *OvsDP) DeleteDataplane(bridgeName string) error {
	cmd := exec.Command("chroot", "/host", "ovs-vsctl", "del-br", bridgeName)
	return cmd.Run()
}

// ovs-vsctl command to create bridge
func createBridge(bridgeName string) error {
	cmd := exec.Command("chroot", "/host", "ovs-vsctl", "--may-exist", "add-br", bridgeName, "--", "set", "bridge", bridgeName, "datapath_type=netdev")
	return cmd.Run()
}

// InitDataPlane initializes the data path in this case it creates an ovs bridge
func (ovsdp *OvsDP) InitDataPlane(bridgeName string, isMacLearning bool) error {
	ovsdp.log.Info("Initializing OVS-DPDK Data Plane")
	ovsdp.bridgeName = bridgeName
	// "br0" For Testing Purpose
	if err := createBridge(bridgeName); err != nil {
		ovsdp.log.Error(err, "Error occurred in creating Bridge")
		return err

	}
	ovsdp.log.Info("OVS-DPDK Bridge Created Successfully", "BridgeName", bridgeName)

	// Modify the default NORMAL flow to have highest priority
	if isMacLearning {
		ovsdp.log.Info("Modifying NORMAL flow to highest priority")
		// First delete the existing NORMAL flow
		cmd := exec.Command("chroot", "/host", "ovs-ofctl", "del-flows", bridgeName)
		if err := cmd.Run(); err != nil {
			ovsdp.log.Error(err, "Error occurred in deleting default NORMAL flow")
			return err
		}
		// Add high priority NORMAL flow
		cmd = exec.Command("chroot", "/host", "ovs-ofctl", "add-flow", bridgeName, "priority=65535,actions=NORMAL")
		if err := cmd.Run(); err != nil {
			ovsdp.log.Error(err, "Error occurred in adding high priority NORMAL flow")
			return err
		}
		ovsdp.log.Info("NORMAL flow priority updated successfully", "Priority", 65535)
	}
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
	cmd := exec.Command("chroot", "/host", "ovs-vsctl", "--may-exist", "list-ports", bridgeName)
	out, err := cmd.Output()
	if err != nil {
		ovsdp.log.Error(err, "Error occurred in reading ports from Bridge")
		return "", err
	}
	return string(out), nil
}

// Add Flow rule to ovs bridge
func (ovsdp *OvsDP) AddFlowRuleToDataPlane(bridgeName string, srcInterface string, dstInterface string, dstMac string) error {
	ovsdp.log.Info("Adding Flow Rule to Bridge", "SrcInterfaces", srcInterface, "DstInterface", dstInterface, "DestinationMac", dstMac)
	// Add flow rule to bridge
	if dstMac != "" {
		if srcInterface == dstInterface {
			ovsdp.log.Info("This is Hairpinning Rule", "SrcInterface", srcInterface, "DstInterface", dstInterface)
			// Hairpinning rule with priority 100
			cmd := exec.Command("chroot", "/host", "ovs-ofctl", "add-flow", bridgeName, fmt.Sprintf("priority=100,in_port=%s,dl_dst=%s,actions=in_port", srcInterface, dstMac))
			if err := cmd.Run(); err != nil {
				ovsdp.log.Error(err, "Error occurred in adding Flow Rule to Bridge")
				return err
			}
		} else {
			ovsdp.log.Info("Adding Flow Rule with dstMac", "SrcInterface", srcInterface, "DstInterface", dstInterface, "DestinationMac", dstMac)
			cmd := exec.Command("chroot", "/host", "ovs-ofctl", "add-flow", bridgeName, fmt.Sprintf("in_port=%s,dl_dst=%s,actions=output:%s", srcInterface, dstMac, dstInterface))
			if err := cmd.Run(); err != nil {
				ovsdp.log.Error(err, "Error occurred in adding Flow Rule to Bridge")
				return err
			}
		}
	} else {
		ovsdp.log.Info("Adding Flow Rule without dstMac", "SrcInterface", srcInterface, "DstInterface", dstInterface)
		cmd := exec.Command("chroot", "/host", "ovs-ofctl", "add-flow", bridgeName, fmt.Sprintf("priority=10,in_port=%s,actions=output:%s", srcInterface, dstInterface))
		if err := cmd.Run(); err != nil {
			ovsdp.log.Error(err, "Error occurred in adding Flow Rule to Bridge")
			return err
		}
	}
	return nil
}
func (ovsdp *OvsDP) DeleteFlowRuleFromDataPlane(bridgeName string, srcInterface string, dstinterface string, dstMac string) error {
	ovsdp.log.Info("Deleting Flow Rule from Bridge", "SrcInterfaces", srcInterface)
	// Delete flow rule from bridge
	if dstMac != "" {
		cmd := exec.Command("chroot", "/host", "ovs-ofctl", "del-flows", bridgeName, fmt.Sprintf("in_port=%s,dl_dst=%s", srcInterface, dstMac))
		if err := cmd.Run(); err != nil {
			ovsdp.log.Error(err, "Error occurred in deleting Flow Rule from Bridge")
			return err
		}
	} else {
		cmd := exec.Command("chroot", "/host", "ovs-ofctl", "del-flows", bridgeName, fmt.Sprintf("in_port=%s", srcInterface))
		if err := cmd.Run(); err != nil {
			ovsdp.log.Error(err, "Error occurred in deleting Flow Rule from Bridge")
			return err
		}
	}

	ovsdp.log.Info("Flow Rule Deleted Successfully", "SrcInterfaces", srcInterface)
	return nil
}
