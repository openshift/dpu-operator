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

func RunP4rtCtlCommand(p4RtBin string, params ...string) error {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := p4rtCtlCommand(p4RtBin, params...)

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
		}).Errorf("error while executing %s", p4RtBin)
		return err
	}

	log.WithField("params", params).Debugf("successfully executed %s", p4RtBin)
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
