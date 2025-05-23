package dhcp

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	DHCP_SERVER_LEASE_FILE = "/var/lib/dhcpd/dhcpd.leases"
	IS_ASSIGN_FLAG         = "SynaXG-Opr"
)
const (
	FAIL_LOAD_STATUS int = 0xFF
	FAIL_STATUS      int = 1
	SUCC_STATUS      int = 0
)

func StartDhcpServer() (int, error) {
	var (
		exitCode int
	)
	//build dynamic dhcpd.conf
	code, er := buildDhcpdConfFile()
	if er != nil {
		return code, er
	}

	//execute command to start dhcp server
	log.Printf("start dhcp server entry...")
	commandStr2 := "nohup dhcpd -f -cf /dhcpd.conf net1 > /dev/null 2>&1 &"
	cmd2 := exec.Command("sh", "-c", commandStr2)
	err := cmd2.Run()
	if err != nil {
		log.Printf("Run inner dhcp command failed. %v", err)
		exitCode = FAIL_LOAD_STATUS
		return exitCode, err
	}

	//Check whether the IP assignment was successful
	for {
		file_exist, err := IsPathExist(DHCP_SERVER_LEASE_FILE)
		if err == nil && file_exist {
			log.Printf("dhcp server start completed")
			//check ip assignment
			isOk, err := CheckFileContents(DHCP_SERVER_LEASE_FILE, IS_ASSIGN_FLAG)
			if isOk && err == nil {
				log.Printf("dhcp server assignment ip completed.")
				break
			}
		}
		log.Printf("Waiting for dhcp server assign ip ...")
		time.Sleep(5 * time.Second)
	}
	return SUCC_STATUS, nil
}

func getNet1IpAddr() (string, error) {
	//eg: ifconfig net1 | grep 'inet ' | awk '{print $2}'
	commandStr := "ifconfig net1 | grep 'inet ' | awk '{print $2}'"
	log.Printf("command: %s", commandStr)
	cmd := exec.Command("sh", "-c", commandStr)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		log.Printf("Run ifconfig command failed. %v,stderr:%s", err, stderr.String())
		return "", err
	}
	log.Printf("ifconfig result: %s \n", out.String())
	outputStr := strings.TrimSpace(out.String())
	if len(outputStr) == 0 {
		log.Printf("execute ifconfig result is empty")
		return "", errors.New("execute ifconfig result is empty")
	}
	return outputStr, nil
}

func buildDhcpdConfFile() (int, error) {
	var (
		exitCode int
	)
	//obtain server ip
	ip, err := getNet1IpAddr()
	if err != nil || ip == "" {
		exitCode = FAIL_LOAD_STATUS
		return exitCode, err
	}
	ipElems := strings.Split(ip, ".")
	if len(ipElems) <= 0 {
		log.Printf("split ip len <= 0")
		return FAIL_LOAD_STATUS, errors.New("split net1 server ip len <= 0")
	} else {
		var ipNum [4]int64
		ipNum[0], err = strconv.ParseInt(ipElems[0], 10, 64)
		if err != nil {
			return FAIL_LOAD_STATUS, err
		}
		ipNum[1], err = strconv.ParseInt(ipElems[1], 10, 64)
		if err != nil {
			return FAIL_LOAD_STATUS, err
		}
		ipNum[2], err = strconv.ParseInt(ipElems[2], 10, 64)
		if err != nil {
			return FAIL_LOAD_STATUS, err
		}
		ipNum[3], err = strconv.ParseInt(ipElems[3], 10, 64)
		if err != nil {
			return FAIL_LOAD_STATUS, err
		}

		serverIp := "192.168.1.0"
		ipRange := "192.168.1.1 192.168.1.14"
		if ipNum[3] >= 1 && ipNum[3] <= 14 {
			serverIp = fmt.Sprintf("%d.%d.%d.0", ipNum[0], ipNum[1], ipNum[2])
			ipRange = fmt.Sprintf("range %d.%d.%d.%d %d.%d.%d.%d;", ipNum[0], ipNum[1], ipNum[2], 1, ipNum[0], ipNum[1], ipNum[2], 14)
		}
		if ipNum[3] >= 17 && ipNum[3] <= 30 {
			serverIp = fmt.Sprintf("%d.%d.%d.0", ipNum[0], ipNum[1], ipNum[2])
			ipRange = fmt.Sprintf("range %d.%d.%d.%d %d.%d.%d.%d;", ipNum[0], ipNum[1], ipNum[2], 17, ipNum[0], ipNum[1], ipNum[2], 30)
		}
		if ipNum[3] >= 33 && ipNum[3] <= 46 {
			serverIp = fmt.Sprintf("%d.%d.%d.0", ipNum[0], ipNum[1], ipNum[2])
			ipRange = fmt.Sprintf("range %d.%d.%d.%d %d.%d.%d.%d;", ipNum[0], ipNum[1], ipNum[2], 33, ipNum[0], ipNum[1], ipNum[2], 46)
		}

		confStr := fmt.Sprintf(
			"#ddns-update-style none;\n"+
				"authoritative;\n"+
				"#log-facility local7;\n"+
				"option serverip code 43 = string;\n"+
				"subnet %s netmask 255.255.255.0 {\n"+
				"\t%s\n"+
				"\toption subnet-mask 255.255.255.0;\n"+
				"\toption domain-name-servers 8.8.8.8, 8.8.4.4;\n"+
				"}\n", serverIp, ipRange)
		log.Printf("dhcpd.conf contexts:\n %s", confStr)
		// write a file
		err := os.WriteFile("/dhcpd.conf", []byte(confStr), 0644)
		if err != nil {
			fmt.Printf("Error writing to file: %v\n", err)
			return FAIL_LOAD_STATUS, err
		}
		return SUCC_STATUS, nil
	}
}

func IsPathExist(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func CheckFileContents(filePath, searchString string) (bool, error) {
	// open scan file
	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("Error: %v\n", err)
		return false, err
	}
	defer file.Close()

	// create a scanner
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		log.Printf("lease file line %s", line)
		if strings.Contains(line, searchString) {
			log.Printf("contains %s", searchString)
			return true, nil
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error: %v\n", err)
		return false, err
	}
	return false, nil
}
