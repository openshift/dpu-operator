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

	"github.com/intel/ipu-opi-plugins/ipu-plugin/pkg/types"
)

// nolint
type mockP4rtClient struct {
}

// nolint
func (p *mockP4rtClient) AddRules(macAddr []byte, vlan int) {
}

// nolint
func (p *mockP4rtClient) DeleteRules(macAddr []byte, vlan int) {
}

// nolint
func (p *mockP4rtClient) ProgramFXPP4Rules(ruleSets []types.FxpRuleBuilder) error {
	return nil
}

// nolint
func (p *mockP4rtClient) GetBin() string {
	return "p4rt-ctl"
}

// nolint
func (p *mockP4rtClient) GetIpPort() string {
	return "0.0.0.0:9559"
}

type mockBrCtlr struct {
	fnCalled  string
	args      []interface{}
	retValues []interface{}
}

func (brCtlr *mockBrCtlr) On(fnName string, params ...interface{}) *mockBrCtlr {
	brCtlr.fnCalled = fnName
	brCtlr.args = make([]interface{}, len(params))
	brCtlr.args = append(brCtlr.args, params...)
	return brCtlr
}

func (brCtlr *mockBrCtlr) Return(retVals ...interface{}) *mockBrCtlr {
	brCtlr.args = make([]interface{}, len(retVals))
	brCtlr.args = append(brCtlr.args, retVals...)
	return brCtlr
}

func (brCtlr *mockBrCtlr) EnsureBridgeExists() error {
	if brCtlr.fnCalled == "EnsureBridgeExists" {
		return brCtlr.retValues[0].(error)
	}
	// return no error by default
	return nil
}

func (brCtlr *mockBrCtlr) AddPort(portName string) error {
	if brCtlr.fnCalled == "AddPort" {
		return brCtlr.retValues[0].(error)
	}
	// return no error by default
	return nil
}

func (brCtlr *mockBrCtlr) DeletePort(portName string) error {
	if brCtlr.fnCalled == "DeletePort" {
		return brCtlr.retValues[0].(error)
	}
	return fmt.Errorf("invalid mock function called")
}
func (brCtlr *mockBrCtlr) DeleteBridges() error {
	if brCtlr.fnCalled == "DeleteBridges" {
		return brCtlr.retValues[0].(error)
	}
	return fmt.Errorf("invalid mock function called")
}
