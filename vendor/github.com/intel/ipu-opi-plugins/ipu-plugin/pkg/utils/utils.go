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
)

const (
	vsiToVportOffset = 16
	pbPythonEnvVar   = "PROTOCOL_BUFFERS_PYTHON_IMPLEMENTATION=python"
	hostToImcIpAddr  = "100.0.0.100"
	accToImcIpAddr   = "192.168.0.1"
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
	var ipAddr string
	if mode == types.HostMode {
		ipAddr = hostToImcIpAddr
	} else if mode == types.IpuMode {
		ipAddr = accToImcIpAddr
	}

	runCommand := fmt.Sprintf(`ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 root@"%s" "/usr/bin/cli_client -cq" \
		| awk '{if(($17 == "%s")) {print $8}}'`, ipAddr, mac)

	output, err := ExecuteScript(runCommand)
	output = strings.TrimSpace(string(output))

	if err != nil || output == "" {
		log.Errorf("unable to reach IMC %v or null output->%v", err, output)
		return "", fmt.Errorf("unable to reach IMC %v or null output->%v", err, output)
	}
	return output, nil
}

func GetVfMacList() ([]string, error) {
	// reach out to the IMC to get the mac addresses of the VFs
	output, err := ExecuteScript(`ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 root@192.168.0.1 "/usr/bin/cli_client -cq" \
		| awk '{if(($4 == "0x0") && ($6 == "yes")) {print $17}}'`)

	if err != nil {
		return nil, fmt.Errorf("unable to reach the IMC %v", err)
	}

	return strings.Split(strings.TrimSpace(output), "\n"), nil
}

func GetAccApfMacList() ([]string, error) {
	// reach out to the IMC to get the mac addresses of the VFs
	output, err := ExecuteScript(`ssh -o StrictHostKeyChecking=no -o ConnectTimeout=10 root@192.168.0.1 "/usr/bin/cli_client -cq" \
		| awk '{if(($2 == "0x4") && ($4 == "0x4")) {print $17}}'`)

	if err != nil {
		return nil, fmt.Errorf("unable to reach the IMC %v", err)
	}

	return strings.Split(strings.TrimSpace(output), "\n"), nil
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
