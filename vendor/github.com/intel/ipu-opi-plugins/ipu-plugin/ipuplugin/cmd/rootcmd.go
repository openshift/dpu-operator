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

package cmd

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"

	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/ipuplugin"
	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/p4rtclient"
	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/types"
	ut "github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/utils"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/vishvananda/netlink"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	configFilePath      = "/etc/ipu/"
	cliName             = "ipuplugin"
	defaultServingAddr  = "/var/run/dpu-daemon/vendor-plugin/vendor-plugin.sock"
	defaultServingPort  = 50152
	defaultServingProto = "unix"
	tenantBridgeName    = "br-infra"
	defaultLogDir       = "/var/log/ipuplugin"
	defaultBridge       = "ovs"
	defaultBridgeIntf   = "enp0s1f0d6"
	defaulP4Pkg         = "linux"
	defaultP4rtBin      = "/opt/p4/p4-cp-nws/bin/p4rt-ctl"
	defaultP4rtIpPort   = "vsp-p4-service.openshift-dpu-operator.svc.cluster.local"
	defaultP4Image      = ""
	defaultOvsCliDir    = "/usr/bin"
	defaultOvsDbPath    = "/opt/p4/p4-cp-nws/var/run/openvswitch/db.sock"
	defaultPortMuxVsi   = 0x0a //this is just a place-holder, since VSI can change.
	defaultP4BridgeName = "br0"
	defaultDaemonHostIp = "192.168.1.1"
	defaultDaemonIpuIp  = "192.168.1.2"
	defaultDaemonPort   = 50151
	p4rtPort            = "9559"
)

var (
	config struct {
		cfgFile       string
		port          int
		servingAddr   string
		servingProto  string
		bridgeName    string
		interfaceName string
		logDir        string
		ovsCliDir     string
		ovsDbPath     string
		bridgeType    string
		p4pkg         string
		p4rtbin       string
		p4rtName      string
		p4Image       string
		portMuxVsi    int
		verbosity     string
		mode          string
		daemonHostIp  string
		daemonIpuIp   string
		daemonPort    int
	}

	rootCmd = &cobra.Command{
		Use:   cliName,
		Short: "IPU plugin is a daemon that exposes a CNI gRPC backend for SR-IOV networking offload to Intel MEV.",
		Long: `IPU plugin is a daemon that exposes a CNI gRPC backend for SR-IOV networking offload to Intel MEV.
		`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validateConfigs()
		},
		Run: func(_ *cobra.Command, _ []string) {
			if err := logInit(viper.GetString("logDir"), viper.GetString("verbosity")); err != nil {
				exitWithError(err, 3)
			}
			servingAddr := viper.GetString("servingAddr")
			servingProto := viper.GetString("servingProto")
			port := viper.GetInt("port")
			bridgeName := viper.GetString("bridgeName")
			intf := viper.GetString("interface")
			ovsCliDir := viper.GetString("ovsCliDir")
			ovsDbPath := viper.GetString("ovsDbPath")
			bridgeType := viper.GetString("bridgeType")
			p4pkg := viper.GetString("p4pkg")
			p4rtbin := viper.GetString("p4rtbin")
			p4rtName := viper.GetString("p4rtName")
			p4Image := viper.GetString("p4Image")
			portMuxVsi := viper.GetInt("portMuxVsi")
			mode := config.mode
			daemonHostIp := viper.GetString("daemonHostIp")
			daemonIpuIp := viper.GetString("daemonIpuIp")
			daemonPort := viper.GetInt("daemonPort")

			log.Info("Initializing IPU plugin")
			if mode == types.IpuMode {
				vsi, err := findVsiForPfInterface(mode, intf)
				/*In case, where default P4 package is loaded with 4 APFs, this check,
				will fail. Note::For linux-P4 package, portMuxVsi(not used).
				For redhat P4 we would need this check.
				As a quick fix, restricting check to redhat P4 package only.
				TODO: If we revert to redhat P4 package(unlikely), this check can be moved else-where.
				*/
				if (err != nil) && (p4pkg == "redhat") {
					log.Errorf("Not able to find VSI->%d, for bridge interface->%v\n", vsi, intf)
					exitWithError(err, 5)
				} else {
					//Overwrite default value with the correct VSI for that interface.
					portMuxVsi = vsi
				}
			}
			log.WithFields(log.Fields{
				"servingAddr":  servingAddr,
				"servingProto": servingProto,
				"servingPort":  port,
				"bridgeName":   bridgeName,
				"interface":    intf,
				"ovsCliDir":    ovsCliDir,
				"ovsDbPath":    ovsDbPath,
				"bridgeType":   bridgeType,
				"p4pkg":        p4pkg,
				"p4rtbin":      p4rtbin,
				"p4rtName":     p4rtName,
				"p4Image":      p4Image,
				"portMuxVsi":   portMuxVsi,
				"mode":         mode,
				"daemonHostIp": daemonHostIp,
				"daemonIpuIp":  daemonIpuIp,
				"daemonPort":   daemonPort,
			}).Info("Configurations")

			brCtlr, brType := getBridgeController(bridgeName, bridgeType, ovsCliDir, ovsDbPath)
			// In case of failure, revert to using localhost:9559 which works for P4 in container
			// but not for P4 in pod. In case of P4 in pod in failure case, we will error out in the
			// waitForInfraP4d()
			p4rtIpPort, err := convertNameToIpAndPort(p4rtName)
			if err != nil {
				log.Warnf("Error %v while converting %s to IP. Using %s instead", err, p4rtName, p4rtIpPort)
			}

			p4Client := getP4Client(p4pkg, p4rtbin, p4rtIpPort, portMuxVsi, defaultP4BridgeName, brType)

			mgr := ipuplugin.NewIpuPlugin(port, brCtlr, p4Client, p4Image, servingAddr, servingProto, bridgeName, intf, ovsCliDir, mode, daemonHostIp, daemonIpuIp, daemonPort)
			if err := mgr.Run(); err != nil {
				exitWithError(err, 4)
			}
		},
	}
)

