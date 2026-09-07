// Package tunnel: ICMP (PingTunnel) support — status detection, core install,
// and systemd unit management for both tunnel sides.
//
// Iran side (client): pingtunnel -type client -l :P -s FOREIGN_IP
//
//	-t 127.0.0.1:P -tcp 1  — listens on P and forwards to itself over ICMP
//
// Foreign side (server): pingtunnel -type server
//
// The foreign side's pingtunnel then forwards to 127.0.0.1:P where the real
// service (e.g. the xray inbound) listens — so the Iran-side -t target should
// be the foreign node's inbound port.
package tunnel

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

const (
	// PingTunnelBin is the installed core binary path.
	PingTunnelBin = "/usr/local/bin/pingtunnel"
	// PingTunnelDir holds the extracted core.
	PingTunnelDir = "/root/pingtunnel-core"
	// pingtunnelVersion is the upstream release PortGuard pins.
	pingtunnelVersion = "2.8"
)

// PingTunnelStatus is the ICMP tunnel environment snapshot.
type PingTunnelStatus struct {
	Installed bool              `json:"installed"`
	Version   string            `json:"version,omitempty"`
	Binary    string            `json:"binary,omitempty"`
	Services  []PingTunnelUnit  `json:"services"`
	ICMPIgnored bool            `json:"icmp_echo_ignored"`
}

// PingTunnelUnit describes one pingtunnel systemd unit on this host.
type PingTunnelUnit struct {
	Unit   string `json:"unit"`
	Active string `json:"active"`
	Role   string `json:"role"`   // iran | foreign
	Port   int    `json:"port"`   // listen port (iran side)
	Target string `json:"target"` // foreign server address (iran side)
}

// DetectPingTunnel gathers the ICMP tunnel state.
func DetectPingTunnel() PingTunnelStatus {
	st := PingTunnelStatus{Services: []PingTunnelUnit{}}
	if fileExists(PingTunnelBin) {
		st.Installed = true
		st.Binary = PingTunnelBin
		if out, err := exec.Command(PingTunnelBin, "--version").CombinedOutput(); err == nil {
			st.Version = pingtunnelVersionToken(string(out))
		}
	}
	// icmp_echo_ignore_all: pingtunnel needs the kernel to not answer pings itself
	if data, err := os.ReadFile("/proc/sys/net/ipv4/icmp_echo_ignore_all"); err == nil {
		st.ICMPIgnored = strings.TrimSpace(string(data)) == "1"
	}
	// discover pingtunnel-*.service units
	out, err := exec.Command("systemctl", "list-units", "--type=service", "--all", "--no-legend",
		"pingtunnel-*").CombinedOutput()
	if err != nil {
		return st
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		unit := fields[0]
		active := "inactive"
		if len(fields) >= 4 {
			active = fields[3]
		}
		u := PingTunnelUnit{Unit: unit, Active: active}
		if strings.Contains(unit, "-iran") || strings.Contains(unit, "-client") {
			u.Role = "iran"
			u.Port = unitPort(unit)
			if props, err := exec.Command("systemctl", "show", unit,
				"--property=ExecStart", "--value").Output(); err == nil {
				u.Target = pingtunnelTarget(string(props))
			}
		} else {
			u.Role = "foreign"
		}
		st.Services = append(st.Services, u)
	}
	return st
}

var rePingtunnelTarget = regexp.MustCompile(`-s\s+(\S+)`)

// pingtunnelTarget extracts -s VALUE from an ExecStart= command line.
func pingtunnelTarget(execStart string) string {
	if m := rePingtunnelTarget.FindStringSubmatch(execStart); len(m) > 1 {
		return m[1]
	}
	return ""
}

// unitPort extracts the trailing port number from a unit name like
// pingtunnel-iran8443.service (the role prefix is stripped first).
func unitPort(unit string) int {
	s := strings.TrimSuffix(unit, ".service")
	s = strings.TrimPrefix(s, "pingtunnel-iran")
	if s == unit { // not an iran unit — no port encoded
		return 0
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// pingtunnelArch maps GOARCH/ uname to the upstream release asset name.
func pingtunnelArch() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64", nil
	case "arm":
		return "arm", nil
	case "arm64":
		return "arm64", nil
	case "mips":
		return "mips", nil
	case "mipsle":
		return "mipsle", nil
	case "mips64":
		return "mips64", nil
	case "mips64le":
		return "mips64le", nil
	case "ppc64":
		return "ppc64", nil
	case "ppc64le":
		return "ppc64le", nil
	case "riscv64":
		return "riscv64", nil
	case "s390x":
		return "s390x", nil
	case "386":
		return "386", nil
	case "loong64":
		return "loong64", nil
	}
	return "", fmt.Errorf("unsupported architecture: %s", runtime.GOARCH)
}

// PingTunnelDownloadURL returns the pinned upstream release URL for this arch.
func PingTunnelDownloadURL() (string, error) {
	arch, err := pingtunnelArch()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://github.com/esrrhs/pingtunnel/releases/download/%s/pingtunnel_linux_%s.zip",
		pingtunnelVersion, arch), nil
}

