// Package tunnel manages the Hedioum Pool Tunnel deployment from PortGuard:
// status detection, Iran-side relay bridge (xray dokodemo → SOCKS5 hub) with
// validate/backup/apply/rollback, and systemd unit installation.
package tunnel

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"portguard/internal/store"
)

const (
	// BridgeConfPath is the live xray bridge config managed by PortGuard.
	BridgeConfPath = "/etc/hedioum-suite/portguard-bridge.json"
	// BridgeUnit is the systemd service for the bridge.
	BridgeUnit = "portguard-tunnel-bridge.service"
	// SocksPortBase is the port range reserved for local SOCKS5 hubs.
	bridgePortHint = 21000
)

// Status is the detected tunnel environment snapshot.
type Status struct {
	HedioumInstalled bool   `json:"hedioum_installed"`
	HedioumVersion    string `json:"hedioum_version,omitempty"`
	HedioumActive     string `json:"hedioum_active"`    // active | inactive | absent
	HedioumBinary     string `json:"hedioum_binary,omitempty"`
	XrayInstalled     bool   `json:"xray_installed"`
	XrayVersion       string `json:"xray_version,omitempty"`
	XrayBinary        string `json:"xray_binary,omitempty"`
	XrayPorts         string `json:"xray_ports,omitempty"`  // ports xray is listening on (public+loopback)
	XrayPublicBind    string `json:"xray_public_bind,omitempty"` // first non-loopback listener (host:port)
	XrayLoopbackOnly  bool   `json:"xray_loopback_only"`   // every xray inbound is 127.0.0.1
	BridgeActive      string `json:"bridge_active"`     // active | inactive | absent
	SocksListening    string `json:"socks_listening,omitempty"` // host:port of a local 40xxx SOCKS listener
	Role              string `json:"role"`             // iran | foreign | unknown
}

