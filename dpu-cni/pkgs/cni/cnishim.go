package cni

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnihelper"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnilogging"
	"github.com/openshift/dpu-operator/dpu-cni/pkgs/cnitypes"
)

// Plugin is the data structure to hold the endpoint information and the corresponding
// functions that use it
type Plugin struct {
	socketPath string
}

// NewCNIPlugin creates the internal Plugin object
func NewCNIPlugin() *Plugin {
	return &Plugin{socketPath: cnitypes.ServerSocketPath}
}

// postRequest reads the cni config args and forwards it via an HTTP post request. The response.
// if any, is passed back to this CNI's plugin.
func (p *Plugin) postRequest(args *skel.CmdArgs) (*cnitypes.Response, string, error) {
	cniRequest := cnihelper.NewCNIRequest(args)

	// Read the cni config stdin args to obtain cniVersion
	conf, err := cnihelper.ReadCNIConfig(args.StdinData)
	if err != nil {
		err = fmt.Errorf("invalid stdin args %v", err)
		return nil, conf.CNIVersion, err
	}

	var body []byte
	body, err = p.doCNI("http://dummy/cni", cniRequest)
	if err != nil {
		return nil, conf.CNIVersion, fmt.Errorf("%s: StdinData: %s", err.Error(), string(args.StdinData))
	}

	response := &cnitypes.Response{}
	if len(body) != 0 {
		if err = json.Unmarshal(body, response); err != nil {
			err = fmt.Errorf("failed to unmarshal response '%s': %v", string(body), err)
			return nil, conf.CNIVersion, err
		}
	}
	return response, conf.CNIVersion, nil
}

// doCNI sends a CNI request to the CNI server via JSON + HTTP over a root-owned unix socket,
// and returns the result
func (p *Plugin) doCNI(url string, req interface{}) ([]byte, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal CNI request %v: %v", req, err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			Dial: func(proto, addr string) (net.Conn, error) {
				return net.Dial("unix", p.socketPath)
			},
		},
	}

	resp, err := client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to send CNI request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read CNI result: %v", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("CNI request failed with status %v: '%s'", resp.StatusCode, string(body))
	}

	return body, nil
}

// SetLogging sets global logging parameters.
func SetLogging(stdinData []byte, containerID, netns, ifName string) error {
	n := &cnitypes.NetConf{}
	if err := json.Unmarshal(stdinData, n); err != nil {
		return fmt.Errorf("SetLogging(): failed to load netconf: %v", err)
	}

	cnilogging.Init(n.LogLevel, n.LogFile, containerID, netns, ifName)
	return nil
}

// CmdAdd is the callback for 'add' cni calls from skel
func (p *Plugin) CmdAdd(args *skel.CmdArgs) error {
	if err := SetLogging(args.StdinData, args.ContainerID, args.Netns, args.IfName); err != nil {
		return err
	}

	cnilogging.Info("function called",
		"func", "cmdAdd",
		"args.Path", args.Path, "args.StdinData", string(args.StdinData), "args.Args", args.Args)

	resp, cniVersion, err := p.postRequest(args)
	if err != nil {
		return fmt.Errorf("failed to post request for cmdAdd: %v", err)
	}

	return types.PrintResult(resp.Result, cniVersion)
}

func (p *Plugin) CmdDel(args *skel.CmdArgs) error {
	if err := SetLogging(args.StdinData, args.ContainerID, args.Netns, args.IfName); err != nil {
		return err
	}

	cnilogging.Info("function called",
		"func", "cmdDel",
		"args.Path", args.Path, "args.StdinData", string(args.StdinData), "args.Args", args.Args)

	_, _, err := p.postRequest(args)
	if err != nil {
		return fmt.Errorf("failed to post request for cmdAdd: %v", err)
	}

	return nil
}

func (p *Plugin) CmdCheck(_ *skel.CmdArgs) error {
	return nil
}
