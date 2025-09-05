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
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/types"
	"github.com/pkg/sftp"
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

const (
	maxRetryCnt = 5
	errStr      = "Process failure, err: -105"
	outputPath  = "/work/cli_output"
	retryDelay  = 500 * time.Millisecond
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

func ExtractVsiVportInfo(macAddr string) (int, int, error) {
	macAddrByte, err := GetMacAsByteArray(macAddr)
	if err != nil {
		log.Info("ExtractVsiVportInfo failed.", macAddr)
		return 0, 0, err
	}
	vfVsi := int(macAddrByte[1])
	vfVport := GetVportForVsi(vfVsi)
	return vfVsi, vfVport, nil
}

func GetVsiVportInfo(macAddr string) (int, int, error) {
	vsi, err := ImcQueryfindVsiGivenMacAddr(types.IpuMode, macAddr)
	if err != nil {
		log.Info("GetVsiVportInfo failed. Unable to find Vsi and Vport for mac: ", macAddr)
		return 0, 0, err
	}
	//skip 0x in front of vsi
	vsi = vsi[2:]
	vsiInt64, err := strconv.ParseInt(vsi, 16, 32)
	if err != nil {
		log.Info("error from ParseInt ", err)
		return 0, 0, err
	}
	vfVsi := int(vsiInt64)
	vfVport := GetVportForVsi(vfVsi)
	return vfVsi, vfVport, nil
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

var P4rtMutex sync.Mutex

func RunP4rtCtlCommand(p4rtBin string, p4rtIpPort string, params ...string) (string, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	//serialize access to p4rtCtlCommand, when they are concurrent callers to RunP4rtCtlCommand
	//Note::p4rtctl python client, uses fixed value(1 for election-id). So when they are concurrent
	//clients invoking it, p4 runtime server(infrap4d) throws error.
	P4rtMutex.Lock()
	defer P4rtMutex.Unlock()
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
		return stderr.String(), stdout.String(), err
	}

	log.WithField("params", params).Debugf("successfully executed %s", p4rtBin)
	return "", "", nil
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

	cliCmd := `set -o pipefail && cli_client -cq `
	subCmd := fmt.Sprintf(` | awk '{if(($17 == "%s")) {print $8}}'`, mac)
	outputBytes, err := RunCliCmdOnImc(cliCmd, subCmd)

	//Handle case where command ran without error, but empty output, due to config issue.
	if (err != nil) || (len(outputBytes) == 0) {
		log.Errorf("ImcQueryfindVsiGivenMacAddr: Error %v, from RunCmdOnImc (OR) empty (output)-%v", err, len(outputBytes))
		return "", fmt.Errorf("ImcQueryfindVsiGivenMacAddr: Error %v, from RunCmdOnImc (OR) empty (output)-%v", err, len(outputBytes))
	}

	outputStr := strings.TrimSpace(string(outputBytes))
	log.Infof("ImcQueryfindVsiGivenMacAddr: %s, len(output)-%v", outputStr, len(outputStr))

	return outputStr, err
}

// skips ACC interfaces D0 to D3, which are used internally. So, not available for other usages.
// $2 == 4 is to get ACC entries, and $10 check is to make sure, we skip rows that has vportIDs from D0 to D3.
func GetAvailableAccVsiList() ([]string, error) {
	// reach out to the IMC
	cliCmd := `set -o pipefail && cli_client -cq `
	subCmd := ` | awk '{if(($2 == "0x4") && ($10 != "0x0") && ($10 != "0x1") && ($10 != "0x2") && ($10 != "0x3")) {print $8}}'`
	outputBytes, err := RunCliCmdOnImc(cliCmd, subCmd)

	var outputStr []string
	//Handle case where command ran without error, but empty output, due to config issue.
	if (err != nil) || (len(outputBytes) == 0) {
		log.Errorf("GetAvailableAccVsiList: Error %v, from RunCmdOnImc (OR) empty (output)-%v", err, len(outputBytes))
		return outputStr, fmt.Errorf("GetAvailableAccVsiList: Error %v, from RunCmdOnImc (OR) empty (output)-%v", err, len(outputBytes))
	}

	outputStr = strings.Split(strings.TrimSpace(string(outputBytes)), "\n")
	log.Infof("GetAvailableAccVsiList: %s, len(output)-%v", outputStr, len(outputStr))

	return outputStr, err
}

// Note: Added retry logic, since ipumgmtd is single-threaded, so concurrent usage,
// of cli-client errors out.
// we retry 5 times, with sleep(0.5s) between retries. If it exceeds max
// attempts, we quit retry. retry logic is a best-effort attempt.
// WARNING: Even if ipumgmtd throws error, cli_client tool still
// returns success. Also this code(for retry) can break, if error string
// or error code gets changed.
func RunCliCmdOnImc(cliCmd, subCmd string) ([]byte, error) {
	config := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.Password(""), // Consider using SSH keys for security
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	// Establish SSH connection
	client, err := ssh.Dial("tcp", imcAddress, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", imcAddress, err)
	}
	defer client.Close()

	var outputBytes []byte

	for retry := 0; retry < maxRetryCnt; retry++ {
		log.Printf("Attempt %d/%d: Executing command", retry+1, maxRetryCnt)

		session, err := client.NewSession()
		if err != nil {
			return nil, fmt.Errorf("failed to create SSH session: %w", err)
		}

		// Run the CLI command
		outputBytes, err = session.CombinedOutput(cliCmd)
		session.Close() // Close session explicitly
		outputStr := string(outputBytes)

		if err != nil {
			log.Printf("Command failed: %v", err)
			return nil, fmt.Errorf("command execution failed: %w", err)
		}

		if strings.Contains(outputStr, errStr) {
			log.Printf("Retrying due to detected error: %s", errStr)
			time.Sleep(retryDelay)
			continue
		}

		if subCmd != "" {
			// Create SFTP client
			sftpClient, err := sftp.NewClient(client)
			if err != nil {
				return nil, fmt.Errorf("failed to create SFTP client: %w", err)
			}
			defer sftpClient.Close()

			// Copy output to file
			if err := CopyFile(outputStr, "/work/cli_output", sftpClient); err != nil {
				return nil, fmt.Errorf("failed to copy file: %w", err)
			}

			// Execute the sub-command
			session, err = client.NewSession()
			if err != nil {
				return nil, fmt.Errorf("failed to create SSH session: %w", err)
			}
			defer session.Close() // Ensure closure of session

			fullCmd := fmt.Sprintf("set -o pipefail && cat /work/cli_output %s", subCmd)
			outputBytes, err = session.CombinedOutput(fullCmd)
			if err != nil {
				return nil, fmt.Errorf("sub-command execution failed: %w", err)
			}
		}
		return outputBytes, nil
	}

	return nil, fmt.Errorf("max retry count (%d) reached", maxRetryCnt)
}

func GetAccApfMacList() ([]string, error) {
	cliCmd := `set -o pipefail && cli_client -cq `
	subCmd := ` | awk '{if(($2 == "0x4") && ($4 == "0x4")) {print $17}}'`
	outputBytes, err := RunCliCmdOnImc(cliCmd, subCmd)

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
	cliCmd := `set -o pipefail && cli_client -cq `
	subCmd := ` | awk '{if(($4 == "0x0") && ($6 == "yes")) {print $17}}'`
	outputBytes, err := RunCliCmdOnImc(cliCmd, subCmd)

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

// compares(using md5sum) binary on IMC, with equivalent binary built-into ipu-plugin(VSP) image.
// returns true, if binary checksum matches, false otherwise.
func CompareBinary(imcPath string, vspPath string, client *ssh.Client) (bool, string) {
	session, err := client.NewSession()
	if err != nil {
		log.Errorf("failed to create session: %v", err)
		return false, fmt.Sprintf("failed to create session: %v", err)
	}
	defer session.Close()

	//compute md5sum of pkg file on IMC
	commands := "set -o pipefail && " + "md5sum " + imcPath + " |  awk '{print $1}'"
	imcOutput, err := session.CombinedOutput(commands)
	if err != nil {
		log.Errorf("Error->%v, running command->%s:", err, commands)
		return false, fmt.Sprintf("Error->%v, running command->%s:", err, commands)
	}

	//compute md5sum of pkg file in ipu-plugin container
	commands = "set -o pipefail && " + "md5sum /" + vspPath + " |  awk '{print $1}'"
	pluginOutput, err := ExecuteScript(commands)
	if err != nil {
		log.Errorf("Error->%v, for md5sum command->%v", err, commands)
		return false, fmt.Sprintf("Error->%v, for md5sum command->%v", err, commands)
	}

	if pluginOutput == string(imcOutput) {
		log.Infof("md5sum match, imcPath-%s, in ipu-plugin->%v, on IMC->%v", imcPath, pluginOutput, string(imcOutput))
	} else {
		log.Infof("md5sum mismatch, imcPath-%s, in ipu-plugin->%v, on IMC->%v", imcPath, pluginOutput, string(imcOutput))
		return false, fmt.Sprintf("md5sum mismatch, imcPath-%s, in ipu-plugin->%v, on IMC->%v", imcPath, pluginOutput, string(imcOutput))
	}

	return true, ""
}

// compares(using md5sum) file on IMC, with equivalent file-content(in string format) as expected by ipu-plugin(VSP).
// returns true, if file checksum matches, false otherwise.
func CompareFile(inputFile string, remoteFilePath string, client *ssh.Client) (bool, string) {
	log.Infof("inputFile->%v", inputFile)
	inputFileHash := md5.Sum([]byte(inputFile))
	inputFileHashStr := hex.EncodeToString(inputFileHash[:])

	// Create an SFTP client.
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		log.Errorf("failed to create SFTP client: %s", err)
		return false, fmt.Sprintf("failed to create SFTP client: %s", err)
	}
	defer sftpClient.Close()

	// destination file on IMC.
	dstFile, err := sftpClient.Open(remoteFilePath)
	if err != nil {
		log.Errorf("failed to create remote file(%s): %s", remoteFilePath, err)
		return false, fmt.Sprintf("failed to create remote file(%s): %s", remoteFilePath, err)
	}
	defer dstFile.Close()

	imcFileBytes, err := io.ReadAll(dstFile)
	if err != nil {
		log.Errorf("failed to read %s: %s", remoteFilePath, err)
		return false, fmt.Sprintf("failed to read %s: %s", remoteFilePath, err)
	}

	imcFileHash := md5.Sum(imcFileBytes)
	imcFileHashStr := hex.EncodeToString(imcFileHash[:])

	if inputFileHashStr == imcFileHashStr {
		log.Infof("File->%s md5 match, generated->%v, on IMC->%v", remoteFilePath, inputFileHashStr, imcFileHashStr)
	} else {
		log.Infof("File->%s, md5 mismatch, generated->%v, on IMC->%v", remoteFilePath, inputFileHashStr, imcFileHashStr)
		return false, fmt.Sprintf("File->%s, md5 mismatch, generated->%v, on IMC->%v", remoteFilePath, inputFileHashStr, imcFileHashStr)
	}

	return true, ""
}

// copies file content(in string) onto IMC(to the path provided)
func CopyFile(inputFile string, remoteFilePath string, sftpClient *sftp.Client) error {
	log.Infof("copyFile: remoteFilePath->%v", remoteFilePath)

	remoteFile, err := sftpClient.Create(remoteFilePath)
	if err != nil {
		log.Errorf("failed to create remote file->%v: %v", remoteFilePath, err)
		return fmt.Errorf("failed to create remote file->%v: %v", remoteFilePath, err)
	}
	defer remoteFile.Close()

	_, err = remoteFile.Write([]byte(inputFile))
	if err != nil {
		log.Errorf("failed to write %v: %v", remoteFilePath, err)
		return fmt.Errorf("failed to write %v: %v", remoteFilePath, err)
	}

	err = remoteFile.Sync()
	if err != nil {
		log.Errorf("failed to sync %v: %v", remoteFilePath, err)
		return fmt.Errorf("failed to sync %v: %v", remoteFilePath, err)
	}

	err = sftpClient.Chmod(remoteFilePath, 0755)
	if err != nil {
		log.Errorf("failed to chmod %v : %v", remoteFilePath, err)
		return fmt.Errorf("failed to chmod %v : %v", remoteFilePath, err)
	}
	return nil
}

// copies binary part of ipu-plugin(VSP) image onto IMC(to the path provided)
func CopyBinary(imcPath string, vspPath string, sftpClient *sftp.Client) error {

	// Open the source file.
	srcFile, err := os.Open(vspPath)
	if err != nil {
		log.Errorf("failed to open local file-%v: %v", vspPath, err)
		return fmt.Errorf("failed to open local file-%v: %v", vspPath, err)
	}
	defer srcFile.Close()

	dirPath := filepath.Dir(imcPath)
	// create any missing directories along the path to file in IMC.
	err = sftpClient.MkdirAll(dirPath)
	if err != nil {
		log.Errorf("error-%v from MkdirAll-for dirPath: %v", err, dirPath)
		return fmt.Errorf("error-%v from MkdirAll-for dirPath: %v", err, dirPath)
	}

	// Create the destination file on the remote server.
	dstFile, err := sftpClient.Create(imcPath)
	if err != nil {
		log.Errorf("failed to create remote file-%v: %v", imcPath, err)
		return fmt.Errorf("failed to create remote file-%v: %v", imcPath, err)
	}
	defer dstFile.Close()

	// Copy the file contents to the destination file.
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		log.Errorf("failed to copy file, vspPath-%v: %v", vspPath, err)
		return fmt.Errorf("failed to copy file, vspPath-%v: %v", vspPath, err)
	}

	// Ensure that the file is written to the remote filesystem.
	err = dstFile.Sync()
	if err != nil {
		log.Errorf("failed to sync file, imcPath-%v: %v", imcPath, err)
		return fmt.Errorf("failed to sync file, imcPath-%v: %v", imcPath, err)
	}
	return nil
}

func RestoreRHPrimaryNetwork() {
	remoteCliCmd := "set -o pipefail && /work/scripts/post_init_app.sh"
	_, err := RunCliCmdOnImc(remoteCliCmd, "")
	if err != nil {
		log.Info("RunCliCmdOnImc: Warning!. Unable to restore primary network access for to this IPU-ACC")
	}
}
