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
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	math_rand "math/rand"
	"net"
	"os"
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
	p4rtbin      string
	bridgeCtlr   types.BridgeController
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

func NewLifeCycleService(daemonHostIp, daemonIpuIp string, daemonPort int, mode string, p4rtbin string, brCtlr types.BridgeController) *LifeCycleServiceServer {
	return &LifeCycleServiceServer{
		daemonHostIp: daemonHostIp,
		daemonIpuIp:  daemonIpuIp,
		daemonPort:   daemonPort,
		mode:         mode,
		p4rtbin:      p4rtbin,
		bridgeCtlr:   brCtlr,
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
	configureFXP(p4rtbin string, brCtlr types.BridgeController) error
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

func FindInterfaceIdForGivenMac(macAddr string) (int, error) {
	intfIndex := 0
	found := false
	if !InitAccApfMacs {
		log.Errorf("FindInterfaceIdForGivenMac: AccApfs info not set, thro-> SetupAccApfs")
		return 0, fmt.Errorf("FindInterfaceIdForGivenMac: AccApfs info not set, thro-> SetupAccApfs")
	}
	for i := 0; i < len(AccApfMacList); i++ {
		if AccApfMacList[i] == macAddr {
			intfIndex = i
			log.Debugf("found intfIndex->%v for mac->%v\n", intfIndex, macAddr)
			found = true
			break
		}
	}
	if found == true {
		return intfIndex, nil
	}
	log.Errorf("Couldnt find intfIndex for mac->%v\n", macAddr)
	return 0, fmt.Errorf("Couldnt find intfIndex for mac->%v\n", macAddr)
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

	// Open the source file.
	p4PkgName := os.Getenv("P4_NAME") + ".pkg"
	localFilePath := "/" + p4PkgName
	srcFile, err := os.Open(localFilePath)
	if err != nil {
		return fmt.Errorf("failed to open local file: %s", err)
	}
	defer srcFile.Close()

	// Create the destination file on the remote server.
	remoteFilePath := "/work/scripts/" + p4PkgName
	dstFile, err := sftpClient.Create(remoteFilePath)
	if err != nil {
		return fmt.Errorf("failed to create remote file: %s", err)
	}
	defer dstFile.Close()

	// Copy the file contents to the destination file.
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return fmt.Errorf("failed to copy file: %s", err)
	}

	// Ensure that the file is written to the remote filesystem.
	err = dstFile.Sync()
	if err != nil {
		return fmt.Errorf("failed to sync file: %s", err)
	}

	// Start a session.
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %s", err)
	}
	defer session.Close()

	// Append python script to configure the ACC
	commands := `echo "python /usr/bin/scripts/cfg_acc_apf_x2.py" >> /work/scripts/pre_init_app.sh`
	err = session.Run(commands)
	if err != nil {
		return fmt.Errorf("failed to run commands: %s", err)
	}

	macAddress, err := setBaseMacAddr()
	if err != nil {
		return fmt.Errorf("error from setBaseMacAddr()->%v", err)
	}

	shellScript := genLoadCustomPkgFile(macAddress)

	loadCustomPkgFilePath := "/work/scripts/load_custom_pkg.sh"
	loadCustomPkgFile, err := sftpClient.Create(loadCustomPkgFilePath)
	if err != nil {
		return fmt.Errorf("failed to create remote load_custom_pkg.sh: %s", err)
	}
	defer loadCustomPkgFile.Close()

	_, err = loadCustomPkgFile.Write([]byte(shellScript))
	if err != nil {
		return fmt.Errorf("failed to write to load_custom_pkg.sh: %s", err)
	}

	err = loadCustomPkgFile.Sync()
	if err != nil {
		return fmt.Errorf("failed to sync load_custom_pkg.sh: %s", err)
	}

	uuidFilePath := "/work/uuid"
	uuidFile, err := sftpClient.Create(uuidFilePath)
	if err != nil {
		return fmt.Errorf("failed to create remote uuid file: %s", err)
	}
	defer uuidFile.Close()

	// Write the new MAC address to the uuid file.
	_, err = uuidFile.Write([]byte(macAddress + "\n"))
	if err != nil {
		return fmt.Errorf("failed to write to uuid file: %s", err)
	}

	// Ensure that the uuid file is written to the remote filesystem.
	err = uuidFile.Sync()
	if err != nil {
		return fmt.Errorf("failed to sync uuid file: %s", err)
	}

	session, err = client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %s", err)
	}
	defer session.Close()

	// Run a command on the remote server and capture the output.
	var stdoutBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	err = session.Run(commands)
	if err != nil {
		return fmt.Errorf("failed to run commands: %s", err)
	}

	session, err = client.NewSession()
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
    sed -i 's/acc_apf = 4;/acc_apf = 16;/g' $CP_INIT_CFG
    sed -i 's/comm_vports = .*/comm_vports = (([5,0],[4,0]),([0,3],[5,3]),([0,2],[4,3]));/g' $CP_INIT_CFG
    sed -i 's/uplink_vports = .*/uplink_vports = ([0,0,0],[0,1,1],[4,1,0],[4,5,1],[5,1,0],[5,2,1]);/g' $CP_INIT_CFG
    sed -i 's/rep_vports = .*/rep_vports = ([0,0,0],[4,5,1]);/g' $CP_INIT_CFG
    sed -i 's/exception_vports = .*/exception_vports = ([0,0,0],[4,5,1]); /g' $CP_INIT_CFG
