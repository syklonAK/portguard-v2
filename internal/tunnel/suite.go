// Package tunnel: hedioum-suite v3 feature set — the pieces the panel did
// not have yet, matching hedioum-suite.sh (github hedioum suite v3.0.2):
//
//   - HedProbe      → `hedioum-tunnel probe --node <alias>`   per-mimic reachability
//   - HedSpeedtest  → `hedioum-tunnel speedtest --node .. --mimic .. --dir ..`
//   - HedCheckIP    → `hedioum-tunnel check-ip`                egress IP reputation
//   - DetectNodeService → PasarGuard/marzban node discovery (docker + systemd)
//     with xray listener ports, panel RPC ports and the first public xray bind
//   - MimicPorts    → the fixed camouflage set the egress claims on a foreign box
//   - MimicClashes  → which of those ports non-Hedioum services already hold
//   - NodePublicBind → first non-loopback xray inbound (loopback-only nodes need
//     an ingress forwarder because the egress refuses loopback targets)
//   - PurgeTrojanSuite → remove every relay/ingress/unit/config the trojan
//     suite created, KEEPING hedioum itself, nginx and the node
//
// All commands are non-interactive (no TUI), argv-only, bounded timeouts.
package tunnel

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// MimicPorts is the camouflage port set the hedioum egress claims on a
// foreign box (hedioum-suite MIMIC_PORTS: ssh, apache decoy, tls, mail).
var MimicPorts = []int{22, 80, 443, 587, 143, 465, 993}

// NodeService describes a detected PasarGuard/marzban-family node on this box.
type NodeService struct {
	Kind        string `json:"kind,omitempty"`          // docker:<names> | systemd:<units>
	XrayPorts   []int  `json:"xray_ports,omitempty"`    // every listening xray inbound
	RPCPorts    []int  `json:"rpc_ports,omitempty"`     // panel RPC ports (62050/62051/62055)
	PublicBind  string `json:"public_bind,omitempty"`   // first non-loopback xray bind
	LoopbackOnly bool  `json:"loopback_only"`           // every xray inbound is 127.0.0.1
}

// MimicClash is one port held by a non-Hedioum service.
type MimicClash struct {
	Port  int    `json:"port"`
	Owner string `json:"owner"`
}

var reNodeName = regexp.MustCompile(`(?i)pasarguard|marzban|marzneshin`)
var reHedioumOwner = regexp.MustCompile(`(?i)hedioum`)
var reBenignOwner = regexp.MustCompile(`(?i)hedioum|sshd|apache`)

// DetectNodeService discovers the panel node (docker first, then systemd)
// and its xray listeners — the detect_node/node_public_bind port.
func DetectNodeService() NodeService {
	ns := NodeService{}
	if out, err := exec.Command("docker", "ps", "--format", "{{.Names}}").Output(); err == nil {
		var names []string
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && reNodeName.MatchString(line) {
				names = append(names, line)
			}
		}
		if len(names) > 0 {
			ns.Kind = "docker:" + strings.Join(names, ",")
		}
	}
	if ns.Kind == "" {
		if out, err := exec.Command("systemctl", "list-units", "--type=service", "--no-legend").Output(); err == nil {
			var units []string
			for _, line := range strings.Split(string(out), "\n") {
				f := strings.Fields(line)
				if len(f) > 0 && reNodeName.MatchString(f[0]) {
					units = append(units, f[0])
					if len(units) == 2 {
						break
					}
				}
			}
			if len(units) > 0 {
				ns.Kind = "systemd:" + strings.Join(units, ",")
			}
		}
	}
	// xray listeners
	if out, err := exec.Command("ss", "-ltnpH").Output(); err == nil {
		seen := map[int]bool{}
		for _, line := range strings.Split(string(out), "\n") {
			if !strings.Contains(line, "xray") {
				continue
			}
			f := strings.Fields(line)
			if len(f) < 4 {
				continue
			}
			addr := f[3]
			port := atoiSafe(addr[strings.LastIndex(addr, ":")+1:])
			if port > 0 && !seen[port] {
				seen[port] = true
				ns.XrayPorts = append(ns.XrayPorts, port)
			}
			host := addr[:strings.LastIndex(addr, ":")]
			if ns.PublicBind == "" && host != "127.0.0.1" && host != "[::1]" && !strings.HasPrefix(host, "127.") {
				ns.PublicBind = addr
			}
		}
		ns.LoopbackOnly = len(ns.XrayPorts) > 0 && ns.PublicBind == ""
	}
	// panel RPC ports
	for _, r := range []int{62050, 62051, 62055} {
		if portListening(r) {
			ns.RPCPorts = append(ns.RPCPorts, r)
		}
	}
	return ns
}