// Detect gathers the current tunnel environment from the host.
func Detect() Status {
	st := Status{HedioumActive: "absent", BridgeActive: "absent", Role: "unknown"}

	for _, c := range []string{"/usr/local/bin/hedioum-tunnel", "/usr/bin/hedioum-tunnel", "/opt/hedioum/hedioum-tunnel"} {
		if fileExists(c) {
			st.HedioumInstalled = true
			st.HedioumBinary = c
			break
		}
	}
	if out, err := exec.Command("hedioum-tunnel", "--version").Output(); err == nil {
		st.HedioumVersion = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("systemctl", "is-active", "hedioum").Output(); err == nil {
		st.HedioumActive = strings.TrimSpace(string(out))
	}
	for _, c := range []string{"/usr/local/bin/xray", "/usr/bin/xray"} {
		if fileExists(c) {
			st.XrayInstalled = true
			st.XrayBinary = c
			break
		}
	}
	if out, err := exec.Command("xray", "version").Output(); err == nil && st.XrayInstalled {
		if fields := strings.Fields(string(out)); len(fields) > 1 {
			st.XrayVersion = fields[1]
		}
	}
	if out, err := exec.Command("systemctl", "is-active", "portguard-tunnel-bridge").Output(); err == nil {
		st.BridgeActive = strings.TrimSpace(string(out))
	}

	// xray inbound discovery (ss-based): every listening xray port, plus the
	// first publicly-bound one — mirrors hedioum-suite's node_public_bind check.
	if out, err := exec.Command("ss", "-ltnp").CombinedOutput(); err == nil {
		var ports []string
		seen := map[string]bool{}
		for _, line := range strings.Split(string(out), "\n") {
			if !strings.Contains(line, "xray") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}
			// listening socket: LocalAddr:Port is field 3
			bind := fields[3]
			port := bind[strings.LastIndex(bind, ":")+1:]
			if port == "" || seen[port] {
				continue
			}
			seen[port] = true
			ports = append(ports, port)
			host := bind[:strings.LastIndex(bind, ":")]
			if host != "127.0.0.1" && host != "[::1]" && st.XrayPublicBind == "" {
				st.XrayPublicBind = bind
			}
		}
		if len(ports) > 0 {
			st.XrayPorts = strings.Join(ports, " ")
			st.XrayLoopbackOnly = st.XrayPublicBind == ""
		}
	}

	// SOCKS hub on loopback 40000-49999?
	if out, err := exec.Command("ss", "-ltnH").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}
			addr := fields[3]
			if !strings.HasPrefix(addr, "127.0.0.1:") {
				continue
			}
			port, _ := strconv.Atoi(addr[strings.Index(addr, ":")+1:])
			if port >= 40000 && port <= 49999 {
				st.SocksListening = addr
				break
			}
		}
	}

	// role detection: a local SOCKS hub means iran; hedioum on classic ports means foreign
	if st.SocksListening != "" {
		st.Role = "iran"
	} else if st.HedioumActive == "active" {
		st.Role = "foreign"
	}
	return st
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// dokodemo inbound as produced by BuildBridgeConfig.
type inboundCfg struct {
	Tag      string `json:"tag"`
	Listen   string `json:"listen"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Settings struct {
		Address         string `json:"address"`
		Port            int    `json:"port"`
		Network         string `json:"network"`
		FollowRedirect  bool   `json:"followRedirect"`
	} `json:"settings"`
	Sniffing struct {
		Enabled bool `json:"enabled"`
	} `json:"sniffing"`
}

type socksServer struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

type outboundCfg struct {
	Tag      string `json:"tag"`
	Protocol string `json:"protocol"`
	Settings struct {
		Servers []socksServer `json:"servers"`
	} `json:"settings"`
	StreamSettings *struct {
		Sockopt struct {
			TCPKeepAliveIdle int  `json:"tcpKeepAliveIdle"`
			TCPNoDelay       bool `json:"tcpNoDelay"`
		} `json:"sockopt"`
	} `json:"streamSettings,omitempty"`
}

type bridgeCfg struct {
	Log       map[string]string `json:"log"`
	Inbounds  []inboundCfg      `json:"inbounds"`
	Outbounds []outboundCfg     `json:"outbounds"`
	Routing   map[string]any    `json:"routing"`
	Policy    map[string]any    `json:"policy"`
}

// BuildBridgeConfig renders the xray dokodemo→SOCKS bridge JSON from the
// relays (mirrors hedioum-suite's build_bridge_config).
func BuildBridgeConfig(relays []store.TunnelRelay, socksHost string, socksPort int) ([]byte, error) {
	sorted := make([]store.TunnelRelay, len(relays))
	copy(sorted, relays)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	cfg := bridgeCfg{
		Log: map[string]string{"loglevel": "warning"},
		Routing: map[string]any{
			"domainStrategy": "AsIs",
			"rules": []map[string]any{
				{"type": "field", "network": "tcp,udp", "outboundTag": "tunnel"},
			},
		},
		Policy: map[string]any{
			"levels": map[string]any{
				"0": map[string]any{"handshake": 8, "connIdle": 300, "uplinkOnly": 2, "downlinkOnly": 5},
			},
			"system": map[string]any{"statsInboundUplink": false, "statsInboundDownlink": false},
		},
	}

	tunnel := outboundCfg{Tag: "tunnel", Protocol: "socks"}
	tunnel.Settings.Servers = []socksServer{{Address: socksHost, Port: socksPort}}
	tunnel.StreamSettings = &struct {
		Sockopt struct {
			TCPKeepAliveIdle int  `json:"tcpKeepAliveIdle"`
			TCPNoDelay       bool `json:"tcpNoDelay"`
		} `json:"sockopt"`
	}{}
	tunnel.StreamSettings.Sockopt.TCPKeepAliveIdle = 30
	tunnel.StreamSettings.Sockopt.TCPNoDelay = true
	direct := outboundCfg{Tag: "direct", Protocol: "freedom"}
	cfg.Outbounds = []outboundCfg{tunnel, direct}

	for _, r := range sorted {
		if !r.Enabled {
			continue
		}
		in := inboundCfg{
			Tag:      "relay-" + r.Name,
			Listen:   "127.0.0.1",
			Port:     r.BridgePort,
			Protocol: "dokodemo-door",
		}
		if r.Mode == "raw" {
			in.Listen = r.ListenIP
			in.Port = r.ListenPort
		}
		in.Settings.Address = r.TargetHost
		in.Settings.Port = r.TargetPort
		in.Settings.Network = "tcp"
		if r.UDP {
			in.Settings.Network = "tcp,udp"
		}
		cfg.Inbounds = append(cfg.Inbounds, in)
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// Validate tests a rendered bridge config with `xray run -test`.
func Validate(cfgBytes []byte, xrayBin string) error {
	if xrayBin == "" {
		xrayBin = "xray"
	}
	tmp, err := os.CreateTemp("", "portguard-bridge-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(cfgBytes); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	cmd := exec.Command(xrayBin, "run", "-test", "-config", tmp.Name())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("xray validation failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// InstallUnit writes the systemd unit for the bridge service (idempotent).
func InstallUnit(xrayBin, confPath string) error {
	if xrayBin == "" {
		xrayBin = "/usr/local/bin/xray"
	}
	unit := fmt.Sprintf(`[Unit]
Description=PortGuard tunnel bridge (xray dokodemo -> Hedioum SOCKS5)
After=network-online.target hedioum.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s run -config %s
Restart=always
RestartSec=3
LimitNOFILE=1000000
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
`, xrayBin, confPath)
	if err := os.MkdirAll("/etc/systemd/system", 0o755); err != nil {
		return err
	}
	if err := os.WriteFile("/etc/systemd/system/"+BridgeUnit, []byte(unit), 0o644); err != nil {
		return err
	}
	return exec.Command("systemctl", "daemon-reload").Run()
}

// BackupBridge copies the current live bridge config (if any) into a
// timestamp-named backup directory — the same layout the Backups page and
// its retention pruning expect.
func BackupBridge(backupsDir string) (string, error) {
	if !fileExists(BridgeConfPath) {
		return "", nil
	}
	dir := filepath.Join(backupsDir, time.Now().UTC().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	data, err := os.ReadFile(BridgeConfPath)
	if err != nil {
		return "", err
	}
	dst := filepath.Join(dir, filepath.Base(BridgeConfPath))
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return "", err
	}
	return dst, nil
}

// Apply writes the bridge config atomically and restarts the service. On
// failure it rolls back to prevBytes (when non-nil) and returns the error.
func Apply(cfgBytes, prevBytes []byte, xrayBin string) error {
	if err := os.MkdirAll(filepath.Dir(BridgeConfPath), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(BridgeConfPath, cfgBytes, 0o600); err != nil {
		return err
	}
	if err := restartBridge(); err != nil {
		// rollback
		if prevBytes != nil {
			_ = os.WriteFile(BridgeConfPath, prevBytes, 0o600)
			_ = restartBridge()
		}
		return err
	}
	return nil
}

func restartBridge() error {
	if out, err := exec.Command("systemctl", "restart", BridgeUnit).CombinedOutput(); err != nil {
		return fmt.Errorf("bridge restart failed: %s", strings.TrimSpace(string(out)))
	}
	time.Sleep(1)
	if out, err := exec.Command("systemctl", "is-active", BridgeUnit).Output(); err == nil && strings.TrimSpace(string(out)) == "active" {
		return nil
	}
	out, _ := exec.Command("journalctl", "-u", BridgeUnit, "-n", "12", "--no-pager").CombinedOutput()
	return fmt.Errorf("bridge is not active after restart:\n%s", strings.TrimSpace(string(out)))
}

// ServiceControl runs a systemctl action on the given unit.
func ServiceControl(unit, action string) (string, error) {
	out, err := exec.Command("systemctl", action, unit).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// ServiceStatus returns the active state of a systemd unit.
func ServiceStatus(unit string) string {
	out, err := exec.Command("systemctl", "is-active", unit).Output()
	if err != nil {
		return "inactive"
	}
	return strings.TrimSpace(string(out))
}
