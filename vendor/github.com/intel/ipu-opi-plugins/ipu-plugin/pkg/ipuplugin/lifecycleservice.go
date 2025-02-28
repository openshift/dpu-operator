// Copyright (c) 2024 Intel Corporation.  All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License")
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ipuplugin

import (
	"context"
	"crypto/rand"
	"fmt"
	math_rand "math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/p4rtclient"
	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/types"
	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/utils"
	pb "github.com/openshift/dpu-operator/dpu-api/gen"
	"github.com/pkg/sftp"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"golang.org/x/crypto/ssh"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type LifeCycleServiceServer struct {
	pb.UnimplementedLifeCycleServiceServer
	daemonHostIp string
	daemonIpuIp  string
	daemonPort   int
	mode         string
	p4rtClient   types.P4RTClient
	bridgeCtlr   types.BridgeController
	initialized  bool // Currently, can only call initiliaze once
}

const (
	hostVportId         = "02"
	accVportId          = "03"
	deviceId            = "0x1452"
	vendorId            = "0x8086"
	imcAddress          = "192.168.0.1:22"
	ApfNumber           = 16
	last_byte_mac_range = 239
)

var InitAccApfMacs = false
var AccApfMacList []string
var PeerToPeerP4RulesAdded = false

// Reserved ACC interfaces(using vport_id or last digit of interface name, like 4 represents-> enp0s1f0d4)
const (
	PHY_PORT0_INTF_INDEX = 4
	PHY_PORT1_INTF_INDEX = 5
	NF_IN_PR_INTF_INDEX  = 9
	NF_OUT_PR_INTF_INDEX = 10
)

// TODO: GetFilteredPFs can be used to fill the array.
var AccIntfNames = [ApfNumber]string{"enp0s1f0", "enp0s1f0d1", "enp0s1f0d2", "enp0s1f0d3", "enp0s1f0d4", "enp0s1f0d5", "enp0s1f0d6",
	"enp0s1f0d7", "enp0s1f0d8", "enp0s1f0d9", "enp0s1f0d10", "enp0s1f0d11", "enp0s1f0d12", "enp0s1f0d13", "enp0s1f0d14", "enp0s1f0d15"}

func NewLifeCycleService(daemonHostIp, daemonIpuIp string, daemonPort int, mode string, p4rtClient types.P4RTClient, brCtlr types.BridgeController) *LifeCycleServiceServer {
	return &LifeCycleServiceServer{
		daemonHostIp: daemonHostIp,
		daemonIpuIp:  daemonIpuIp,
		daemonPort:   daemonPort,
		mode:         mode,
		p4rtClient:   p4rtClient,
		bridgeCtlr:   brCtlr,
		initialized:  false,
	}
}

type NetworkHandler interface {
	AddrAdd(link netlink.Link, addr *netlink.Addr) error
	AddrList(link netlink.Link, family int) ([]netlink.Addr, error)
	LinkList() ([]netlink.Link, error)
}

type NetworkHandlerImpl struct{}

func (h *NetworkHandlerImpl) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrAdd(link, addr)
}
func (h *NetworkHandlerImpl) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return netlink.AddrList(link, family)
}
func (h *NetworkHandlerImpl) LinkList() ([]netlink.Link, error) {
	return netlink.LinkList()
}

type FileSystemHandler interface {
	GetDevice(iface string) ([]byte, error)
	GetVendor(iface string) ([]byte, error)
}
type FileSystemHandlerImpl struct{}

func (fs *FileSystemHandlerImpl) GetDevice(iface string) ([]byte, error) {
	return os.ReadFile(fmt.Sprintf("/sys/class/net/%s/device/device", iface))
}
func (fs *FileSystemHandlerImpl) GetVendor(iface string) ([]byte, error) {
	return os.ReadFile(fmt.Sprintf("/sys/class/net/%s/device/vendor", iface))
}

type ExecutableHandler interface {
	validate() bool
	nmcliSetupIpAddress(link netlink.Link, ipStr string, ipAddr *netlink.Addr) error
	SetupAccApfs() error
}

type ExecutableHandlerImpl struct{}

type SSHHandler interface {
	sshFunc() error
}

type SSHHandlerImpl struct{}

type FXPHandler interface {
	configureFXP(p4rtClient types.P4RTClient, brCtlr types.BridgeController) error
}

type FXPHandlerImpl struct{}

var fileSystemHandler FileSystemHandler
var networkHandler NetworkHandler
var ExecutableHandlerGlobal ExecutableHandler
var sshHandler SSHHandler
var fxpHandler FXPHandler

func InitHandlers() {
	if fileSystemHandler == nil {
		fileSystemHandler = &FileSystemHandlerImpl{}
	}
	if networkHandler == nil {
		networkHandler = &NetworkHandlerImpl{}
	}
	if ExecutableHandlerGlobal == nil {
		ExecutableHandlerGlobal = &ExecutableHandlerImpl{}
	}
	if sshHandler == nil {
		sshHandler = &SSHHandlerImpl{}
	}
	if fxpHandler == nil {
		fxpHandler = &FXPHandlerImpl{}
	}
}