func findVsiForPfInterface(mode string, intfName string) (int, error) {

	var pfList []netlink.Link

	ipuplugin.InitHandlers()

	if err := ipuplugin.GetFilteredPFs(&pfList); err != nil {
		log.Errorf("configureChannel: err->%v from GetFilteredPFs", err)
		return 0, status.Error(codes.Internal, err.Error())
	}

	mac, err := ipuplugin.GetMacforNetworkInterface(intfName, pfList)
	if err != nil {
		return 0, err
	}
	vsi, err := ut.ImcQueryfindVsiGivenMacAddr(mode, mac)
	if err != nil {
		return 0, err
	}
	//skip 0x in front of vsi
	vsi = vsi[2:]
	vsiInt64, err := strconv.ParseInt(vsi, 16, 32)
	if err != nil {
		log.Errorf("error from ParseInt %v", err)
		return 0, fmt.Errorf("error from ParseInt %v", err)
	}
	vsiInt := int(vsiInt64)
	log.Debugf("Found VSI->%d, for interface->%v\n", vsiInt, intfName)

	return vsiInt, nil
}

func exitWithError(err error, exitCode int) {
	fmt.Fprintf(os.Stderr, "There was an error while executing %s: %s\n", cliName, err.Error())
	os.Exit(exitCode)
}

