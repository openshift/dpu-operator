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
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/types"
	pb2 "github.com/openshift/dpu-operator/dpu-api/gen"

	pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
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
	p4RtClient      types.P4RTClient
	mode            string
	daemonHostIp    string
	daemonIpuIp     string
	daemonPort      int
	p4rtbin         string
}

func NewIpuPlugin(port int, brCtlr types.BridgeController, p4rtbin string,
	p4Client types.P4RTClient, servingAddr, servingProto, bridge, intf, p4cpInstall, mode, daemonHostIp, daemonIpuIp string, daemonPort int) types.Runnable {
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
		p4RtClient:      p4Client,
		mode:            mode,
		daemonHostIp:    daemonHostIp,
		daemonIpuIp:     daemonIpuIp,
		daemonPort:      daemonPort,
		p4rtbin:         p4rtbin,
	}
}

func (s *server) Run() error {
	var err error
	signalChannel := make(chan os.Signal, 2)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)

	listen, err := s.getListener()
	if err != nil {
		return fmt.Errorf("unable to run IPU plugin")
	}

	if s.mode == types.IpuMode {
		if err := s.bridgeCtlr.EnsureBridgeExists(); err != nil {
			log.Errorf("error while checking host bridge existance: %v", err)
			//log.Fatalf("error while checking host bridge existance: %v", err)
			//return fmt.Errorf("host bridge error")
		}
		if err := ExecutableHandlerGlobal.SetupAccApfs(); err != nil {
			log.Errorf("error from  SetupAccApfs %v", err)
			return fmt.Errorf("error from  SetupAccApfs %v", err)
		} else {
			log.Info("ipuplugin: setup ACC APFs")
		}
	}
	pb2.RegisterLifeCycleServiceServer(s.grpcSrvr, NewLifeCycleService(s.daemonHostIp, s.daemonIpuIp, s.daemonPort, s.mode, s.p4rtbin, s.bridgeCtlr))
	if s.mode == types.IpuMode {
		pb.RegisterBridgePortServiceServer(s.grpcSrvr, s)
		pb2.RegisterNetworkFunctionServiceServer(s.grpcSrvr, NewNetworkFunctionService(s.Ports, s.bridgeCtlr, s.p4RtClient, s.p4rtbin))
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

func (s *server) Stop() {
	s.log.Info("Stopping IPU plugin")
	if s.mode == types.IpuMode {
		//Note: Deletes bridge created in EnsureBridgeExists in  Run api.
		s.bridgeCtlr.DeleteBridges()
	}
	s.grpcSrvr.GracefulStop()
	if s.listener != nil {
		s.listener.Close()
		_ = s.cleanUp()
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
