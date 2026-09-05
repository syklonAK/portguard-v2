// Package tools manages the software PortGuard (and its tunnels) depend on:
// detection of installed binaries + versions, and one-click installation via
// the official installers, runnable from the panel or remotely by a master.
package tools

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Tool describes one manageable software package.
type Tool struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"` // proxy | tunnel | security | infra
	InstallCmd  string `json:"-"`        // shell command run as root
	VerifyBins  []string `json:"-"`       // binaries that must exist afterwards
	DocsURL     string `json:"docs_url"`
}

// State is the per-tool detection result.
type State struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Binary    string `json:"binary,omitempty"`
	Category  string `json:"category"`
}

// Registry is the catalogue of tools PortGuard can install/detect.
var Registry = []Tool{
	{
		ID:   "xray",
		Name: "Xray-core",
		Description: "Proxy platform used by PasarGuard nodes and the PortGuard tunnel bridge (dokodemo→SOCKS5). " +
			"Installs to /usr/local/bin/xray via the official XTLS installer.",
		Category:   "proxy",
		InstallCmd: `bash -c "$(curl -fsSL https://github.com/XTLS/Xray-install/raw/main/install-release.sh)" @ install`,
		VerifyBins: []string{"/usr/local/bin/xray", "/usr/bin/xray"},
		DocsURL:    "https://xtls.github.io/en/config/",
	},
	{
		ID:   "hedioum",
		Name: "Hedioum Pool Tunnel",
		Description: "The pool tunnel itself (egress + hub sides). Installs hedioum-tunnel via the official bootstrap; " +
			"then run its setup-foreign / setup-iran flows (see the Tunnels page).",
		Category:   "tunnel",
		InstallCmd: `bash <(curl -fsSL https://raw.githubusercontent.com/hedioum/Hedioum-Pool-Tunnel/main/install.sh)`,
		VerifyBins: []string{"/usr/local/bin/hedioum-tunnel", "/usr/bin/hedioum-tunnel", "/opt/hedioum/hedioum-tunnel"},
		DocsURL:    "https://github.com/hedioum/Hedioum-Pool-Tunnel",
	},
	{
		ID:   "certbot",
		Name: "Certbot (Let's Encrypt)",
		Description: "Issues and renews real TLS certificates via the ACME protocol. Used for HTTPS mappings on " +
			"domains with public DNS.",
		Category:  "security",
		InstallCmd: `DEBIAN_FRONTEND=noninteractive apt-get update -y -qq && ` +
			`DEBIAN_FRONTEND=noninteractive apt-get install -y -qq certbot`,
		VerifyBins: []string{"/usr/bin/certbot"},
		DocsURL:    "https://certbot.eff.org/",
	},
	{
		ID:   "wireguard",
		Name: "WireGuard",
		Description: "Kernel VPN backend used by PasarGuard wireguard nodes (apt: wireguard tools + dkms module).",
		Category:  "infra",
		InstallCmd: `DEBIAN_FRONTEND=noninteractive apt-get update -y -qq && ` +
			`DEBIAN_FRONTEND=noninteractive apt-get install -y -qq wireguard wireguard-tools`,
		VerifyBins: []string{"/usr/bin/wg"},
		DocsURL:    "https://www.wireguard.com/",
	},
	{
		ID:   "nginx",
		Name: "nginx (with stream module)",
		Description: "Web/proxy engine with TCP/UDP stream forwarding. Usually already installed by PortGuard's installer.",
		Category:  "infra",
		InstallCmd: `DEBIAN_FRONTEND=noninteractive apt-get update -y -qq && ` +
			`DEBIAN_FRONTEND=noninteractive apt-get install -y -qq nginx libnginx-mod-stream`,
		VerifyBins: []string{"/usr/sbin/nginx"},
		DocsURL:    "https://nginx.org/",
	},
	{
		ID:   "haproxy",
		Name: "HAProxy",
		Description: "TCP/HTTP load balancer and the second PortGuard engine. Usually already installed by PortGuard's installer.",
		Category:  "infra",
		InstallCmd: `DEBIAN_FRONTEND=noninteractive apt-get update -y -qq && ` +
			`DEBIAN_FRONTEND=noninteractive apt-get install -y -qq haproxy`,
		VerifyBins: []string{"/usr/sbin/haproxy"},
		DocsURL:    "https://www.haproxy.org/",
	},
}

