package DebugDP

import (
	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
)

type DebugDP struct {
	log logr.Logger
}

func NewDebugDP() *DebugDP {
	return &DebugDP{
		log: ctrl.Log.WithName("MarvellVSP:DebugDP"),
	}
}

func (debugDP *DebugDP) AddPortToDataPlane(bridgeName string, portName string, vfPCIAddres string, isDPDK bool) error {
	debugDP.log.Info("AddPortToBridge ", "bridgeName", bridgeName, "PortName", portName)
	return nil
}

func (debugDP *DebugDP) DeletePortFromDataPlane(bridgeName string, portName string) error {
	debugDP.log.Info("DeletePortFromBridge ", "bridgeName", bridgeName, "PortName", portName)
	return nil

}

func (debugDP *DebugDP) InitDataPlane(bridgeName string) error {
	debugDP.log.Info("Init Data plane", "bridgeName", bridgeName)
	return nil
}

func (debugDP *DebugDP) ReadAllPortFromDataPlane(bridgeName string) (string, error) {
	debugDP.log.Info("ReadAllPortFromBridge ", "bridgeName", bridgeName)
	return "", nil
}
func (debugDP *DebugDP) DeleteDataplane(bridgeName string) error {
	debugDP.log.Info("DeleteDataplane", "bridgeName", bridgeName)
	return nil
}

func (debugDP *DebugDP) AddFlowRuleToDataPlane(bridgeName string, inpPort string, outPort string, dstMac string) error {
	debugDP.log.Info("AddNfRuleToDataPlane", "bridgeName", bridgeName, "inpPort", inpPort, "outPort", outPort)
	return nil
}
func (debugDP *DebugDP) DeleteFlowRuleFromDataPlane(bridgeName string, inPort string, outPort string, dstMac string) error {
	debugDP.log.Info("DeleteNfRuleFromDataPlane", "bridgeName", bridgeName, "inPort", inPort)
	return nil
}