func isPF(iface string) (bool, error) {
	device, err := fileSystemHandler.GetDevice(iface)
	if err != nil {
		return false, fmt.Errorf("cannot identify device with code: %s; error %v ", deviceId, err.Error())
	}

	vendor, err := fileSystemHandler.GetVendor(iface)
	if err != nil {
		return false, fmt.Errorf("cannot identify vendor device with code: %s; error %v ", vendorId, err.Error())
	}

	return strings.TrimSpace(string(device)) == deviceId && strings.TrimSpace(string(vendor)) == vendorId, nil
}

func getCommPf(mode string, linkList []netlink.Link) (netlink.Link, error) {
	var pf netlink.Link
	for i := 0; i < len(linkList); i++ {
		mac := linkList[i].Attrs().HardwareAddr.String()
		octets := strings.Split(mac, ":")

		if mode == types.IpuMode {

			// Check the 4th octet which is used to identify the PF
			if octets[3] == accVportId {

				// On ACC, the 4th octet in the base mac address may already be set to accVportId and used by
				// the another APF (i.e., the first one). If it is the first APF, then it already has an IP.
				// Two distinguish between the two, we select the one which doesn't have an IP set already.
				if list, _ := networkHandler.AddrList(linkList[i], netlink.FAMILY_V4); len(list) == 0 {
					pf = linkList[i]
					break
				}
			}
		} else {

			// Check the 4th octet which is used to identify the PF
			if octets[3] == hostVportId {

				if list, _ := networkHandler.AddrList(linkList[i], netlink.FAMILY_V4); len(list) == 0 {
					pf = linkList[i]
					break
				}
			}
		}
	}

	if pf == nil {
		return nil, fmt.Errorf("check if the ip address already set")
	}

	return pf, nil
}

/*
It can take time for network-manager's state for each interface, to become
activated, when IP address is set, which can cause the IP address to not stick.
Note: Currently we only support nmcli/NetworkManager daemon combination(RHEL),
this api can be extended for other distros that use different CLI/systemd-networkd.
Option2: First set IP address, sleep for a while, and check
if interface is activated thro nmcli. Retry for few times,
until it succeeds or times out. Also had to add connection,if profile does not exist.
*/
func (e *ExecutableHandlerImpl) nmcliSetupIpAddress(link netlink.Link, ipStr string, ipAddr *netlink.Addr) error {
	var runCmd string
	var err error
	var output string
	maxRetries := 8
	retryInterval := 10 * time.Second
	intfActivated := false
	ipAddrSet := false
	intfName := link.Attrs().Name
	ipWithMask := ipStr + "/24"

	for cnt := 0; cnt < maxRetries; cnt++ {
		if err = networkHandler.AddrAdd(link, ipAddr); err != nil {
			//Note::Can error if address already set, ignoring for now.
			log.Errorf("AddrAdd err ->%v, for ip->%v\n", err, ipAddr)
		}
		addrList, err := networkHandler.AddrList(link, netlink.FAMILY_V4)
		if err == nil {
			ipAddrList := fmt.Sprintf("AddrList->%v\n", addrList)
			if strings.Contains(ipAddrList, ipStr) {
				log.Infof("AddrList->%v, contains expected IP->%v\n", ipAddrList, ipStr)
				ipAddrSet = true
				goto sleep
			}
			log.Errorf("AddrList->%v, does not contain expected IP->%v\n", ipAddrList, ipStr)
		} else {
			log.Errorf("AddrList err ->%v\n", err)
		}
	sleep:
		if ipAddrSet && intfActivated {
			break
		}
		if intfActivated != true {
			time.Sleep(retryInterval)
			output, err = utils.ExecuteScript(`nmcli general status`)
			if err == nil {
				runCmd = fmt.Sprintf(`nmcli -g GENERAL.STATE con show "%s" | grep activated`, intfName)
				output, err = utils.ExecuteScript(runCmd)
				output = strings.TrimSuffix(output, "\n")
				if (output != "activated") || (err != nil) {
					log.Errorf("nmcli err ->%v, output->%v, for cmd->%v\n", err, output, runCmd)
					// no such connection profile
					if strings.Contains(err.Error(), "no such connection profile") {
						runCmd = fmt.Sprintf(`nmcli connection add type ethernet ifname "%s" con-name "%s" \
						ip4 "%s"`, intfName, intfName, ipWithMask)
						_, err = utils.ExecuteScript(runCmd)
						if err != nil {
							log.Errorf("nmcli err->%v, for cmd->%v\n", err, runCmd)
							goto retry
						} else {
							log.Infof("nmcli cmd->%v, passed\n", runCmd)
						}
					}
					goto retry
				} else {
					log.Infof("nmcli interface->%v activated\n", intfName)
					intfActivated = true
				}
			} else {
				log.Infof("network manager not running, err->%v, output-%v\n", err, output)
				goto retry
			}
		}
	retry:
		log.Infof("nmcliSetIPAddress: Retry attempt cnt->%v:\n", cnt)
	}
	if ipAddrSet && intfActivated {
		log.Infof("nmcliSetIPAddress: successful->%v, for interface->%v\n", ipStr, intfName)
		return nil
	}
	log.Errorf("nmcliSetIP: error->%v, setting IP for->%v, ipAddrSet->%v, intfActivated->%v\n", err, intfName, ipAddrSet, intfActivated)
	return fmt.Errorf("nmcliSetIP: error->%v, setting IP for->%v, ipAddrSet->%v, intfActivated->%v\n", err, intfName, ipAddrSet, intfActivated)
}