// ByID finds a tool by its identifier.
func ByID(id string) *Tool {
	for i := range Registry {
		if Registry[i].ID == id {
			return &Registry[i]
		}
	}
	return nil
}

// DetectAll probes every registered tool.
func DetectAll() []State {
	out := make([]State, 0, len(Registry))
	for _, t := range Registry {
		st := State{ID: t.ID, Name: t.Name, Category: t.Category}
		for _, bin := range t.VerifyBins {
			if path, ver := probe(bin); path != "" {
				st.Installed = true
				st.Binary = path
				st.Version = ver
				break
			}
		}
		// also try PATH lookup (e.g. xray installed elsewhere)
		if !st.Installed {
			if path, err := exec.LookPath(t.ID); err == nil {
				if path, ver := probe(path); path != "" {
					st.Installed = true
					st.Binary = path
					st.Version = ver
				}
			}
		}
		out = append(out, st)
	}
	return out
}

// probe returns the binary path and a short version string for one candidate.
func probe(bin string) (string, string) {
	cmd := exec.Command(bin, "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// xray uses "xray version"; haproxy uses -v; try common variants lazily
		for _, args := range [][]string{{"-v"}, {"--version"}} {
			cmd = exec.Command(bin, args...)
			if out, err = cmd.CombinedOutput(); err == nil {
				break
			}
		}
	}
	if err != nil && len(out) == 0 {
		// binary exists but refuses to talk — still counts as installed
		if _, serr := exec.Command(bin, "--help").Output(); serr == nil || strings.Contains(serr.Error(), "exit status") {
			// best effort: keep as installed without version
			return bin, ""
		}
		return "", ""
	}
	return bin, firstVersionToken(string(out))
}

// firstVersionToken extracts the first x.y.z-looking token from installer output.
func firstVersionToken(s string) string {
	for _, line := range strings.Split(s, "\n") {
		fields := strings.Fields(line)
		for _, f := range fields {
			if looksLikeVersion(f) {
				return strings.TrimPrefix(f, "v")
			}
		}
	}
	return ""
}

func looksLikeVersion(s string) bool {
	// optional leading v (stripped by the caller when reporting)
	if strings.HasPrefix(s, "v") {
		s = s[1:]
	}
	if len(s) < 3 || len(s) > 20 {
		return false
	}
	dots := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r == '.':
			dots++
		default:
			return false
		}
	}
	return dots >= 1
}

// InstallResult reports the outcome of one install run.
type InstallResult struct {
	ToolID  string `json:"tool_id"`
	OK      bool   `json:"ok"`
	Output  string `json:"output"` // tail of the installer output
	Elapsed string `json:"elapsed"`
}

// installMu serializes installs: apt/dpkg and the xray installer both use
// exclusive locks, and two concurrent installs (local page + remote master
// trigger) would randomly fail on lock contention.
var installMu sync.Mutex

// Install runs the tool's official installer and returns combined output.
// The command is executed as the panel user (root) with a generous timeout.
func Install(id string) (*InstallResult, error) {
	t := ByID(id)
	if t == nil {
		return nil, fmt.Errorf("unknown tool: %s", id)
	}
	installMu.Lock()
	defer installMu.Unlock()

	// remember pre-install state so a failed reinstall isn't reported as ok
	preInstalled := toolPresent(t)

	start := time.Now()
	cmd := exec.Command("bash", "-c", t.InstallCmd)
	out, err := cmd.CombinedOutput()
	res := &InstallResult{
		ToolID:  id,
		OK:      err == nil,
		Output:  tail(string(out), 8000),
		Elapsed: time.Since(start).Round(time.Millisecond).String(),
	}
	if err != nil {
		res.Output += fmt.Sprintf("\n[exit error: %v]", err)
	}
	// post-condition: the installer must have actually produced the binary
	// (or it must already have been present when the command succeeded)
	if !res.OK && preInstalled && toolPresent(t) {
		// installer failed but the tool was and still is present: report a
		// clear failure, not a masked success
		res.Output += "\n[installer failed; tool was already present — state unchanged]"
	} else {
		res.OK = toolPresent(t)
	}
	return res, nil
}

// toolPresent checks whether any of the tool's verify binaries is executable.
func toolPresent(t *Tool) bool {
	for _, bin := range t.VerifyBins {
		if exec.Command("test", "-x", bin).Run() == nil {
			return true
		}
	}
	if path, _ := exec.LookPath(t.ID); path != "" {
		return true
	}
	return false
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