// Execute executes the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		exitWithError(err, 1)
	}
}
func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&config.cfgFile, "config", "", "config file (default is /etc/ipu/ipuplugin.yaml)")

	rootCmd.PersistentFlags().StringVar(&config.logDir, "logDir", defaultLogDir, "IPU plugin log directory")
	rootCmd.PersistentFlags().IntVar(&config.port, "port", defaultServingPort, "IPU plugin serving TCP port")
	rootCmd.PersistentFlags().StringVar(&config.servingAddr, "servingAddr", defaultServingAddr, "IPU plugin serving host")
	rootCmd.PersistentFlags().StringVar(&config.servingProto, "servingProto", defaultServingProto, "IPU plugin serving protocol: 'unix|tcp'")
	rootCmd.PersistentFlags().StringVar(&config.interfaceName, "interface", defaultBridgeIntf, "The uplink network interface name")
	rootCmd.PersistentFlags().StringVar(&config.bridgeName, "bridgeName", tenantBridgeName, "The bridgeName that IPU plugin will manage")
	rootCmd.PersistentFlags().StringVar(&config.ovsCliDir, "ovsCliDir", defaultOvsCliDir, "The directory where the ovs-vsctl is located")
	rootCmd.PersistentFlags().StringVar(&config.ovsDbPath, "ovsDbPath", defaultOvsDbPath, "Path to the OVS socket to connect to")
	rootCmd.PersistentFlags().StringVar(&config.bridgeType, "bridgeType", defaultBridge, "The bridge type that IPU plugin will manage")
	rootCmd.PersistentFlags().StringVar(&config.p4pkg, "p4pkg", defaulP4Pkg, "The P4 package plugin is running with")
	rootCmd.PersistentFlags().StringVar(&config.p4rtbin, "p4rtbin", defaultP4rtBin, "The directory where the p4rt-ctl binary is located")
	rootCmd.PersistentFlags().StringVar(&config.p4rtName, "p4rtName", defaultP4rtIpPort, "p4rt server full name to DNS lookup. Eg: vsp-p4-service.openshift-dpu-operator.svc.cluster.local")
	rootCmd.PersistentFlags().StringVar(&config.p4Image, "p4Image", defaultP4rtIpPort, "P4Image that needs to be pulled. If none is given, then P4IMAGE env is used")
	rootCmd.PersistentFlags().IntVar(&config.portMuxVsi, "portMuxVsi", defaultPortMuxVsi,
		"The port mux VSI number. This must be for the same interface from --interface flags")
	//Default Log level value is the warn level
	rootCmd.PersistentFlags().StringVarP(&config.verbosity, "verbosity", "v", log.InfoLevel.String(), "Log level (debug, info, warn, error, fatal, panic")
	rootCmd.PersistentFlags().StringVar(&config.daemonHostIp, "daemonHostIp", defaultDaemonHostIp, "Daemon address on host")
	rootCmd.PersistentFlags().StringVar(&config.daemonIpuIp, "daemonIpuIp", defaultDaemonIpuIp, "Daemon address on ipu")
	rootCmd.PersistentFlags().IntVar(&config.daemonPort, "daemonPort", defaultDaemonPort, "Daemon port port")

	// Determine plugin mode based on platform arch. i.e.; arm == "ipu" mode
	// Should the platform arch changes to amd64 in future then we will need to introduce the "mode" flag again
	config.mode = getPluginMode()

	// Update below list of flags for any new flags added/updated above to bind them via Viper
	flagList := []string{
		"logDir",
		"port",
		"servingAddr",
		"servingProto",
		"interface",
		"bridgeName",
		"ovsCliDir",
		"ovsDbPath",
		"bridgeType",
		"p4pkg",
		"p4rtbin",
		"p4rtName",
		"p4Image",
		"portMuxVsi",
		"verbosity",
		"daemonHostIp",
		"daemonIpuIp",
		"daemonPort",
	}

	for _, f := range flagList {
		if err := viper.BindPFlag(f, rootCmd.PersistentFlags().Lookup(f)); err != nil {
			fmt.Fprintf(os.Stderr, "There was an error while binding flags '%s'", err)
			os.Exit(1)
		}
	}
	fmt.Printf("Default Config, configFile=%s, bridgeName=%s bridgeType=%s daemonPort=%v daemonHostIp=%v daemonIpuIp=%v\n",
		viper.ConfigFileUsed(), viper.GetString("bridgeName"), viper.GetString("bridgeType"), viper.GetString("daemonPort"), viper.GetString("daemonHostIp"), viper.GetString("daemonIpuIp"))
	fmt.Printf("Default Config, interface=%s mode=%v ovsCliDir=%v ovsDbPath=%v p4pkg=%v p4rtbin=%v p4rtName=%v p4Image=%v servingPort=%v portMuxVsi=%d\n",
		viper.GetString("interface"), config.mode, viper.GetString("ovsCliDir"), viper.GetString("OvsDbPath"), viper.GetString("p4pkg"), viper.GetString("p4rtbin"), viper.GetString("p4rtName"), viper.GetString("p4Image"), viper.GetString("port"), viper.GetInt("portMuxVsi"))
	fmt.Printf("Default Config, servingAddr=%v servingProto=%v \n",
		viper.GetString("servingAddr"), viper.GetString("servingProto"))
}

