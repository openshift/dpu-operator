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

package ipuplugin

import (
	"context"
	"fmt"
	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/firewall"
	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/infrapod"
	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/p4rtclient"
	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/types"
	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/utils"
	pb2 "github.com/openshift/dpu-operator/dpu-api/gen"
	pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	p4_v1 "github.com/p4lang/p4runtime/go/p4/v1"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

const (
	dpuNamespace = "openshift-dpu-operator"
)

type server struct {
	pb.UnimplementedBridgePortServiceServer
	servingAddr     string
	servingPort     int
	servingProto    string
	bridgeName      string
	uplinkInterface string
	grpcSrvr        *grpc.Server
	listener        net.Listener
	log             *log.Entry
	p4cpInstall     string
	Ports           map[string]*types.BridgePortInfo
	bridgeCtlr      types.BridgeController
	p4rtClient      types.P4RTClient
	p4Image         string
	mode            string
	daemonHostIp    string
	daemonIpuIp     string
	daemonPort      int
	infrapodMgr     types.InfrapodMgr
}

func NewIpuPlugin(port int, brCtlr types.BridgeController,
	p4Client types.P4RTClient, p4Image string, servingAddr, servingProto, bridge, intf, p4cpInstall, mode, daemonHostIp, daemonIpuIp string, daemonPort int) types.Runnable {
	return &server{
		servingAddr:     servingAddr,
		servingPort:     port,
		servingProto:    servingProto,
		bridgeName:      bridge,
		uplinkInterface: intf,
		grpcSrvr:        grpc.NewServer(),
		log:             log.WithField("pkg", "ipuplugin"),
		p4cpInstall:     p4cpInstall,
		Ports:           make(map[string]*types.BridgePortInfo),
		bridgeCtlr:      brCtlr,
		p4rtClient:      p4Client,
		p4Image:         p4Image,
		mode:            mode,
		daemonHostIp:    daemonHostIp,
		daemonIpuIp:     daemonIpuIp,
		daemonPort:      daemonPort,
		infrapodMgr:     nil, // Created in Run()
	}
}

func waitForInfraP4d(p4rtClient types.P4RTClient, inCluster bool) (string, error) {
	ctx := context.Background()
	// Higher retries because ipu-plugin itself starts infrapod
	// and if it doesn't wait the minimum, then there is a chance that it
	// will keep restarting and hence restarting infrapod too
	maxRetries := 50
	retryInterval := 2 * time.Second

	var err error
	var count int
	var conn *grpc.ClientConn

	log.Infof("waitForInfraP4d: inCluster: %v", inCluster)
	for count = 0; count < maxRetries; count++ {
		time.Sleep(retryInterval)
		// Infrapod was created successfully. Since the service must have been
		// restarted, the IP would be new. Resolve again and reassign
		err = p4rtClient.ResolveServiceIp(inCluster)
		if err != nil {
			log.Warnf("Error %v while trying to resolve IP", err)
			continue
		}
		conn, err = grpc.Dial(p4rtClient.GetIpPort(), grpc.WithTransportCredentials(insecure.NewCredentials()))
		log.Infof("Connecting to server %s retry count:%d", p4rtClient.GetIpPort(), count)
		if err != nil {
			log.Warnf("Cannot connect to server: %v", err)
			continue
		}
		c := p4_v1.NewP4RuntimeClient(conn)
		req := &p4_v1.GetForwardingPipelineConfigRequest{
			DeviceId:     1,
			ResponseType: p4_v1.GetForwardingPipelineConfigRequest_ResponseType(p4_v1.GetForwardingPipelineConfigRequest_ALL),
		}
		fwdResp, err := c.GetForwardingPipelineConfig(ctx, req)
		if err != nil {
			log.Warnf("error when retrieving forwardingpipeline config: %v", err)
			continue
		}

		config := fwdResp.GetConfig()
		if config == nil {
			// pipeline doesn't have a config yet
			log.Warnf("No forwardingpipeline config yet: %v", err)
			continue
		} else {
			break
		}
	}
	defer conn.Close()
	if count == maxRetries {
		log.Fatalf("Failed to wait for infrap4d. Exiting\n")
		os.Exit(1)
	}
	return "", nil
}

func (s *server) Run() error {
	var err error
	signalChannel := make(chan os.Signal, 2)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)
	inCluster := true

	listen, err := s.getListener()
	if err != nil {
		return fmt.Errorf("unable to run IPU plugin")
	}

	if s.mode == types.IpuMode {
		// Configure ACC firewall settings to allow microshift, dpu-operator, ACC-IMC internal traffics.
		log.Info("Configure firewalld on ACC")
		if err := firewall.Configure(); err != nil {
			log.Error(err, "firewall setup failed: %v", err)
		}

		log.Info("Starting infrapod")
		if s.p4Image != "" {
			log.Infof("Using P4 image as : %s\n", s.p4Image)
			s.infrapodMgr, err = infrapod.NewInfrapodMgr(s.p4Image, dpuNamespace)
			if err != nil {
				log.Error(err, "unable to create InfrapodMgr : %v", err)
				return err
			}
			go func() {
				if err = s.infrapodMgr.StartMgr(); err != nil {
					log.Error(err, "unable to Start mgr : %v", err)
					time.Sleep(2 * time.Second)
					// Sending Sigterm to the main thread to start cleanup
					syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
				}
			}()
			if err = s.infrapodMgr.DeleteCrs(); err != nil {
				log.Error(err, "unable to Delete Crs : %v", err)
				return err
			}
			if err = s.infrapodMgr.WaitForPodDelete(60 * time.Second); err != nil {
				log.Error(err, "unable to Wait for pod deletion : %v", err)
				return err
			}
			if err = s.infrapodMgr.CreatePvCrs(); err != nil {
				log.Error(err, "unable to Create PV Crs : %v", err)
				return err
			}
			if err = s.infrapodMgr.CreateCrs(); err != nil {
				log.Error(err, "unable to Create Crs : %v", err)
				return err
			}
			if err = s.infrapodMgr.WaitForPodReady(60 * time.Second); err != nil {
				log.Error(err, "unable to Wait for pod creation : %v", err)
				return err
			}
		} else {
			inCluster = false
			log.Infof("Waiting for P4 pod to be started manually\n")
		}
		// Wait for the infrap4d connection to come up
		if _, err := waitForInfraP4d(s.p4rtClient, inCluster); err != nil {
			log.Error(err, "unable to connect to infrap4d, %v; Exiting", err)
			return err
		}
		// Create bridge if it doesn't exist
		if err := s.bridgeCtlr.EnsureBridgeExists(); err != nil {
			log.Infof("error while checking host bridge existence: %v", err)
			log.Fatalf("error while checking host bridge existence: %v", err)
			return fmt.Errorf("host bridge error")
		}
	}
	pb2.RegisterLifeCycleServiceServer(s.grpcSrvr, NewLifeCycleService(s.daemonHostIp, s.daemonIpuIp, s.daemonPort, s.mode, s.p4rtClient, s.bridgeCtlr))
	if s.mode == types.IpuMode {
		pb.RegisterBridgePortServiceServer(s.grpcSrvr, s)
		pb2.RegisterNetworkFunctionServiceServer(s.grpcSrvr, NewNetworkFunctionService(s.Ports, s.bridgeCtlr, s.p4rtClient))
	}
	pb2.RegisterDeviceServiceServer(s.grpcSrvr, NewDevicePluginService(s.mode))

	s.log.WithField("addr", listen.Addr().String()).Info("IPU plugin server listening on at:")
	go func() {
		if err = s.grpcSrvr.Serve(listen); err != nil {
			log.Fatalf("IPU plugin failed to serve: %v", err)
			return
		}
	}()

	// Wait for SIGTERM signal
	<-signalChannel
	s.log.Infof("SIGINT received, exiting")
	s.Stop()
	return nil
}

