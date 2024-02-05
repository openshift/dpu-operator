package cnihelper

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
)

// newCNIRequest creates and fills a Request with this CNI Plugin's environment and
// stdin which contains the CNI variables and configuration.
func NewCNIRequest(args *skel.CmdArgs) *cnitypes.Request {
	envMap := make(map[string]string)
	for _, item := range os.Environ() {
		idx := strings.Index(item, "=")
		if idx > 0 {
			envMap[strings.TrimSpace(item[:idx])] = item[idx+1:]
		}
	}
	return &cnitypes.Request{
		Env:    envMap,
		Config: args.StdinData,
	}
}

// parseNetconf parses the cni config to a NetConf data structure.
func parseNetConf(bytes []byte) (*cnitypes.NetConf, error) {
	netconf := &cnitypes.NetConf{}
	err := json.Unmarshal(bytes, &netconf)
	if err != nil {
		return nil, err
	}

	return netconf, nil
}

// ReadCNIConfig unmarshals a CNI JSON config into an NetConf structure.
func ReadCNIConfig(bytes []byte) (*cnitypes.NetConf, error) {
	conf, err := parseNetConf(bytes)
	if err != nil {
		return nil, err
	}
	if conf.RawPrevResult != nil {
		if err := version.ParsePrevResult(&conf.NetConf); err != nil {
			return nil, err
		}
	}
	return conf, nil
}