func setIP(link netlink.Link, ip string) error {
	list, err := networkHandler.AddrList(link, netlink.FAMILY_V4)

	if err != nil {
		log.Errorf("setIP: unable to get the ip address of link: %v\n", err)
		return fmt.Errorf("unable to get the ip address of link: %v", err)
	}

	if len(list) == 0 {

		ipAddr := net.ParseIP(ip)

		if ipAddr.To4() == nil {
			log.Errorf("setIP: invalid ip->%v\n", ipAddr)
			return fmt.Errorf("not a valid IPv4 address: %v", err)
		}

		// Set the IP address on PF
		addr := &netlink.Addr{IPNet: &net.IPNet{IP: ipAddr, Mask: net.CIDRMask(24, 32)}}

		if err = ExecutableHandlerGlobal.nmcliSetupIpAddress(link, ip, addr); err != nil {
			log.Errorf("setIP: err->%v from nmcliSetup\n", err)
			return fmt.Errorf("setIP: err->%v from nmcliSetup", err)
		}

	} else {
		log.Errorf("address already set. Unset ip address for interface %s and run again\n", link.Attrs().Name)
		return fmt.Errorf("address already set. Unset ip address for interface %s and run again", link.Attrs().Name)
	}
	log.Debugf("setIP: Address->%v, set for interface->%v\n", ip, link.Attrs().Name)
	return nil
}

func GetMacforNetworkInterface(intf string, linkList []netlink.Link) (string, error) {
	mac := ""
	found := false
	for i := 0; i < len(linkList); i++ {
		if linkList[i].Attrs().Name == intf {
			mac = linkList[i].Attrs().HardwareAddr.String()
			log.Debugf("found mac->%v for interface->%v\n", mac, intf)
			found = true
			break
		}
	}

	if found == true {
		return mac, nil
	}
	log.Errorf("Couldnt find mac for interface->%v\n", intf)
	return "", fmt.Errorf("Couldnt find mac for interface->%v\n", intf)
}

// TODO: Can we cache 2 PF lists for host and ACC, to avoid repeated calls to GetFilteredPFs
func GetFilteredPFs(pfList *[]netlink.Link) error {

	linkList, err := networkHandler.LinkList()

	if err != nil || len(linkList) == 0 {
		return fmt.Errorf("unable to retrieve link list: %v, len->%v", err, len(linkList))
	}

	for i := 0; i < len(linkList); i++ {
		result, err := isPF(linkList[i].Attrs().Name)

		if result && err == nil {
			*pfList = append(*pfList, linkList[i])
		}
	}

	return nil
}

func FindInterfaceForGivenMac(macAddr string) (string, error) {
	var pfList []netlink.Link
	InitHandlers()
	if err := GetFilteredPFs(&pfList); err != nil {
		log.Errorf("FindInterfaceForGivenMac: err->%v from GetFilteredPFs", err)
		return "", status.Error(codes.Internal, err.Error())
	}

	intfName := ""
	found := false
	for i := 0; i < len(pfList); i++ {
		if pfList[i].Attrs().HardwareAddr.String() == macAddr {
			intfName = pfList[i].Attrs().Name
			log.Debugf("found intfName->%v for mac->%v\n", intfName, macAddr)
			found = true
			break
		}
	}
	if found == true {
		return intfName, nil
	}
	log.Errorf("Couldnt find intfName for mac->%v\n", macAddr)
	return "", fmt.Errorf("Couldnt find intfName for mac->%v\n", macAddr)
}

/*
If IDPF net devices dont show up on host-side(this can happen if IMC reboot is done without rmmod(for IDPF on host).
This function is a best effort to bring-up IDPF netdevices, using rmmod/modprobe of IDPF.
*/
func checkIdpfNetDevices(mode string) {
	var pfList []netlink.Link
	if mode == types.HostMode {
		if err := GetFilteredPFs(&pfList); err != nil {
			log.Errorf("checkNetDevices: err->%v from GetFilteredPFs", err)
			return
		}
		//Case where we dont see host IDPF netdevices.
		if len(pfList) == 0 {
			log.Debugf("Not seeing host IDPF netdevices, attempt rmmod/modprobe\n")
			output, err := utils.ExecuteScript(`lsmod | grep idpf`)
			if err != nil {
				log.Errorf("lsmod err->%v, output->%v\n", err, output)
				return
			}

			_, err = utils.ExecuteScript(`rmmod idpf`)

			if err != nil {
				log.Errorf("rmmod err->%v\n", err)
				return
			} else {
				_, err = utils.ExecuteScript(`modprobe idpf`)
				if err != nil {
					log.Errorf("modprobe err->%v\n", err)
					return
				}
			}
			log.Debugf("completed-rmmod and modprobe of IDPF\n")
		} else {
			log.Debugf("host IDPF netdevices exist, count->%d\n", len(pfList))
		}
	}
}

