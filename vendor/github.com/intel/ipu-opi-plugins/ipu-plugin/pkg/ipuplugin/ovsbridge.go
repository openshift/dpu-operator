package ipuplugin

import (
	"fmt"
	"strings"
	"os"
	"path/filepath"

	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/types"
	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/utils"
	log "github.com/sirupsen/logrus"
)

type ovsBridge struct {
	bridgeName string
	ovsCliDir  string
	ovsDbPath  string
}

const (
	ACC_VM_PR_IP = "192.168.100.252/24"
)

func NewOvsBridgeController(bridgeName, ovsCliDir string, ovsDbPath string) types.BridgeController {
	return &ovsBridge{
		bridgeName: bridgeName,
		ovsCliDir:  ovsCliDir,
		ovsDbPath:  ovsDbPath,
	}
}

func createDbParam(ovsDbPath string) string {
	return "--db=unix:" + ovsDbPath
}

func isNumeric(s string) bool {
    var n int
    _, err := fmt.Sscanf(s, "%d", &n)
    return err == nil
}

func getPIDsWithComm(target string) ([]string, error) {
    files, err := os.ReadDir("/proc")
    if err != nil {
        return nil, fmt.Errorf("error reading /proc: %v", err)
    }

    var pids []string
    for _, file := range files {
        if file.IsDir() && isNumeric(file.Name()) {
            cmdPath := filepath.Join("/proc", file.Name(), "cmdline")
            data, err := os.ReadFile(cmdPath)
            if err != nil {
                continue
            }
            if strings.Contains(strings.TrimSpace(string(data)), target) {
                pids = append(pids, file.Name())
            }
        }
    }

    return pids, nil
}

func getInfrapodNamespace() (string, error) {
	pids, err := getPIDsWithComm("entrypoint.sh")
	if err != nil {
		return "", fmt.Errorf("error retrieving PIDs: %v", err)
	}

	if len(pids) == 0 {
		return "", fmt.Errorf("could not find any PID with comm containing 'entrypoint.sh'")
	}

	if len(pids) > 1 {
		// TODO: A better way is needed to identify the container
		return "", fmt.Errorf("%v PIDs found for 'entrypoint.sh': %v", len(pids), pids)
	}

	targetPID := pids[0]
	cmd := fmt.Sprintf("ip netns identify %s | tr -d '\\n'", targetPID)
	ret, err := utils.ExecuteScript(cmd)
	if err != nil {
		log.Errorf("unable to get Namespace of infrapod: %v", err)
		return "", fmt.Errorf("unable to get Namespace of infrapod: %v", err)
	} else {
		log.Debugf("Namespace of infrapod: %s", ret)
	}
	return ret, nil
}

func (b *ovsBridge) EnsureBridgeExists() error {
	// ovs-vsctl --db=/path/to/sock --may-exist add-br br-infra
	createBrParams := []string{createDbParam(b.ovsDbPath), "--may-exist", "add-br", b.bridgeName}
	if err := utils.ExecOsCommand(b.ovsCliDir+"/ovs-vsctl", createBrParams...); err != nil {
		return fmt.Errorf("error creating ovs bridge %s with ovs-vsctl command %s", b.bridgeName, err.Error())
	}
	netNs, err := getInfrapodNamespace()
	if err != nil {
		log.Errorf("EnsureBridgeExists: error->%v from getInfrapodNamespace", err)
		return err
	}
	// Flush any existing IP addresses from the bridge interface from any previous runs
	flushIPCmd := []string{"net", "exec", netNs, "ip", "addr", "flush", "dev", b.bridgeName}
	if err := utils.ExecOsCommand("ip", flushIPCmd...); err != nil {
		return fmt.Errorf("error flushing IP addresses for bridge %s: %v", b.bridgeName, err)
	}
	//assigning IP for bridge interface.
	ipAddr := ACC_VM_PR_IP
	cmdParams := []string{"net", "exec", netNs, "ip", "addr", "add", "dev", b.bridgeName, ipAddr}
	if err := utils.ExecOsCommand("ip", cmdParams...); err != nil {
		return fmt.Errorf("error->%v, assigning IP->%v to ovs bridge %s", err.Error(), ipAddr, b.bridgeName)
	}
	//bring the interface up.
	cmdParams = []string{"net", "exec", netNs, "ip", "link", "set", "dev", b.bridgeName, "up"}
	if err := utils.ExecOsCommand("ip", cmdParams...); err != nil {
		return fmt.Errorf("error->%v, bringing UP bridge interface->%v", err.Error(), b.bridgeName)
	}
	return nil
}

// Note:: This is expected to be called, when plugin exits(Stop),
// so continue to delete, without exiting for any error.
// Note: Deleting bridge, automatically deletes any ports added to it.
func (b *ovsBridge) DeleteBridges() error {
	brParams := []string{createDbParam(b.ovsDbPath), "--may-exist", "del-br", b.bridgeName}
	if err := utils.ExecOsCommand(b.ovsCliDir+"/ovs-vsctl", brParams...); err != nil {
		log.Errorf("error deleting ovs bridge %s with ovs-vsctl command %s", b.bridgeName, err.Error())
	}
	return nil
}

func (b *ovsBridge) AddPort(portName string) error {
	netNs, err := getInfrapodNamespace()
	if err != nil {
		log.Errorf("AddPort: error->%v from getInfrapodNamespace", err)
		return err
	}
	// Move interface to the infrapod namespace
	ipParams := []string{"link", "set", portName, "netns", netNs}
	err = utils.ExecOsCommand("ip", ipParams...)
	if err != nil {
		log.Errorf("error moving interface %s to infra namespace with error %s", portName, err.Error())
	}

	brParams := []string{createDbParam(b.ovsDbPath), "add-port", b.bridgeName, portName}
	err = utils.ExecOsCommand(b.ovsCliDir+"/ovs-vsctl", brParams...)
	if err != nil {
		return fmt.Errorf("unable to add port to the bridge: %w", err)
	}
	//bring the interface up.
	cmdParams := []string{"net", "exec", netNs, "ip", "link", "set", "dev", portName, "up"}
	if err := utils.ExecOsCommand("ip", cmdParams...); err != nil {
		return fmt.Errorf("error->%v, bringing UP interface->%v", err.Error(), portName)
	}
	log.WithField("portName", portName).Infof("port added to ovs bridge %s", b.bridgeName)
	return nil
}

func (b *ovsBridge) DeletePort(portName string) error {
	netNs, err := getInfrapodNamespace()
	if err != nil {
		log.Errorf("DeletePort: error->%v from getInfrapodNamespace", err)
		return err
	}
	// Move interface out of the infrapod namespace
	ipParams := []string{"net", "exec", netNs, "ip", "link", "set", "dev", portName, "netns", "1"}
	err = utils.ExecOsCommand("ip", ipParams...)
	if err != nil {
		log.Errorf("error moving interface %s to infra namespace with error %s", portName, err.Error())
	}
	brParams := []string{createDbParam(b.ovsDbPath), "del-port", b.bridgeName, portName}
	err = utils.ExecOsCommand(b.ovsCliDir+"/ovs-vsctl", brParams...)
	if err != nil {
		return fmt.Errorf("unable to delete port from the bridge: %w", err)
	}
	log.WithField("portName", portName).Infof("port deleted from ovs bridge %s", b.bridgeName)
	return nil
}