// InstallPingTunnelCore downloads and installs the pingtunnel core
// (download → unzip → /usr/local/bin) and applies the ICMP sysctl.
func InstallPingTunnelCore(unzipPath string) error {
	url, err := PingTunnelDownloadURL()
	if err != nil {
		return err
	}
	if unzipPath == "" {
		unzipPath = "unzip"
	}
	tmp, err := os.MkdirTemp("", "portguard-pingtunnel-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	zipPath := tmp + "/pingtunnel.zip"
	if out, err := exec.Command("curl", "-fsSL", "--connect-timeout", "20", "-m", "300",
		"-o", zipPath, url).CombinedOutput(); err != nil {
		return fmt.Errorf("download failed: %s", strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command(unzipPath, "-o", zipPath, "-d", tmp).CombinedOutput(); err != nil {
		return fmt.Errorf("unzip failed: %s", strings.TrimSpace(string(out)))
	}
	if err := os.MkdirAll(PingTunnelDir, 0o755); err != nil {
		return err
	}
	bin := tmp + "/pingtunnel"
	if !fileExists(bin) {
		// some releases nest the binary one level down — find it
		entries, _ := os.ReadDir(tmp)
		for _, e := range entries {
			if !e.IsDir() && strings.HasPrefix(e.Name(), "pingtunnel") {
				bin = tmp + "/" + e.Name()
				break
			}
		}
	}
	if !fileExists(bin) {
		return fmt.Errorf("archive did not contain a pingtunnel binary")
	}
	if out, err := exec.Command("cp", bin, PingTunnelBin).CombinedOutput(); err != nil {
		return fmt.Errorf("install failed: %s", strings.TrimSpace(string(out)))
	}
	if err := os.Chmod(PingTunnelBin, 0o755); err != nil {
		return err
	}
	// pingtunnel answers ICMP itself — the kernel must stay silent
	_ = exec.Command("sysctl", "-w", "net.ipv4.icmp_echo_ignore_all=1").Run()
	return nil
}

// RemovePingTunnelCore deletes the core directory if no services remain.
func RemovePingTunnelCore() error {
	st := DetectPingTunnel()
	if len(st.Services) > 0 {
		return fmt.Errorf("%d pingtunnel services still installed — delete them first", len(st.Services))
	}
	return os.RemoveAll(PingTunnelDir)
}

// BuildPingTunnelUnit renders the systemd unit for one side.
// Iran (client): forward 0.0.0.0:port to foreignIP:targetPort over ICMP.
// Foreign (server): plain ICMP server.
func BuildPingTunnelUnit(side, port int, foreignIP string, targetPort int) (string, string, error) {
	bin := PingTunnelBin
	var name, exec string
	switch side {
	case 0: // iran / client
		if foreignIP == "" {
			return "", "", fmt.Errorf("foreign server IP is required")
		}
		if port < 1025 || port > 65535 {
			return "", "", fmt.Errorf("tunnel port must be 1025-65535")
		}
		if targetPort < 1 || targetPort > 65535 {
			return "", "", fmt.Errorf("target port must be 1-65535")
		}
		name = fmt.Sprintf("pingtunnel-iran%d.service", port)
		exec = fmt.Sprintf("%s -type client -l :%d -s %s -t 127.0.0.1:%d -tcp 1",
			bin, port, foreignIP, targetPort)
	case 1: // foreign / server
		name = "pingtunnel-kharej.service"
		exec = fmt.Sprintf("%s -type server", bin)
	default:
		return "", "", fmt.Errorf("unknown side")
	}
	unit := fmt.Sprintf(`[Unit]
Description=PortGuard ICMP tunnel (%s)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s
Restart=always
RestartSec=3
LimitNOFILE=1000000
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
`, strings.TrimSuffix(name, ".service"), exec)
	return name, unit, nil
}

// InstallPingTunnelUnit writes the unit file and starts it (idempotent).
func InstallPingTunnelUnit(name, unit string) error {
	if err := os.WriteFile("/etc/systemd/system/"+name, []byte(unit), 0o644); err != nil {
		return err
	}
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return err
	}
	return exec.Command("systemctl", "enable", "--now", name).Run()
}

// DeletePingTunnelUnit stops, disables and removes one unit file.
func DeletePingTunnelUnit(unit string) error {
	// refuse anything that is not our naming pattern
	base := strings.TrimSuffix(unit, ".service")
	if !strings.HasPrefix(base, "pingtunnel-") || strings.Contains(unit, "/") || strings.Contains(unit, "..") {
		return fmt.Errorf("not a pingtunnel unit: %s", unit)
	}
	_ = exec.Command("systemctl", "disable", "--now", unit).Run()
	if err := os.Remove("/etc/systemd/system/" + unit); err != nil && !os.IsNotExist(err) {
		return err
	}
	return exec.Command("systemctl", "daemon-reload").Run()
}

// pingtunnelVersionToken extracts the first dotted-numeric version token
// ("pingtunnel version 2.8.0" -> 2.8.0). Local copy — the tools package's
// richer parser is not importable without an import cycle.
func pingtunnelVersionToken(s string) string {
	for _, line := range strings.Split(s, "\n") {
		cur := ""
		for _, r := range line {
			if (r >= '0' && r <= '9') || r == '.' {
				cur += string(r)
			} else {
				if len(cur) >= 3 && strings.Count(cur, ".") >= 1 {
					return cur
				}
				cur = ""
			}
		}
		if len(cur) >= 3 && strings.Count(cur, ".") >= 1 {
			return cur
		}
	}
	return ""
}
