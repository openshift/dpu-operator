// Copyright (c) 2023 Intel Corporation.  All Rights Reserved.
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

package utils

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/types"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

const (
	vsiToVportOffset = 16
	pbPythonEnvVar   = "PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION=python"
	hostToImcIpAddr  = "100.0.0.100"
	accToImcIpAddr   = "192.168.0.1"
	imcAddress       = "192.168.0.1:22"
)

var execCommand = exec.Command

func ExecOsCommand(cmdBin string, params ...string) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := execCommand(cmdBin, params...)

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		log.WithFields(log.Fields{
			"params": params,
			"err":    err,
			"stdout": stdout.String(),
			"stderr": stderr.String(),
		}).Errorf("error while executing %s", cmdBin)
		return err
	}

	log.WithField("params", params).Debugf("successfully executed %s", cmdBin)
	return nil
}

func GetVportForVsi(vsi int) int {
	return vsi + vsiToVportOffset
}

func GetMacAsByteArray(macAddr string) ([]byte, error) {
	mAddr, err := net.ParseMAC(macAddr)
	if err != nil {
		return nil, fmt.Errorf("error parsing MAC address: %v", err)
	}
	return mAddr, nil
}

func GetMacIntValueFromBytes(macAddr []byte) uint64 {
	// see how this works: https://go.dev/play/p/MZnMiotnew2
	hwAddr := net.HardwareAddr(macAddr)
	macStr := strings.Replace(hwAddr.String(), ":", "", -1)
	macToInt, _ := strconv.ParseUint(macStr, 16, 64)
	return macToInt
}

var p4rtCtlCommand = exec.Command

func RunP4rtCtlCommand(p4rtBin string, p4rtIpPort string, params ...string) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := p4rtCtlCommand(p4rtBin, append([]string{"-g", p4rtIpPort}, params...)...)

	// Set required env var for python implemented protobuf
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, pbPythonEnvVar)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		log.WithFields(log.Fields{
			"params": params,
			"err":    err,
			"stdout": stdout.String(),
			"stderr": stderr.String(),
		}).Errorf("error while executing %s", p4rtBin)
		return err
	}

	log.WithField("params", params).Debugf("successfully executed %s", p4rtBin)
	return nil
}

func ExecuteScript(script string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.Command("sh", "-c", script)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("ExecuteScript: error %s, %s, %v", stdout.String(), stderr.String(), err)
	}
	return stdout.String(), nil
}

func ImcQueryfindVsiGivenMacAddr(mode string, mac string) (string, error) {
	if (mode == types.HostMode) || (mode != types.IpuMode) {
		log.Errorf("ImcQueryfindVsiGivenMacAddr: invalid mode-%v. access from host to IMC, not supported", mode)
		return "", fmt.Errorf("ImcQueryfindVsiGivenMacAddr: invalid mode-%v. access from host to IMC, not supported", mode)
	}

	commands := fmt.Sprintf(`set -o pipefail && cli_client -cq | awk '{if(($17 == "%s")) {print $8}}'`, mac)
	outputBytes, err := RunCmdOnImc(commands)

	//Handle case where command ran without error, but empty output, due to config issue.
	if (err != nil) || (len(outputBytes) == 0) {
		log.Errorf("ImcQueryfindVsiGivenMacAddr: Error %v, from RunCmdOnImc (OR) empty (output)-%v", err, len(outputBytes))
		return "", fmt.Errorf("ImcQueryfindVsiGivenMacAddr: Error %v, from RunCmdOnImc (OR) empty (output)-%v", err, len(outputBytes))
	}

	outputStr := strings.TrimSpace(string(outputBytes))
	log.Infof("ImcQueryfindVsiGivenMacAddr: %s, len(output)-%v", outputStr, len(outputStr))

	return outputStr, err
}

