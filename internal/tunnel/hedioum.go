// Package tunnel: hedioum hub/egress wizards — the PortGuard port of the
// setup-foreign / setup-iran flows, matching the upstream Hedioum-Pool-Tunnel
// v0.11 CLI (github.com/hedioum/Hedioum-Pool-Tunnel, cmd/hedioum/setup_cmd.go):
//
//	setup-foreign [flags]  → writes the FOREIGN config, restarts the daemon and
//	                         prints a v2 pairing token (base64url JSON {v,ip,
//	                         auth,persona,sni,eps}) — paste-only hub onboarding.
//	setup-iran   [flags]   → alias for add-node: writes the IRAN config with one
//	                         node (a v2 token is self-contained: IP+ports+key).
//
// Safety contract for the panel (the reason this wrapper exists): the two
// commands are MUTUALLY EXCLUSIVE — setup-foreign overwrites /etc/hedioum/
// hedioum.json with role=foreign and restarts the daemon, so running it on a
// live iran hub silently destroys the hub (the "panel made itself foreign"
// bug). PortGuard therefore refuses a setup-foreign on any box that already
// has an iran config, refuses a setup-iran on a foreign box, and only when
// the caller passes force=true (an explicit override in the UI) does it
// proceed. Token extraction handles BOTH the v2 base64url pairing token and
// the legacy 32-hex raw key (upstream prints the raw key in brackets).
package tunnel

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// HedBinCandidates is where the hedioum-tunnel binary may live (same set as
// the tools registry's VerifyBins).
var HedBinCandidates = []string{"/usr/local/bin/hedioum-tunnel", "/usr/bin/hedioum-tunnel", "/opt/hedioum/hedioum-tunnel"}

// HedConfigPath is the single config database hedioum manages
// (upstream config.SaveConfig target).
const HedConfigPath = "/etc/hedioum/hedioum.json"

// HedBinary returns the hedioum-tunnel path or "" when absent.
func HedBinary() string {
	for _, c := range HedBinCandidates {
		if fileExists(c) {
			return c
		}
	}
	return ""
}

// reHedV2Token matches the v2 pairing token: base64url (raw, no padding) of a
// JSON payload — upstream pairing.Encode uses base64.RawURLEncoding, so the
// on-wire alphabet is [A-Za-z0-9_-] and the payload always contains "v":2.
var reHedV2Token = regexp.MustCompile(`\b[A-Za-z0-9_-]{80,}\b`)

// reHedV1Token matches the legacy bare 32-hex auth key (upstream
// pairing.IsLegacyHex) — printed in brackets as "for advanced/legacy setups".
var reHedV1Token = regexp.MustCompile(`(?i)\b[0-9a-f]{32}\b`)

// HedPairingToken mirrors upstream internal/pairing.Token: what a v2 token
// decodes to. Fields are exported so the API can show the operator what the
// foreign advertised (exit IP, persona, ports) before onboarding.
type HedPairingToken struct {
	Version   int            `json:"v"`
	ExitIP    string         `json:"ip"`
	AuthKey   string         `json:"auth"`
	Persona   string         `json:"persona,omitempty"`
	SNI       string         `json:"sni,omitempty"`
	Endpoints map[string]int `json:"eps"`
}

// DecodeHedToken parses a pairing string exactly like upstream
// pairing.Decode: (token, true, nil) for a valid v2 token; (nil, false, nil)
// for a bare v1 32-hex token; (nil, false, err) for anything malformed.
func DecodeHedToken(s string) (*HedPairingToken, bool, error) {
	s = strings.TrimSpace(s)
	if reHedV1Token.MatchString(s) && len(s) == 32 {
		return nil, false, nil
	}
	// RawURL first (upstream), then padded std as a lenient fallback
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		if raw, err = base64.StdEncoding.DecodeString(s); err != nil {
			return nil, false, fmt.Errorf("token is neither a v1 hex token nor a valid v2 pairing token")
		}
	}
	var t HedPairingToken
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, false, fmt.Errorf("invalid v2 pairing token: %w", err)
	}
	if t.Version != 2 {
		return nil, false, fmt.Errorf("unsupported pairing token version %d (want 2)", t.Version)
	}
	if !regexp.MustCompile(`^[A-Fa-f0-9]{32}$`).MatchString(t.AuthKey) {
		return nil, false, fmt.Errorf("pairing token has an invalid auth key")
	}
	if t.ExitIP == "" {
		return nil, false, fmt.Errorf("pairing token has no exit IP")
	}
	if len(t.Endpoints) == 0 {
		return nil, false, fmt.Errorf("pairing token has no endpoints")
	}
	for ty, port := range t.Endpoints {
		if port < 1 || port > 65535 {
			return nil, false, fmt.Errorf("pairing token endpoint %q has invalid port %d", ty, port)
		}
	}
	return &t, true, nil
}

// hedRole reads the live config's role: "iran" | "foreign" | "" (none).
// This is the guard input for both wizards.
func hedRole() string {
	raw, err := readFileString(HedConfigPath)
	if err != nil {
		return ""
	}
	var cfg struct {
		Role string `json:"role"`
	}
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return ""
	}
	return cfg.Role
}

// HedRole reports the live hedioum config role ("" = unconfigured).
func HedRole() string { return hedRole() }