func cleanUpRulesOnExit(p4rtClient types.P4RTClient) error {
	log.Infof("DeletePhyPortRules, path->%s, 1->%v, 2->%v", p4rtClient.GetBin(), AccApfInfo[PHY_PORT0_SECONDARY_INTF_INDEX].Mac, AccApfInfo[PHY_PORT0_PRIMARY_INTF_INDEX].Mac)
	p4rtclient.DeletePhyPortRules(p4rtClient, AccApfInfo[PHY_PORT0_SECONDARY_INTF_INDEX].Mac, AccApfInfo[PHY_PORT0_PRIMARY_INTF_INDEX].Mac)

	vfMacList, err := utils.GetVfMacList()
	if err != nil {
		log.Errorf("Stop: Error->%v", err)
		return fmt.Errorf("Stop: Error->%v", err)
	}
	if len(vfMacList) == 0 || (len(vfMacList) == 1 && vfMacList[0] == "") {
		log.Errorf("No VFs initialized on the host")
	} else {
		log.Infof("DeletePeerToPeerP4Rules, path->%s, vfMacList->%v", p4rtClient.GetBin(), vfMacList)
		p4rtclient.DeletePeerToPeerP4Rules(p4rtClient, vfMacList)
	}

	log.Infof("DeleteLAGP4Rules, path->%s", p4rtClient.GetBin())
	p4rtclient.DeleteLAGP4Rules(p4rtClient)

	log.Infof("DeleteRHPrimaryNetworkVportP4Rules, path->%s, 1->%v", p4rtClient, AccApfInfo[PHY_PORT0_PRIMARY_INTF_INDEX].Mac)
	p4rtclient.DeleteRHPrimaryNetworkVportP4Rules(p4rtClient, AccApfInfo[PHY_PORT0_PRIMARY_INTF_INDEX].Mac)
	return nil
}

