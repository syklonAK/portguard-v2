// Package tunnel: hedioum hub/egress wizards — the PortGuard port of the
// setup-foreign / setup-iran flows. The panel runs the hedioum-tunnel CLI
// non-interactively, captures the pairing token (foreign side) and
// verifies the local SOCKS hub (iran side) the way the bash wizard does.
package tunnel

import (
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// HedBinCandidates is where the hedioum-tunnel binary may live (same set as
// the tools registry's VerifyBins).
var HedBinCandidates = []string{"/usr/local/bin/hedioum-tunnel", "/usr/bin/hedioum-tunnel", "/opt/hedioum/hedioum-tunnel"}

// HedBinary returns the hedioum-tunnel path or "" when absent.
func HedBinary() string {
	for _, c := range HedBinCandidates {
		if fileExists(c) {
			return c
		}
	}
	return ""
}

// reHedToken matches the pairing token printed by setup-foreign.
var reHedToken = regexp.MustCompile(`(?i)[0-9a-f]{32,128}`)

// SetupForeign runs `hedioum-tunnel setup-foreign`, extracts the pairing
// token from its output and enables the hedioum service.
func SetupForeign() (token string, output string, err error) {
	bin := HedBinary()
	if bin == "" {
		return "", "", fmt.Errorf("hedioum-tunnel is not installed — install it from the Tools page first")
	}
	cmd := exec.Command(bin, "setup-foreign")
	out, err := cmd.CombinedOutput()
	output = string(out)
	if m := reHedToken.FindString(output); m != "" {
		token = m
	}
	if err != nil {
		return token, output, fmt.Errorf("setup-foreign failed: %s", strings.TrimSpace(output))
	}
	_ = exec.Command("systemctl", "enable", "hedioum").Run()
	_ = exec.Command("systemctl", "start", "hedioum").Run()
	return token, output, nil
}

// SetupIranConfig is the request for SetupIran.
type SetupIranConfig struct {
	Alias     string
	Token     string
	SocksPort int // default 40001
}

// SetupIran runs `hedioum-tunnel setup-iran --alias .. --token ..` and then
// verifies the SOCKS hub answers on loopback. Returns the detected hub
// endpoint (host:port) on success.
func SetupIran(cfg SetupIranConfig) (output string, err error) {
	bin := HedBinary()
	if bin == "" {
		return "", fmt.Errorf("hedioum-tunnel is not installed — install it from the Tools page first")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return "", fmt.Errorf("pairing token is required")
	}
	if cfg.Alias == "" {
		cfg.Alias = "relay01"
	}
	if cfg.SocksPort < 1 || cfg.SocksPort > 65535 {
		cfg.SocksPort = 40001
	}
	out, err := exec.Command(bin, "setup-iran",
		"--alias", cfg.Alias,
		"--token", strings.TrimSpace(cfg.Token),
		"--socks-port", strconv.Itoa(cfg.SocksPort)).CombinedOutput()
	output = string(out)
	if err != nil {
		return output, fmt.Errorf("setup-iran failed (check the token): %s", strings.TrimSpace(output))
	}
	_ = exec.Command("systemctl", "enable", "hedioum").Run()
	_ = exec.Command("systemctl", "start", "hedioum").Run()
	time.Sleep(2 * time.Second)
	if !HubAlive("127.0.0.1", cfg.SocksPort) {
		return output, fmt.Errorf("setup-iran finished but nothing answers on 127.0.0.1:%d — check `journalctl -u hedioum`", cfg.SocksPort)
	}
	return output, nil
}

// HubAlive TCP-probes a local SOCKS hub.
func HubAlive(host string, port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 3*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// EgressIP returns the tunnel exit IP by requesting api.ipify.org through
// the local SOCKS5 hub. Empty when the tunnel is not passing traffic.
func EgressIP(socksHost string, socksPort int) string {
	if _, err := exec.LookPath("curl"); err != nil {
		return ""
	}
	out, err := exec.Command("curl", "-s", "--max-time", "12",
		"--socks5-hostname", fmt.Sprintf("%s:%d", socksHost, socksPort),
		"https://api.ipify.org").Output()
	if err != nil {
		return ""
	}
	ip := strings.TrimSpace(string(out))
	for _, r := range ip {
		if !(r >= '0' && r <= '9') && r != '.' && r != ':' {
			return ""
		}
	}
	return ip
}