// ExtractHedToken pulls the pairing token out of setup-foreign output: a v2
// token if one is present, else the legacy raw hex key. Returns the token,
// whether it is a self-contained v2 token, and the decoded token (v2 only).
func ExtractHedToken(output string) (token string, isV2 bool, decoded *HedPairingToken) {
	if m := reHedV2Token.FindString(output); m != "" {
		if t, ok, err := DecodeHedToken(m); err == nil && ok {
			return m, true, t
		}
		// base64-looking run that does not decode as v2: skip to the hex scan
		_ = m
	}
	if m := reHedV1Token.FindString(output); m != "" {
		return m, false, nil
	}
	return "", false, nil
}

// SetupForeignConfig carries the operator's choices for the foreign side.
// Empty fields let upstream defaults stand (setup_cmd.go flag defaults).
type SetupForeignConfig struct {
	Persona    string // auto|cpanel|directadmin|devops (default: auto)
	Domain     string // real domain for a Let's Encrypt cert (optional)
	MoveSSH    bool   // relocate OpenSSH to the decoy port
	PublicIP   string // pin the exit IP in the pairing token (optional)
	Token      string // explicit auth token (generated upstream when empty)
	Force      bool   // overwrite an existing hedioum config of a DIFFERENT role
}

// SetupForeign runs `hedioum-tunnel setup-foreign` non-interactively and
// returns the pairing token. Guard: refused when this box already runs an
// iran hub (setup-foreign would destroy it) unless Force is set.
func SetupForeign(cfg SetupForeignConfig) (token string, isV2 bool, decoded *HedPairingToken, output string, err error) {
	bin := HedBinary()
	if bin == "" {
		return "", false, nil, "", fmt.Errorf("hedioum-tunnel is not installed — install it from the Tools page first")
	}
	if role := hedRole(); role == "iran" && !cfg.Force {
		return "", false, nil, "", fmt.Errorf(
			"this server is already configured as the IRAN hub (%s) — running setup-foreign here would DESTROY the hub. "+
				"The foreign setup belongs on the FOREIGN node. Pass force=true only if you really mean it", HedConfigPath)
	}

	args := []string{"setup-foreign"}
	if cfg.Persona != "" {
		args = append(args, "--persona", cfg.Persona)
	}
	if cfg.Domain != "" {
		args = append(args, "--domain", cfg.Domain)
	}
	if cfg.PublicIP != "" {
		args = append(args, "--public-ip", cfg.PublicIP)
	}
	if cfg.Token != "" {
		args = append(args, "--token", cfg.Token)
	}
	if cfg.MoveSSH {
		args = append(args, "--move-ssh")
	}
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	output = string(out)
	token, isV2, decoded = ExtractHedToken(output)
	if err != nil {
		return token, isV2, decoded, output, fmt.Errorf("setup-foreign failed: %s", strings.TrimSpace(output))
	}
	// upstream setup-foreign restarts the daemon itself; enable only
	_ = exec.Command("systemctl", "enable", "hedioum").Run()
	return token, isV2, decoded, output, nil
}

// SetupIranConfig is the request for SetupIran.
type SetupIranConfig struct {
	Alias     string
	Token     string // v2 pairing token (preferred) or legacy 32-hex key
	SocksPort int    // default 40001
	Force     bool   // overwrite an existing hedioum config of a DIFFERENT role
}

// SetupIran runs `hedioum-tunnel setup-iran --alias .. --token ..` and then
// verifies the SOCKS hub answers on loopback. The v2 pairing token is
// validated client-side first so a pasted raw key gives a precise error
// instead of upstream's generic "provide a v2 pairing token". Guard: refused
// on a foreign-configured box unless Force is set.
func SetupIran(cfg SetupIranConfig) (output string, err error) {
	cfg.Token = strings.TrimSpace(cfg.Token)
	if cfg.Token == "" {
		return "", fmt.Errorf("pairing token is required")
	}
	if _, isV2, derr := DecodeHedToken(cfg.Token); derr != nil {
		return "", fmt.Errorf("invalid token: %v — paste the full pairing token printed by setup-foreign on the foreign node", derr)
	} else if !isV2 {
		return "", fmt.Errorf("this looks like a legacy raw 32-hex key, not the v2 pairing token — "+
			"re-run setup-foreign on the foreign node and paste the long base64 token (or decode it with --target-ip manually)")
	}
	bin := HedBinary()
	if bin == "" {
		return "", fmt.Errorf("hedioum-tunnel is not installed — install it from the Tools page first")
	}
	if cfg.Alias == "" {
		cfg.Alias = "relay01"
	}
	if cfg.SocksPort < 1 || cfg.SocksPort > 65535 {
		cfg.SocksPort = 40001
	}
	if role := hedRole(); role == "foreign" && !cfg.Force {
		return "", fmt.Errorf("this server is already configured as the FOREIGN egress (%s) — the iran hub setup belongs on the IRAN node. Pass force=true only if you really mean it", HedConfigPath)
	}
	out, err := exec.Command(bin, "setup-iran",
		"--alias", cfg.Alias,
		"--token", cfg.Token,
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

// HedVersion returns the daemon version via the `version` subcommand
// (upstream cli.go: `hedioum-tunnel version`; there is no --version flag).
func HedVersion() string {
	bin := HedBinary()
	if bin == "" {
		return ""
	}
	out, err := exec.Command(bin, "version").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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

// readFileString is a tiny os.ReadFile wrapper (kept for test seams).
func readFileString(p string) (string, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