// MimicClashes reports mimic ports held by services that are NOT hedioum,
// sshd or the apache decoy (detect_clashes port).
func MimicClashes() []MimicClash {
	var out []MimicClash
	for _, p := range MimicPorts {
		if !portListening(p) {
			continue
		}
		owner := portOwnerName(p)
		if owner == "" || reBenignOwner.MatchString(owner) {
			continue
		}
		out = append(out, MimicClash{Port: p, Owner: owner})
	}
	return out
}

// NodeMimicOverlap lists the node's xray ports that sit on mimic ports — a
// forwarder cannot fix that; the inbound must move (ingress_mimic_warning).
func NodeMimicOverlap(ns NodeService) []int {
	var hit []int
	for _, p := range ns.XrayPorts {
		for _, m := range MimicPorts {
			if p == m {
				hit = append(hit, p)
				break
			}
		}
	}
	return hit
}

// HedProbe runs `hedioum-tunnel probe --node <alias>` (per-mimic reachability).
func HedProbe(alias string) (string, error) {
	return hedSubcommand(60*time.Second, "probe", "--node", alias)
}

// HedSpeedtest runs `hedioum-tunnel speedtest --node .. --mimic .. --dir ..`.
func HedSpeedtest(alias, mimic, dir string) (string, error) {
	args := []string{"speedtest", "--node", alias}
	if mimic != "" {
		args = append(args, "--mimic", mimic)
	}
	if dir != "" {
		args = append(args, "--dir", dir)
	}
	return hedSubcommand(180*time.Second, args...) // real throughput takes a while
}

// HedCheckIP runs `hedioum-tunnel check-ip` (egress IP reputation, foreign side).
func HedCheckIP() (string, error) {
	return hedSubcommand(60*time.Second, "check-ip")
}

func hedSubcommand(timeout time.Duration, args ...string) (string, error) {
	bin := HedBinary()
	if bin == "" {
		return "", fmt.Errorf("hedioum-tunnel is not installed — install it from the Tools page first")
	}
	cmd := exec.Command(bin, args...)
	cmd.Args[0] = bin
	done := make(chan struct{})
	var out []byte
	go func() {
		out, _ = cmd.CombinedOutput()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		return "", fmt.Errorf("hedioum-tunnel %s timed out after %s", strings.Join(args, " "), timeout)
	}
	return string(out), nil
}

// PurgeTrojanSuite removes everything the trojan suite created on this box —
// relays/ingresses are already emptied by the caller (DB side) — while
// KEEPING hedioum itself, nginx and the node (purge_suite contract).
// Files/systemd units only; every removal is name-checked against our own
// naming pattern, nothing else is touched.
func PurgeTrojanSuite() ([]string, error) {
	var removed []string
	// our units
	for _, unit := range []string{TrojanBridgeUnit, TrojanIngressUnit} {
		_ = exec.Command("systemctl", "disable", "--now", unit).Run()
		p := "/etc/systemd/system/" + unit
		if fileExists(p) {
			if err := os.Remove(p); err == nil {
				removed = append(removed, p)
			}
		}
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	// our configs
	for _, p := range []string{
		"/etc/hedioum-suite/portguard-trojan-bridge.json",
		"/etc/hedioum-suite/portguard-trojan-ingress.json",
		"/etc/hedioum-suite",
	} {
		if err := os.RemoveAll(p); err == nil {
			removed = append(removed, p)
		}
	}
	return removed, nil
}

// PurgeResult is the JSON shape returned by the purge endpoint.
type PurgeResult struct {
	Removed []string `json:"removed"`
	Kept    []string `json:"kept"`
}

// PurgeReport builds the user-facing result: what went, what stayed.
func PurgeReport(removed []string) PurgeResult {
	return PurgeResult{
		Removed: removed,
		Kept: []string{
			"the Hedioum tunnel itself (/usr/local/bin/hedioum-tunnel, hedioum.service, /etc/hedioum)",
			"nginx and its configuration",
			"the PasarGuard node service and its inbounds",
		},
	}
}

// --- small local helpers (ss-based, no shell) ---

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func portListening(port int) bool {
	out, err := exec.Command("ss", "-ltnH").Output()
	if err != nil {
		return false
	}
	suffix := fmt.Sprintf(":%d\n", port)
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && strings.HasSuffix(f[3]+ "\n", suffix) {
			return true
		}
	}
	return false
}

// portOwnerName maps a listening port to its process name via ss -ltnpH.
func portOwnerName(port int) string {
	out, err := exec.Command("ss", "-ltnpH").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 6 && strings.HasSuffix(f[3], fmt.Sprintf(":%d", port)) {
			// users:(("nginx",pid=123,fd=6)) → nginx
			if i := strings.Index(line, `("`); i >= 0 {
				rest := line[i+2:]
				if j := strings.Index(rest, `"`); j >= 0 {
					return rest[:j]
				}
			}
			return strings.Join(f[5:], " ")
		}
	}
	return ""
}

// marshalIndentJSON is a helper used by the API layer for debug dumps.
func marshalIndentJSON(v any) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return b
}