func configureChannel(mode, daemonHostIp, daemonIpuIp string) error {

	var pfList []netlink.Link

	if err := GetFilteredPFs(&pfList); err != nil {
		fmt.Printf("configureChannel: err->%v from GetFilteredPFs", err)
		return status.Error(codes.Internal, err.Error())
	}

	pf, err := getCommPf(mode, pfList)

	if pf == nil {
		// Address already set - we don't proceed with setting the ip
		fmt.Printf("configureChannel: pf nil from getCommPf\n")
		return nil
	}

	if err != nil {
		fmt.Printf("configureChannel: err->%v from getCommPf\n", err)
		return status.Error(codes.Internal, err.Error())
	}

	var ip string

	if mode == "ipu" {
		ip = daemonIpuIp
	} else {
		ip = daemonHostIp
	}

	if err := setIP(pf, ip); err != nil {
		fmt.Printf("configureChannel: err->%v from setIP", err)
		return status.Error(codes.Internal, err.Error())
	}

	return nil
}

// sets random bytes for last 2 bytes(5th and 6th) in MAC address
func setBaseMacAddr() (string, error) {
	var macAddress string
	macBytes := make([]byte, 2)
	_, err := rand.Read(macBytes)
	if err != nil {
		return "", fmt.Errorf("error->%v, failed to create random bytes for MAC: ", err)
	}
	//Restricting range of last byte in node-policy to be less than 240,
	//to allow for 16 function-ids. Since last-byte(+1) is done
	//for the 16 function-ids, in CP code->set_start_mac_address(in mac_utils.c)
	if macBytes[1] > last_byte_mac_range {
		macBytes[1] = byte(math_rand.Intn(last_byte_mac_range) + 1)
	}
	log.Debugf("mac bytes ->%v\n", macBytes)

	macAddress = fmt.Sprintf("00:00:00:00:%x:%x", macBytes[0], macBytes[1])
	log.Info("Allocated IPU MAC pattern:", macAddress)

	return macAddress, nil
}

/*
Updates files below on IMC, and does IMC reboot.
1. Copy P4 package from container to IMC
2. Update load_custom_pkg.sh
3. Create post_init_app.sh
4. Create uuid file.
*/
func (s *SSHHandlerImpl) sshFunc() error {
	config := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.Password(""),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// Connect to the remote server.
	client, err := ssh.Dial("tcp", imcAddress, config)
	if err != nil {
		return fmt.Errorf("failed to dial: %s", err)
	}
	defer client.Close()

	// Create an SFTP client.
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("failed to create SFTP client: %s", err)
	}
	defer sftpClient.Close()

	//copy P4 package file.
	p4PkgName := os.Getenv("P4_NAME") + ".pkg"
	imcPath := "/work/scripts/" + p4PkgName
	vspPath := "/" + p4PkgName
	err = utils.CopyBinary(imcPath, vspPath, sftpClient)

	if err != nil {
		return fmt.Errorf("sshFunc:copyBinary-error: %v", err)
	}

	macAddress, err := setBaseMacAddr()
	if err != nil {
		return fmt.Errorf("error from setBaseMacAddr()->%v", err)
	}

	inputFile := genLoadCustomPkgFile(macAddress)
	remoteFilePath := "/work/scripts/load_custom_pkg.sh"

	err = utils.CopyFile(inputFile, remoteFilePath, sftpClient)

	if err != nil {
		return fmt.Errorf("sshFunc:CopyFile-error: %v", err)
	}

	//post_init_app.sh
	inputFile = postInitAppScript()
	remoteFilePath = "/work/scripts/post_init_app.sh"

	err = utils.CopyFile(inputFile, remoteFilePath, sftpClient)

	if err != nil {
		return fmt.Errorf("sshFunc:CopyFile-error: %v", err)
	}

	inputFile = macAddress + "\n"
	remoteFilePath = "/work/uuid"

	err = utils.CopyFile(inputFile, remoteFilePath, sftpClient)

	if err != nil {
		return fmt.Errorf("sshFunc:CopyFile-error: %v", err)
	}

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %s", err)
	}
	defer session.Close()

	err = session.Run("reboot")
	if err != nil {
		return fmt.Errorf("failed to run commands: %s", err)
	}

	return nil
}

func countAPFDevices() int {
	var pfList []netlink.Link

	if err := GetFilteredPFs(&pfList); err != nil {
		return 0
	}

	return len(pfList)
}