func RunCmdOnImc(cmd string) ([]byte, error) {

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
		log.Errorf("failed to dial remote server(%s): %s", imcAddress, err)
		return nil, fmt.Errorf("failed to dial remote server(%s): %s", imcAddress, err)
	}
	defer client.Close()

	// Start a session.
	session, err := client.NewSession()
	if err != nil {
		log.Errorf("failed to create ssh session: %s", err)
		return nil, fmt.Errorf("failed to create ssh session: %s", err)
	}
	defer session.Close()

	// Run a command on the remote server and capture the output.
	outputBytes, err := session.CombinedOutput(cmd)
	if err != nil {
		log.Errorf("cmd error: %s", err)
		return nil, fmt.Errorf("cmd error: %s", err)
	}

	return outputBytes, nil

}

func GetAccApfMacList() ([]string, error) {
	commands := `set -o pipefail && cli_client -cq | awk '{if(($2 == "0x4") && ($4 == "0x4")) {print $17}}'`
	outputBytes, err := RunCmdOnImc(commands)

	var outputStr []string
	//Handle case where command ran without error, but empty output, due to config issue.
	if (err != nil) || (len(outputBytes) == 0) {
		log.Errorf("GetAccApfMacList: Error %v, from RunCmdOnImc (OR) empty (output)-%v", err, len(outputBytes))
		return outputStr, fmt.Errorf("GetAccApfMacList: Error %v, from RunCmdOnImc (OR) empty (output)-%v", err, len(outputBytes))
	}

	outputStr = strings.Split(strings.TrimSpace(string(outputBytes)), "\n")
	log.Infof("GetAccApfMacList: %s, len(output)-%v", outputStr, len(outputStr))

	return outputStr, err
}

func GetVfMacList() ([]string, error) {
	// reach out to the IMC to get the mac addresses of the VFs
	commands := `set -o pipefail && cli_client -cq | awk '{if(($4 == "0x0") && ($6 == "yes")) {print $17}}'`
	outputBytes, err := RunCmdOnImc(commands)

	var outputStr []string
	//Handle case where command ran without error, but empty output, due to config issue.
	if (err != nil) || (len(outputBytes) == 0) {
		log.Errorf("GetVfMacList: Error %v, from RunCmdOnImc (OR) empty (output)-%v", err, len(outputBytes))
		return outputStr, fmt.Errorf("GetVfMacList: Error %v, from RunCmdOnImc (OR) empty (output)-%v", err, len(outputBytes))
	}

	outputStr = strings.Split(strings.TrimSpace(string(outputBytes)), "\n")
	log.Infof("GetVfMacList: %s, len(output)-%v", outputStr, len(outputStr))

	return outputStr, err
}

// Taken from the IPDK k8s-infra-offload project instead of including the full project as a module
// https://github.com/ipdk-io/k8s-infra-offload/blob/30f0efb483d177df7ce052d014593c1a17def8bc/pkg/utils/utils.go#L659
//
// VerifiedFilePath validates a file for potential file path traversal attacks.
// It returns the real filepath after cleaning and evaluiating any symlinks in the path.
// It returns error if the "fileName" is not within the "allowedDir", point to a non-privileged location or "fileName" points to a file outside of allowed dir.
func VerifiedFilePath(fileName string, allowedDir string) (string, error) {
	path := fileName
	path = filepath.Clean(path)
	if fileInfo, err := os.Lstat(path); err == nil {
		realPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", fmt.Errorf("Unsafe or invalid path specified. %s", err)
		}

		if fileInfo.Mode()&os.ModeSymlink == os.ModeSymlink {
			return "", fmt.Errorf("file %s is a symlink", fileName)
		}
		path = realPath
	}

	inTrustedRoot := func(path string) error {
		p := path
		for p != "/" {
			p = filepath.Dir(p)
			if p == allowedDir {
				return nil
			}
		}
		return fmt.Errorf("path: %s is outside of permissible directory: %s", path, allowedDir)
	}

	err := inTrustedRoot(path)
	if err != nil {
		return "", fmt.Errorf("Unsafe or invalid path specified. %s", err.Error())
	}
	return path, nil
}
