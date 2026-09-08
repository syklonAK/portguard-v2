// Package tunnel: firewall helpers — the PortGuard port of hedioum-allinone's
// detect_fw/fw_allow. Detects the active firewall (ufw / firewalld /
// nftables+iptables) and opens one TCP port for the world or a source IP.
// Purely additive: it never closes or denies anything.
package tunnel

import (
	"fmt"
	"os/exec"
	"strings"
)

// FirewallKind is the detected firewall tooling.
type FirewallKind string

const (
	FWUfw       FirewallKind = "ufw"
	FWFirewalld FirewallKind = "firewalld"
	FWNftables  FirewallKind = "nftables"
	FWIptables  FirewallKind = "iptables"
	FWNone      FirewallKind = "none"
)

// DetectFirewall reports which firewall manages this host.
func DetectFirewall() FirewallKind {
	if out, err := exec.Command("ufw", "status").Output(); err == nil && strings.Contains(strings.ToLower(string(out)), "active") {
		return FWUfw
	}
	if err := exec.Command("systemctl", "is-active", "--quiet", "firewalld").Run(); err == nil {
		return FWFirewalld
	}
	if _, err := exec.Command("nft", "list", "ruleset").Output(); err == nil {
		return FWNftables // nft present with a ruleset; iptables may be the frontend
	}
	if _, err := exec.Command("iptables", "--version").Output(); err == nil {
		return FWIptables
	}
	return FWNone
}

// AllowPort opens one TCP port (optionally restricted to a source CIDR/IP).
// Returns a human-readable action summary. Best-effort: unknown/absent
// firewalls are a no-op, not an error — the caller decides whether to warn.
func AllowPort(kind FirewallKind, port int, source string) (string, error) {
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("port must be 1-65535")
	}
	if strings.ContainsAny(source, ";/&|$`") {
		return "", fmt.Errorf("invalid source")
	}
	switch kind {
	case FWUfw:
		if source != "" {
			out, err := exec.Command("ufw", "allow", "from", source, "to", "any", "port", fmt.Sprint(port), "proto", "tcp").CombinedOutput()
			return strings.TrimSpace(string(out)), err
		}
		out, err := exec.Command("ufw", "allow", fmt.Sprintf("%d/tcp", port)).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	case FWFirewalld:
		var out []byte
		var err error
		if source != "" {
			rule := fmt.Sprintf("rule family=ipv4 source address=%s port port=%d protocol=tcp accept", source, port)
			out, err = exec.Command("firewall-cmd", "--permanent", "--add-rich-rule="+rule).CombinedOutput()
		} else {
			out, err = exec.Command("firewall-cmd", "--permanent", "--add-port="+fmt.Sprint(port)+"/tcp").CombinedOutput()
		}
		if err == nil {
			_, _ = exec.Command("firewall-cmd", "--reload").CombinedOutput()
		}
		return strings.TrimSpace(string(out)), err
	case FWNftables, FWIptables:
		if _, err := exec.Command("iptables", "--version").Output(); err != nil {
			return "", fmt.Errorf("no usable firewall tool found")
		}
		args := []string{"-C", "INPUT", "-p", "tcp", "--dport", fmt.Sprint(port), "-j", "ACCEPT"}
		if source != "" {
			args = []string{"-C", "INPUT", "-p", "tcp", "-s", source, "--dport", fmt.Sprint(port), "-j", "ACCEPT"}
		}
		if err := exec.Command("iptables", args...).Run(); err == nil {
			return "rule already present", nil
		}
		args[0] = "-I"
		out, err := exec.Command("iptables", args...).CombinedOutput()
		if err != nil {
			return strings.TrimSpace(string(out)), err
		}
		return "iptables rule inserted (not persistent unless saved)", nil
	default:
		return "no active firewall — nothing to open", nil
	}
}