/*
Updates post_init_app.sh script, which installs
port-setup.sh script. port-setup.sh script,
will run devmem command for D5 interface, as soon as
D5 comes up on ACC, to enable connectivity.
There is an intermittent race condition, when port-setup.log file, is
accessed(for file removal or any other case) by post_init_app.sh,
then later when nohup tries to stdout to that log file(port-setup.log),
there are file sync issues, either log file is not updated or not present.
In order to circumvent this, just allowing nohup to over-write port-setup.log.
Also, to make this more robust, added polling in post-init-app.sh, to check for
log(and if not present within 10 secs), we start second instance of port-setup.sh.
So, at the most, we would run port-setup.sh twice.
Running devmem commands thro locking mechanism, so the devmem commands are
run in critical section, so that devmem commands are run sequentially,
when port-setup.sh gets run concurrently.
Note: In order to handle RHEL ISO install use-case, where ACC reboots(post install),
independant of IMC, port-setup script will be running
as daemon, so anytime ACC goes down(it will get detected), so that devmem commands
will get re-run, when ACC comes up. Based on design, atleast 1 or utmost 2 instances
of port-setup can be running as daemon.
*/
func postInitAppScript() string {

	postInitAppScriptStr := `#!/bin/bash
set -x

trap 'echo "Line $LINENO: $BASH_COMMAND"' DEBUG

PORT_SETUP_SCRIPT=/work/scripts/port-setup.sh
POST_INIT_LOG=/work/scripts/post-init.log
PORT_SETUP_LOG=/work/scripts/port-setup.log
PORT_SETUP_LOG2=/work/scripts/port-setup2.log
OPCODE_CFG_FILE=$(mktemp -p /tmp --suffix=.txt)

/usr/bin/rm -f ${PORT_SETUP_SCRIPT} ${POST_INIT_LOG}
/usr/bin/rm -f ${PORT_SETUP_LOG} ${POST_SETUP_LOG2}
sleep 1
sync

exec 2>&1 1>${POST_INIT_LOG}

pkill -9 $(basename ${PORT_SETUP_SCRIPT})

cat<<PORT_CONFIG_EOF > ${PORT_SETUP_SCRIPT}
#!/bin/bash
IDPF_VPORT_NAME="enp0s1f0d5"
ACC_VPORT_ID=0x5
retry=0
ran_cmds=0
ran_cmds_cnt=0
ran_cmds_cnt_max=2147483647 # Max value for 32-bit signed integer

# Set max size of log file (bytes)
MAX_LOG_SIZE=1048576
# Log file to check
LOG_FILE=\${1}

echo "LOG_FILE->:"\${LOG_FILE}

echo "random_num(for unique port-setup.log):"\$(od -An -N8 -i /dev/urandom)

LOCKFILE=/tmp/mylockfile

# Function to release the lock
release_lock() {
  rm -f \${LOCKFILE}
}

# Set up the trap to release the lock on exit
trap release_lock EXIT

# Function to invoke devmem commands
run_devmem_cmds() {
retry=0
while [[ \${ran_cmds} -eq 0 ]] ; do
sync
sleep 4
cli_entry=(\$(cli_client -qc | grep "fn_id: 0x4 .* vport_id \${ACC_VPORT_ID}" | sed 's/: / /g' | sed 's/addr //g'))
if [ \${#cli_entry[@]} -gt 1 ] ; then

        for (( id=0 ; id<\${#cli_entry[@]} ; id+=2 )) ;  do
                declare "\${cli_entry[id]}"="\${cli_entry[\$((id+1))]}"
                #echo "\${cli_entry[id]}"="\${cli_entry[\$((id+1))]}"
        done

        if [ X\${is_created} == X"yes" ] && [ X\${is_enabled} == X"yes" ] ; then
                IDPF_VPORT_VSI_HEX=\${vsi_id}
                VSI_GROUP_INIT=\$(printf  "0x%x" \$((0x8000050000000000 + IDPF_VPORT_VSI_HEX)))
                VSI_GROUP_WRITE=\$(printf "0x%x" \$((0xA000050000000000 + IDPF_VPORT_VSI_HEX)))
                echo "#Add to VSI Group 1 :  \${IDPF_VPORT_NAME} [vsi: \${IDPF_VPORT_VSI_HEX}]"
                # Try to acquire the lock
                while true ; do
                if ( set -o noclobber; echo \$$ > \${LOCKFILE} ) 2>/dev/null; then
                  echo "RunDevMemCmds_Start: LogFile->"
                  # Critical section - only one script can be here at a time
                  set -x
		  # OPCODE update to program the rx_phy_port_to_pr_map table default action with the correct vsi_id of D5 interface, which could potentially change
		  # per reboot of IMC.
		  # opcode 0x1305 is for DELETE an entry.
                  echo "opcode=0x1305 prof_id=0xb cookie=123 key=0x00,0x00,0x00,0x00 act=set_vsi{act_val=\${vsi_id} val_type=0 dst_pe=0 slot=0x0}" > $OPCODE_CFG_FILE
                  # opcode 0x1303 is for ADD an entry.
		  echo "opcode=0x1303 prof_id=0xb cookie=123 key=0x00,0x00,0x00,0x00 act=set_vsi{act_val=\${vsi_id} val_type=0 dst_pe=0 slot=0x0}" >> $OPCODE_CFG_FILE
		  cli_client -x -f $OPCODE_CFG_FILE
                  devmem 0x20292002a0 64 \${VSI_GROUP_INIT}
                  devmem 0x2029200388 64 0x1
                  devmem 0x20292002a0 64 \${VSI_GROUP_WRITE}
                  set +x
                  sync
                  if [ \${ran_cmds_cnt} -eq  \${ran_cmds_cnt_max} ]; then
                     echo "ran_cmds_cnt has reached maximum value, reset"
                     ran_cmds_cnt=0
                  fi
                  ran_cmds_cnt=\$((ran_cmds_cnt+1))
                  echo "RunDevMemCmds_End: LogFile, ran_cmds_cnt->"\${ran_cmds_cnt}
                  # Release the lock
                  release_lock
                  ran_cmds=1
                  break
                else
                  echo "RunDevMemCmds: Needs to wait,sleep"
                  sleep 1
                fi
                done
        fi
else
        retry=\$((retry+1))
        echo "RETRY: \${retry} : #Add to VSI Group 1 :  \${IDPF_VPORT_NAME} .. "
fi
done
}

# Function to check if D5 interface is up on ACC
d5_interface_up() {
  cli_entry=(\$(cli_client -qc | grep "fn_id: 0x4 .* vport_id \${ACC_VPORT_ID}" | sed 's/: / /g' | sed 's/addr //g'))
  if [ \${#cli_entry[@]} -gt 1 ] ; then
   return 1  # Success
  fi
  return 0
}

# Invokes run_devmem_cmds, upon startup, and periodically checks if D5 is alive, if not re-run.
while [[ \${ran_cmds} -eq 0 ]]; do
   echo "invoke run_devmem_cmds"
   run_devmem_cmds
   echo "ran_cmds->"\${ran_cmds}

   #Truncate log, if needed
   LOG_FILE_SIZE=\$(stat -c %s \${LOG_FILE})
   # Check if file size exceeds max
   if [ \${LOG_FILE_SIZE} -gt \${MAX_LOG_SIZE} ]; then
      echo "File exceeds maximum size. Truncating->"\${LOG_FILE}
      # Truncate the file, by saving the last 1000 lines
      X=\$(tail -1000 \${LOG_FILE})
      echo \${X}>\${LOG_FILE}
   fi

   #inner while
   while true ; do
   d5_interface_up
   if [[ \$? -eq 1 ]]; then
      #echo "D5 interface up, sleep"
      sleep 7
   else
      echo "D5 not found. ACC may have gone down, retry."
      ran_cmds=0
      break
   fi
   done
done
PORT_CONFIG_EOF

/usr/bin/chmod a+x ${PORT_SETUP_SCRIPT}
/usr/bin/nohup bash -c ''"${PORT_SETUP_SCRIPT}"' '"${PORT_SETUP_LOG}"'' 0>&- &> ${PORT_SETUP_LOG} &

PS_SCRIPT_NAME=$(basename ${PORT_SETUP_SCRIPT})

log_retry=0
while true ; do
   sync
if [[ $log_retry -gt 10 ]]; then
   echo "waited for log more than 10 secs, log not detected"
   if pgrep -x "${PS_SCRIPT_NAME}" > /dev/null 2>&1; then
      echo "Process '${PS_SCRIPT_NAME}' is running."
   else
      echo "Process '${PS_SCRIPT_NAME}' is not running."
   fi
   echo "waited for log more than 10 secs, 2nd attempt for port_setup.sh below"
   /usr/bin/chmod a+x ${PORT_SETUP_SCRIPT}
   /usr/bin/nohup bash -c ''"${PORT_SETUP_SCRIPT}"' '"${PORT_SETUP_LOG2}"'' 0>&- &> ${PORT_SETUP_LOG2} &
   echo "2nd attempt for port_setup.sh done"
   sleep 1
   sync
   break
fi
if [[ -s ${PORT_SETUP_LOG}  ]]; then
   echo "non-empty log file exists"
   sleep 1
   break
else
   log_retry=$((log_retry+1))
   if [ -f ${PORT_SETUP_LOG} ]; then
      echo "log file exists. but empty"
   else
      echo "log file doesnt exist"
   fi
   sleep 1
fi
done`

	return postInitAppScriptStr
}

