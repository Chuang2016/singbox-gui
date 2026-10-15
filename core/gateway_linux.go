//go:build linux

package core

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Gateway (transparent proxy) support for Linux.
//
// When enabled, sbgui:
//  1. enables IPv4 forwarding,
//  2. installs iptables mangle rules that redirect all forwarded TCP/UDP
//     traffic into sing-box's tproxy inbound via TPROXY,
//  3. adds the fwmark policy-routing entries TPROXY needs.
//
// LAN clients then only need to set this box as their gateway — no per-device
// proxy settings. Traffic still goes through the normal route rules
// (CN direct / foreign via proxy / ads blocked / DNS hijacked).
//
// Requires root (iptables, ip rule, sysctl). Idempotent.

const (
	gatewayChain     = "SBGUI"
	gatewayMark      = "0x1/0x1"
	gatewayTable     = "100"
	GatewayTproxyPort = 2082
)

func runCmd(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "),
			err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ipt(args ...string) error { return runCmd("iptables", args...) }

// SetupGateway installs the transparent-proxy firewall rules. Safe to call
// repeatedly (e.g. on every sbgui start, since iptables rules don't survive reboot).
func SetupGateway(tproxyPort int) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("gateway mode requires root (iptables / ip rule / sysctl)")
	}
	if _, err := exec.LookPath("iptables"); err != nil {
		return fmt.Errorf("iptables not found")
	}
	if _, err := exec.LookPath("ip"); err != nil {
		return fmt.Errorf("iproute2 (ip command) not found")
	}
	// 1. IPv4 forwarding, persisted
	if err := runCmd("sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		return err
	}
	_ = os.WriteFile("/etc/sysctl.d/99-sbgui.conf",
		[]byte("net.ipv4.ip_forward=1\n"), 0644)

	// 2. dedicated mangle chain
	_ = ipt("-t", "mangle", "-N", gatewayChain) // already exists -> ignore
	_ = ipt("-t", "mangle", "-F", gatewayChain)
	if err := ipt("-t", "mangle", "-C", "PREROUTING", "-j", gatewayChain); err != nil {
		if err := ipt("-t", "mangle", "-I", "PREROUTING", "1", "-j", gatewayChain); err != nil {
			return err
		}
	}
	// skip packets addressed to this box itself (SSH, dashboard, local services)
	_ = ipt("-t", "mangle", "-A", gatewayChain,
		"-p", "tcp", "-m", "addrtype", "--dst-type", "LOCAL", "-j", "RETURN")
	_ = ipt("-t", "mangle", "-A", gatewayChain,
		"-p", "udp", "-m", "addrtype", "--dst-type", "LOCAL", "-j", "RETURN")
	// redirect everything else into sing-box tproxy
	if err := ipt("-t", "mangle", "-A", gatewayChain,
		"-p", "tcp", "-j", "TPROXY",
		"--on-port", fmt.Sprint(tproxyPort), "--tproxy-mark", gatewayMark); err != nil {
		return fmt.Errorf("TPROXY not supported by this kernel/iptables: %v", err)
	}
	if err := ipt("-t", "mangle", "-A", gatewayChain,
		"-p", "udp", "-j", "TPROXY",
		"--on-port", fmt.Sprint(tproxyPort), "--tproxy-mark", gatewayMark); err != nil {
		return err
	}
	// 3. policy routing so marked packets reach the local tproxy socket
	_ = runCmd("ip", "rule", "add", "fwmark", gatewayMark, "table", gatewayTable)
	_ = runCmd("ip", "route", "add", "local", "0.0.0.0/0", "dev", "lo", "table", gatewayTable)
	return nil
}

// TeardownGateway removes the rules installed by SetupGateway.
func TeardownGateway() error {
	if os.Geteuid() != 0 {
		return nil
	}
	_ = ipt("-t", "mangle", "-D", "PREROUTING", "-j", gatewayChain)
	_ = ipt("-t", "mangle", "-F", gatewayChain)
	_ = ipt("-t", "mangle", "-X", gatewayChain)
	_ = runCmd("ip", "rule", "del", "fwmark", gatewayMark, "table", gatewayTable)
	_ = runCmd("ip", "route", "del", "local", "0.0.0.0/0", "dev", "lo", "table", gatewayTable)
	return nil
}

// GatewayActive reports whether our mangle chain exists.
func GatewayActive() bool {
	out, err := exec.Command("iptables", "-t", "mangle", "-L", gatewayChain, "-n").CombinedOutput()
	return err == nil && !strings.Contains(string(out), "No chain/target/match")
}