func (s *server) Stop() {
	s.log.Info("Stopping IPU plugin")
	if s.mode == types.IpuMode {
		//Note: Deletes bridge created in EnsureBridgeExists in  Run api.
		s.bridgeCtlr.DeleteBridges()
		// Delete P4 rules on exit
		cleanUpRulesOnExit(s.p4rtClient)
		if err := s.infrapodMgr.DeleteCrs(); err != nil {
			log.Error(err, "unable to Delete Crs : %v", err)
			// Do not return since we continue on error
		}
		//Restore Red Hat primary network path via opcodes - This is required after the primiary network P4 rules are deleted.
		utils.RestoreRHPrimaryNetwork()
	}
	// Stopping the gRPC server for the DPU daemon
	s.grpcSrvr.GracefulStop()
	if s.listener != nil {
		s.listener.Close()
		_ = s.cleanUp()
	}

	if s.mode == types.IpuMode {
		//Reset firewall to its default settings.
		log.Info("Stop firewalld on ACC")
		if err := firewall.CleanUp(); err != nil {
			log.Error(err, "firewall cleanup failed: %v", err)
		}
	}

	s.log.Info("IPU plugin has stopped")
}

func (s *server) getListener() (net.Listener, error) {
	if s.servingProto == "unix" {
		// Do clean up first
		if err := s.cleanUp(); err != nil {
			return nil, err
		}
		socketDir := filepath.Dir(s.servingAddr)
		if err := os.MkdirAll(socketDir, 0600); err != nil {
			log.Fatalf("failed to create plugin socket directory: %v", err)
			return nil, fmt.Errorf("unable to create socket directory")
		}

		listen, err := net.Listen(s.servingProto, s.servingAddr)
		if err != nil {
			log.Fatalf("failed to open unix socket listener: %v", err)
			return nil, fmt.Errorf("unable to open unix socket")
		}
		return listen, nil
	} else if s.servingProto == "tcp" {
		listen, err := net.Listen(s.servingProto, fmt.Sprintf("%s:%d", s.servingAddr, s.servingPort))
		if err != nil {
			log.Fatalf("failed to open TCP socket listener: %v", err)
			return nil, fmt.Errorf("unable to open TCP socket")
		}
		return listen, nil
	}
	return nil, fmt.Errorf("unsupported serving protocol %s", s.servingProto)

}

func (s *server) cleanUp() error {
	if err := os.Remove(s.servingAddr); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