func initConfig() {
	if config.cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(config.cfgFile)
	} else {
		// Search config in default location
		viper.AddConfigPath(configFilePath)
		viper.SetConfigType("yaml")
		viper.SetConfigName("ipuplugin.yaml")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}

func validateConfigs() error {
	if config.mode != types.HostMode && config.mode != types.IpuMode {
		return fmt.Errorf("invalid mode specified: %s", config.mode)
	}
	if !(config.p4pkg == "linux" || config.p4pkg == "redhat") {
		return fmt.Errorf("invalid p4pkg specified: %s", config.p4pkg)
	}
	return nil
}

func logInit(logDir string, logLevel string) error {
	if err := os.MkdirAll(logDir, 0644); err != nil {
		return err
	}

	logFilename := path.Join(logDir, cliName+".log")
	verifiedFileName, err := ut.VerifiedFilePath(logFilename, logDir)
	if err != nil {
		return err
	}

	logFile, err := os.OpenFile(verifiedFileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	mw := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(mw)
	lgLvl, err := log.ParseLevel(logLevel)
	if err != nil {
		return err
	}
	log.SetLevel(lgLvl)
	log.SetFormatter(&log.TextFormatter{
		PadLevelText:     true,
		QuoteEmptyFields: true,
	})
	if lgLvl == log.DebugLevel {
		log.SetReportCaller(true)
	}
	return nil
}

func getBridgeController(bridgeName, bridgeType, ovsCliDir string, ovsDbPath string) (types.BridgeController, types.BridgeType) {
	switch bridgeType {
	case "ovs":
		return ipuplugin.NewOvsBridgeController(bridgeName, ovsCliDir, ovsDbPath), types.OvsBridge
	case "linux":
		return ipuplugin.NewLinuxBridgeController(bridgeName), types.LinuxBridge
	default:
		return ipuplugin.NewLinuxBridgeController(bridgeName), types.LinuxBridge
	}
}

func getMachineArchitecture() (string, error) {
	cmd := exec.Command("uname", "-m")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func getPluginMode() string {
	arch, err := getMachineArchitecture()
	if err != nil {
		return "error getting architecture: " + err.Error()
	}

	switch arch {
	case "x86_64":
		return types.HostMode
	case "aarch64":
		return types.IpuMode
	default:
		return "unsupported architecture: " + arch
	}
}

func convertNameToIpAndPort(p4rtName string) (string, error) {

	p4rtIp := "127.0.0.1"
	ip, err := net.LookupIP(p4rtName)
	if err != nil {
		log.Errorf("Couldn't resolve Name %s to IP: err->%s", p4rtName, err)
	} else {
		p4rtIp = ip[0].String()
	}

	log.Infof("Setting p4runtime Ip to %s", p4rtIp)
	return p4rtIp + ":" + p4rtPort, err
}

func getP4Client(p4pkg string, p4rtbin string, p4rtIpPort string, portMuxVsi int, p4BridgeName string, brType types.BridgeType) types.P4RTClient {
	switch p4pkg {
	case "linux":
		return p4rtclient.NewP4RtClient(p4rtbin, p4rtIpPort, portMuxVsi, p4BridgeName, brType)
	case "redhat":
		return p4rtclient.NewRHP4Client(p4rtbin, p4rtIpPort, portMuxVsi, p4BridgeName, brType)
	default:
		return nil
	}
}