func genLoadCustomPkgFile(macAddress string) string {

	p4PkgName := os.Getenv("P4_NAME") + ".pkg"
	shellScript := fmt.Sprintf(`#!/bin/sh
CP_INIT_CFG=/etc/dpcp/cfg/cp_init.cfg
cd /work/scripts
echo "Checking for custom package..."
if [ -e %s ]; then
    echo "Custom package %s found. Overriding default package"
    cp %s /etc/dpcp/package/
    rm -rf /etc/dpcp/package/default_pkg.pkg
    ln -s /etc/dpcp/package/%s /etc/dpcp/package/default_pkg.pkg
    sed -i 's/sem_num_pages = 1;/sem_num_pages = 256;/g' $CP_INIT_CFG
    sed -i 's/lem_num_pages = 6;/lem_num_pages = 32;/g' $CP_INIT_CFG
    sed -i 's/mod_num_pages = 1;/mod_num_pages = 2;/g' $CP_INIT_CFG
    sed -i 's/cxp_num_pages = 1;/cxp_num_pages = 6;/g' $CP_INIT_CFG
    sed -i 's/pf_mac_address = "00:00:00:00:03:14";/pf_mac_address = "%s";/g' $CP_INIT_CFG
    sed -i 's/acc_apf = 4;/acc_apf = %s;/g' $CP_INIT_CFG
    sed -i 's/comm_vports = .*/comm_vports = (([5,0],[4,0]),([0,3],[5,3]),([0,2],[4,3]));/g' $CP_INIT_CFG
    sed -i 's/uplink_vports = .*/uplink_vports = ([4,5,0],[5,1,0],[5,2,1]);/g' $CP_INIT_CFG
    sed -i 's/rep_vports = .*/rep_vports = ([4,5,0]);/g' $CP_INIT_CFG
    sed -i 's/exception_vports = .*/exception_vports = ([4,5,0]); /g' $CP_INIT_CFG
else
    echo "No custom package found. Continuing with default package"
fi
`, p4PkgName, p4PkgName, p4PkgName, p4PkgName, macAddress, strconv.Itoa(ApfNumber))

	return shellScript

}