else
    echo "No custom package found. Continuing with default package"
fi
`, p4PkgName, p4PkgName, p4PkgName, p4PkgName, macAddress)

	return shellScript

}

/*
	IMC reboot needed for following cases:

1. First time provisioning of IPU system(where MAC gets set in node policy)
2. Upgrade-for any update to P4 package.
3. Upgrade-for node policy. Other changes in node policy thro load_custom_pkg.sh.
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

	session, err = client.NewSession()
	if err != nil {
		log.Errorf("failed to create session: %v", err)
		return false, fmt.Sprintf("failed to create session: %v", err)
	}
	defer session.Close()

	//compute md5sum of pkg file on IMC
	p4PkgName := os.Getenv("P4_NAME") + ".pkg"
	commands = "cd /work/scripts; md5sum " + p4PkgName + " |  awk '{print $1}'"
	imcOutput, err := session.CombinedOutput(commands)
	if err != nil {
		log.Errorf("Error->%v, running command->%s:", err, commands)
		return false, fmt.Sprintf("Error->%v, running command->%s:", err, commands)
	}

	//compute md5sum of pkg file in ipu-plugin container
	commands = "md5sum /" + p4PkgName + " |  awk '{print $1}'"
	pluginOutput, err := utils.ExecuteScript(commands)
	if err != nil {
		log.Errorf("Error->%v, for md5sum command->%v", err, commands)
		return false, fmt.Sprintf("Error->%v, for md5sum command->%v", err, commands)
	}

	if pluginOutput != string(imcOutput) {
		log.Infof("md5sum mismatch, in ipu-plugin->%v, on IMC->%v", pluginOutput, string(imcOutput))
	} else {
		log.Infof("md5sum match, in ipu-plugin->%v, on IMC->%v", pluginOutput, string(imcOutput))
		p4pkgMatch = true
	}

	if !p4pkgMatch {
		return false, "md5sum mismatch"
	}

	genLcpkgFileStr := genLoadCustomPkgFile(outputStr)
	log.Infof("loadCustomPkgFileStr->%v", genLcpkgFileStr)
	genLcpkgFileHash := md5.Sum([]byte(genLcpkgFileStr))
	genLcpkgFileHashStr := hex.EncodeToString(genLcpkgFileHash[:])

	// Create an SFTP client.
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		log.Errorf("failed to create SFTP client: %s", err)
		return false, fmt.Sprintf("failed to create SFTP client: %s", err)
	}
	defer sftpClient.Close()

	// destination file on IMC.
	remoteFilePath := "/work/scripts/load_custom_pkg.sh"
	dstFile, err := sftpClient.Open(remoteFilePath)
	if err != nil {
		log.Errorf("failed to create remote file: %s", err)
		return false, fmt.Sprintf("failed to create remote file: %s", err)
	}
	defer dstFile.Close()

	imcLcpkgFileBytes, err := io.ReadAll(dstFile)
	if err != nil {
		log.Errorf("failed to read load_custom_pkg.sh: %s", err)
		return false, fmt.Sprintf("failed to read load_custom_pkg.sh: %s", err)
	}

	imcLcpkgFileHash := md5.Sum(imcLcpkgFileBytes)
	imcLcpkgFileHashStr := hex.EncodeToString(imcLcpkgFileHash[:])

	if genLcpkgFileHashStr != imcLcpkgFileHashStr {
		log.Infof("load_custom md5 mismatch, generated->%v, on IMC->%v", genLcpkgFileHashStr, imcLcpkgFileHashStr)
	} else {
		log.Infof("load_custom md5 match, generated->%v, on IMC->%v", genLcpkgFileHashStr, imcLcpkgFileHashStr)
		lcpkgFileMatch = true
	}

	if !lcpkgFileMatch {
		return false, "lcpkgFileMatch mismatch"
	}

	log.Infof("uuidFileExists->%v, p4pkgMatch->%v, lcpkgFileMatch->%v", uuidFileExists, p4pkgMatch, lcpkgFileMatch)
	return true, fmt.Sprintf("checks pass, imc reboot not required")

}

func (e *ExecutableHandlerImpl) validate() bool {

	/*Note: Num of APFs gets validated early on,
	in SetupAccApfs, prior to 1 interface(for Phy Port),
	getting moved to p4 container in configureFxp */

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
			log.Errorf("unable to reach the IMC %v", err)
			return fmt.Errorf("unable to reach the IMC %v", err)
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

func (s *FXPHandlerImpl) configureFXP(p4rtbin string, brCtlr types.BridgeController) error {
	vfMacList, err := utils.GetVfMacList()
	if err != nil {
		return fmt.Errorf("Unable to reach the IMC %v", err)
	}
	if len(vfMacList) == 0 {
		return fmt.Errorf("No NFs initialized on the host")
	}
	if !InitAccApfMacs {
		log.Errorf("configureFXP: AccApfs info not set, thro-> SetupAccApfs")
		return fmt.Errorf("configureFXP: AccApfs info not set, thro-> SetupAccApfs")
	}
	//Add Phy Port0 to ovs bridge
	//Note: Per current design, Phy Port1 is added to a different bridge(through P4 rules).
	if err := brCtlr.AddPort(AccIntfNames[PHY_PORT0_INTF_INDEX]); err != nil {
		log.Errorf("failed to add port to bridge: %v, for interface->%v", err, AccIntfNames[PHY_PORT0_INTF_INDEX])
		//return fmt.Errorf("failed to add port to bridge: %v, for interface->%v", err, AccIntfNames[PHY_PORT0_INTF_INDEX])
	}
	//Add P4 rules for phy ports
	log.Infof("DeletePhyPortRules, path->%s, 1->%v, 2->%v", p4rtbin, AccApfMacList[PHY_PORT0_INTF_INDEX], AccApfMacList[PHY_PORT1_INTF_INDEX])
	p4rtclient.DeletePhyPortRules(p4rtbin, AccApfMacList[PHY_PORT0_INTF_INDEX], AccApfMacList[PHY_PORT1_INTF_INDEX])
	log.Infof("AddPhyPortRules, path->%s, 1->%v, 2->%v", p4rtbin, AccApfMacList[PHY_PORT0_INTF_INDEX], AccApfMacList[PHY_PORT1_INTF_INDEX])
	p4rtclient.AddPhyPortRules(p4rtbin, AccApfMacList[PHY_PORT0_INTF_INDEX], AccApfMacList[PHY_PORT1_INTF_INDEX])

	log.Infof("DeletePeerToPeerP4Rules, path->%s, vfMacList->%v", p4rtbin, vfMacList)
	p4rtclient.DeletePeerToPeerP4Rules(p4rtbin, vfMacList)
	log.Infof("AddPeerToPeerP4Rules, path->%s, vfMacList->%v", p4rtbin, vfMacList)
	p4rtclient.AddPeerToPeerP4Rules(p4rtbin, vfMacList)

	log.Infof("DeleteLAGP4Rules, path->%s", p4rtbin)
	p4rtclient.DeleteLAGP4Rules(p4rtbin)
	log.Infof("AddLAGP4Rules, path->%v", p4rtbin)
	p4rtclient.AddLAGP4Rules(p4rtbin)

	return nil
}

func (s *LifeCycleServiceServer) Init(ctx context.Context, in *pb.InitRequest) (*pb.IpPort, error) {
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
		if err := fxpHandler.configureFXP(s.p4rtbin, s.bridgeCtlr); err != nil {
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
