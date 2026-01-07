// Copyright (c) 2025 Intel Corporation.  All Rights Reserved.
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

package firewall

import (
	"bytes"
	"fmt"
	log "github.com/sirupsen/logrus"
	"os"
	"os/exec"
	"strings"
)

// portRule defines a set of ports under a zone and protocol.
type portRule struct {
	Zone  string
	Proto string
	Ports []string
}

// trustedCIDRs are the subnets granted full access in the internal zone.
var trustedCIDRs = []string{
	"169.254.169.0/24",
	"192.168.0.0/24",
}

// portRules lists the ports opened per zone in configure mode.
var portRules = []portRule{
	{Zone: "internal", Proto: "tcp", Ports: []string{"9559"}},
}

// run executes a command and logs detailed errors on failure.
func run(cmd string, args ...string) error {
	full := append([]string{cmd}, args...)
	log.Infof("executing command %s", strings.Join(full, " "))
	c := exec.Command(cmd, args...)
	var buf bytes.Buffer
	c.Stdout, c.Stderr = &buf, &buf
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s failed: %w\n%s", strings.Join(full, " "), err, buf.String())
	}
	return nil
}

// runSilent executes a command without logging output.
func runSilent(cmd string, args ...string) error {
	return exec.Command(cmd, args...).Run()
}

// ensureService starts and enables a systemd service if not already running.
func ensureService(name string) error {
	if err := runSilent("systemctl", "is-active", "--quiet", name); err != nil {
		log.Infof("starting service %s", name)
		if err := run("systemctl", "start", name); err != nil {
			return fmt.Errorf("start %s: %w", name, err)
		}
	}
	if err := runSilent("systemctl", "enable", name); err != nil {
		return fmt.Errorf("enable %s: %w", name, err)
	}
	return nil
}

// applyRules adds or removes firewall rules based on mode ("configure" or "cleanup").
func applyInternalRules(mode string) error {
	// set default zone
	zoneMap := map[string]string{"configure": "drop", "cleanup": "public"}
	zone, ok := zoneMap[mode]
	if !ok {
		return fmt.Errorf("invalid mode %s", mode)
	}
	if err := run("firewall-cmd", "--set-default-zone="+zone); err != nil {
		return fmt.Errorf("set default zone: %w", err)
	}

	// create or delete internal zone
	if mode == "configure" {
		_ = run("firewall-cmd", "--permanent", "--new-zone=internal")
	} else {
		_ = run("firewall-cmd", "--permanent", "--delete-zone=internal")
	}

	// prepare --permanent batch args
	args := []string{"--permanent"}

	// allow ingress (source) and egress (destination) for trusted CIDRs
	for _, cidr := range trustedCIDRs {
		// ingress rule via add/remove source
		opSrc := "--add-source=" + cidr
		// egress rule via rich rule
		rule := fmt.Sprintf("rule family=\"ipv4\" destination address=\"%s\" accept", cidr)
		opDst := fmt.Sprintf("--add-rich-rule=%s", rule)
		// cleanup adjustments
		if mode == "cleanup" {
			opSrc = "--remove-source=" + cidr
			// remove rich-rule syntax
			rule = fmt.Sprintf("rule family=\"ipv4\" destination address=\"%s\" accept", cidr)
			opDst = fmt.Sprintf("--remove-rich-rule=%s", rule)
		}
		args = append(args, "--zone=internal", opSrc, "--zone=internal", opDst)
	}

	// handle ports per rule
	for _, rule := range portRules {
		for _, p := range rule.Ports {
			op := fmt.Sprintf("--add-port=%s/%s", p, rule.Proto)
			if mode == "cleanup" {
				op = fmt.Sprintf("--remove-port=%s/%s", p, rule.Proto)
			}
			args = append(args, fmt.Sprintf("--zone=%s", rule.Zone), op)
		}
	}

	// execute single batch command
	cmd := append([]string{"firewall-cmd"}, args...)
	return run(cmd[0], cmd[1:]...)
}

// Configure applies a hardened firewall configuration and reloads.
func Configure() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("must run as root")
	}
	if _, err := exec.LookPath("firewall-cmd"); err != nil {
		return fmt.Errorf("firewalld not installed: %w", err)
	}

	if err := ensureService("firewalld"); err != nil {
		return err
	}

	if err := applyInternalRules("configure"); err != nil {
		return err
	}
	if err := run("firewall-cmd", "--reload"); err != nil {
		return fmt.Errorf("reload: %w", err)
	}

	log.Info("firewalld hardened successfully")
	return nil
}

// CleanUp removes the hardened firewall configuration while preserving SSH connectivity.
func CleanUp() error {
	if err := ensureService("firewalld"); err != nil {
		return err
	}

	if err := applyInternalRules("cleanup"); err != nil {
		return err
	}
	if err := run("firewall-cmd", "--reload"); err != nil {
		return fmt.Errorf("reload after cleanup: %w", err)
	}

	log.Info("firewalld cleanup complete")
	return nil
}