/*
	IMC reboot needed for following cases:

Note: Changes to load_custom_pkg.sh or post_init_app.sh is managed
thro ipu-plugin->using genLoadCustomPkgFile and postInitAppScript.
1. First time provisioning of IPU system(where MAC gets set in node policy)
2. Upgrade-for any update to P4 package.
3. Upgrade-for node policy. Other changes in node policy thro load_custom_pkg.sh.
4. Check if file-> post_init_app.sh exists.
Returns-> bool(returns false, if IMC reboot is required), string->for any error or success string.
*/
func skipIMCReboot() (bool, string) {
	config := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.Password(""),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// Connect to the remote server.
	client, err := ssh.Dial("tcp", imcAddress, config)
	if err != nil {
		return false, fmt.Sprintf("failed to dial remote server: %s", err)
	}
	defer client.Close()

	// Start a session.
	session, err := client.NewSession()
	if err != nil {
		return false, fmt.Sprintf("failed to create session: %s", err)
	}
	defer session.Close()

	commands := "if [ -f /work/uuid ]; then cat /work/uuid; else echo 'File does not exist'; fi"

	// Run a command on the remote server and capture the output.
	outputBytes, err := session.CombinedOutput(commands)
	if err != nil {
		return false, fmt.Sprintf("mac not found: %s", err)
	}

	p4pkgMatch := false
	uuidFileExists := false
	lcpkgFileMatch := false
	piaFileMatch := false
	outputStr := strings.TrimSuffix(string(outputBytes), "\n")

	if outputStr == "File does not exist" {
		log.Infof("UUID File does not exist")
	} else {
		log.Infof("UUID File exists, uuid->%v", outputStr)
		uuidFileExists = true
	}
	if !uuidFileExists {
		return false, "UUID File does not exist"
	}

	p4PkgName := os.Getenv("P4_NAME") + ".pkg"
	imcPath := "/work/scripts/" + p4PkgName
	vspPath := "/" + p4PkgName
	p4pkgMatch, errStr := utils.CompareBinary(imcPath, vspPath, client)

	if !p4pkgMatch {
		return false, errStr
	}

	genLcpkgFileStr := genLoadCustomPkgFile(outputStr)
	log.Infof("loadCustomPkgFileStr->%v", genLcpkgFileStr)
	// destination file on IMC.
	remoteFilePath := "/work/scripts/load_custom_pkg.sh"
	lcpkgFileMatch, errStr = utils.CompareFile(genLcpkgFileStr, remoteFilePath, client)

	if !lcpkgFileMatch {
		return false, errStr
	}

	postInitAppFile := postInitAppScript()
	postInitRemoteFilePath := "/work/scripts/post_init_app.sh"
	piaFileMatch, errStr = utils.CompareFile(postInitAppFile, postInitRemoteFilePath, client)

	if !piaFileMatch {
		return false, errStr
	}

	log.Infof("uuidFileExists->%v, p4pkgMatch->%v, lcpkgFileMatch->%v, piaFileMatch->%v",
		uuidFileExists, p4pkgMatch, lcpkgFileMatch, piaFileMatch)
	return true, "checks pass, imc reboot not required"

}

func (e *ExecutableHandlerImpl) validate() bool {

	if numAPFs := countAPFDevices(); numAPFs < ApfNumber {
		log.Errorf("Not enough APFs %v, expected->%v", numAPFs, ApfNumber)
		return false
	}
	if noReboot, infoStr := skipIMCReboot(); !noReboot {
		fmt.Printf("IMC reboot required : %v\n", infoStr)
		return false
	}

	return true
}

func (e *ExecutableHandlerImpl) SetupAccApfs() error {
	var err error

	if !InitAccApfMacs {
		AccApfMacList, err = utils.GetAccApfMacList()

		if err != nil {
			log.Errorf("SetupAccApfs: Error-> %v", err)
			return fmt.Errorf("SetupAccApfs: Error-> %v", err)
		}

		if len(AccApfMacList) != ApfNumber {
			log.Errorf("not enough APFs initialized on ACC, total APFs->%d, APFs->%v", len(AccApfMacList), AccApfMacList)
			return fmt.Errorf("not enough APFs initialized on ACC, total APFs->%d", len(AccApfMacList))
		}
		log.Infof("On ACC, total APFs->%d", len(AccApfMacList))
		for i := 0; i < len(AccApfMacList); i++ {
			log.Infof("index->%d, mac->%s", i, AccApfMacList[i])
		}
		InitAccApfMacs = true
	}
	return nil
}

// If ipu-plugin's Init function gets invoked on ACC, prior to getting invoked
// on x86, then host VFs will not be setup yet. In that case, peer2peer rules
// will get added in CreateBridgePort or CreateNetworkFunction.
func CheckAndAddPeerToPeerP4Rules(p types.P4RTClient) {
	if !PeerToPeerP4RulesAdded {
		vfMacList, err := utils.GetVfMacList()
		if err != nil {
			log.Errorf("CheckAndAddPeerToPeerP4Rules: Error-> %v", err)
			return
		}
		if len(vfMacList) == 0 {
			log.Infof("No VFs initialized on the host yet")
		} else {
			log.Infof("AddPeerToPeerP4Rules, path->%s, vfMacList->%v", p.GetBin(), vfMacList)
			p4rtclient.AddPeerToPeerP4Rules(p, vfMacList)
			PeerToPeerP4RulesAdded = true
		}
	}
}

func (s *FXPHandlerImpl) configureFXP(p types.P4RTClient, brCtlr types.BridgeController) error {
	if !InitAccApfMacs {
		log.Errorf("configureFXP: AccApfs info not set, thro-> SetupAccApfs")
		return fmt.Errorf("configureFXP: AccApfs info not set, thro-> SetupAccApfs")
	}
	//Add Phy Port0 to ovs bridge
	//Note: Per current design, Phy Port1 is added to a different bridge(through P4 rules).
	if err := brCtlr.AddPort(AccIntfNames[PHY_PORT0_INTF_INDEX]); err != nil {
		log.Errorf("failed to add port to bridge: %v, for interface->%v", err, AccIntfNames[PHY_PORT0_INTF_INDEX])
		return fmt.Errorf("failed to add port to bridge: %v, for interface->%v", err, AccIntfNames[PHY_PORT0_INTF_INDEX])
	}
	//Add P4 rules for phy ports
	log.Infof("AddPhyPortRules, path->%s, 1->%v, 2->%v", p.GetBin(), AccApfMacList[PHY_PORT0_INTF_INDEX], AccApfMacList[PHY_PORT1_INTF_INDEX])
	p4rtclient.AddPhyPortRules(p, AccApfMacList[PHY_PORT0_INTF_INDEX], AccApfMacList[PHY_PORT1_INTF_INDEX])

	CheckAndAddPeerToPeerP4Rules(p)

	log.Infof("AddLAGP4Rules, path->%v", p.GetBin())
	p4rtclient.AddLAGP4Rules(p)

	//Add P4 rules to handle Primary network traffic via phy port0
	log.Infof("AddRHPrimaryNetworkVportP4Rules,  path->%s, 1->%v, 2->%v", p.GetBin(), AccApfMacList[PHY_PORT0_INTF_INDEX], AccApfMacList[PHY_PORT1_INTF_INDEX])
	p4rtclient.AddRHPrimaryNetworkVportP4Rules(p, AccApfMacList[PHY_PORT0_INTF_INDEX], AccApfMacList[PHY_PORT1_INTF_INDEX])

	return nil
}

func (s *LifeCycleServiceServer) Init(ctx context.Context, in *pb.InitRequest) (*pb.IpPort, error) {
	if s.initialized {
		return nil, fmt.Errorf("Error during init call, already initialized")
	}
	s.initialized = true

	InitHandlers()

	if in.DpuMode && s.mode != types.IpuMode || !in.DpuMode && s.mode != types.HostMode {
		return nil, status.Errorf(codes.Internal, "Ipu plugin running in %s mode", s.mode)
	}

	if in.DpuMode {
		if val := ExecutableHandlerGlobal.validate(); !val {
			log.Info("forcing state")
			if err := sshHandler.sshFunc(); err != nil {
				return nil, fmt.Errorf("error calling sshFunc %s", err)
			}
		} else {
			log.Info("not forcing state")
		}
		if err := ExecutableHandlerGlobal.SetupAccApfs(); err != nil {
			log.Errorf("error from  SetupAccApfs %v", err)
			return nil, fmt.Errorf("error from  SetupAccApfs %v", err)
		} else {
			log.Info("setup ACC APFs")
		}

		// Preconfigure the FXP with point-to-point rules between host VFs
		if err := fxpHandler.configureFXP(s.p4rtClient, s.bridgeCtlr); err != nil {
			return nil, status.Errorf(codes.Internal, "Error when preconfiguring the FXP: %v", err)
		}
	}

	checkIdpfNetDevices(s.mode)

	if err := configureChannel(s.mode, s.daemonHostIp, s.daemonIpuIp); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	response := &pb.IpPort{Ip: s.daemonIpuIp, Port: int32(s.daemonPort)}

	return response, nil
}
