// Package tunnel: standalone hedioum core management — the panel installs
// the hedioum-tunnel binary itself (no upstream bootstrap script, no TUI)
// and drives its lifecycle as a first-class PortGuard resource.
//
// Install = download the pinned upstream release asset for this arch →
// verify the sha256 published alongside it (when the release provides one)
// → self-install via the binary's own `install` subcommand (self-copy to
// /usr/local/bin, systemd unit render, BBR enable) — fully non-interactive,
// mirroring the upstream install.sh bootstrap minus the interactive tail.
package tunnel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// hedCoreVersion is the upstream release PortGuard pins. The asset names are
// stable across the project (install.sh picks exactly these two).
const hedCoreVersion = "v0.11.0"

// HedCoreBin is where the binary self-installs (upstream install.go).
const HedCoreBin = "/usr/local/bin/hedioum-tunnel"

// HedCoreAssetName maps GOARCH to the upstream release asset.
func HedCoreAssetName() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "hedioum-tunnel", nil
	case "arm64":
		return "hedioum-tunnel-arm64", nil
	case "arm":
		return "hedioum-tunnel-armv7", nil
	}
	return "", fmt.Errorf("unsupported architecture for the hedioum core: %s", runtime.GOARCH)
}

// HedCoreDownloadURL returns the pinned release asset URL for this arch.
func HedCoreDownloadURL() (string, error) {
	asset, err := HedCoreAssetName()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://github.com/hedioum/Hedioum-Pool-Tunnel/releases/download/%s/%s",
		hedCoreVersion, asset), nil
}

// HedCoreStatus is the core-installation snapshot for the UI.
type HedCoreStatus struct {
	Installed  bool   `json:"installed"`
	Version    string `json:"version,omitempty"`
	Binary     string `json:"binary,omitempty"`
	Pinned     string `json:"pinned_version"`
	UnitState  string `json:"unit_state"`           // active | inactive | failed | absent
	UnitExists bool   `json:"unit_exists"`
	Role       string `json:"role,omitempty"`       // live config role (iran/foreign/"")
	ConfigOK   bool   `json:"config_ok"`            // /etc/hedioum/hedioum.json parseable
	PinWarning string `json:"pin_warning,omitempty"` // pinned != installed version
}

// DetectHedCore gathers the core state. Uses the `version` subcommand
// (upstream has no --version flag).
func DetectHedCore() HedCoreStatus {
	st := HedCoreStatus{Pinned: hedCoreVersion, UnitState: "absent"}
	if b := HedBinary(); b != "" {
		st.Installed = true
		st.Binary = b
		st.Version = HedVersion()
		if st.Version != "" && !strings.Contains(st.Version, hedCoreVersion) {
			st.PinWarning = fmt.Sprintf("installed build (%s) differs from the pinned release (%s) — reinstall to align", firstLine(st.Version), hedCoreVersion)
		}
	}
	out, err := exec.Command("systemctl", "is-active", "hedioum").Output()
	if err == nil {
		st.UnitState = strings.TrimSpace(string(out))
		st.UnitExists = true
	} else {
		// is-active exits nonzero for inactive/failed; distinguish "no unit"
		if _, showErr := exec.Command("systemctl", "cat", "hedioum").Output(); showErr == nil {
			st.UnitExists = true
			st.UnitState = strings.TrimSpace(string(out))
			if st.UnitState == "" {
				st.UnitState = "unknown"
			}
		}
	}
	st.Role = HedRole()
	st.ConfigOK = st.Role != ""
	return st
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// InstallHedCore downloads the pinned release binary and runs its own
// `install` subcommand (self-copy → systemd unit → BBR). sha256 optionally
// verifies against <url>.sha256 when the release publishes one; a missing
// checksum file is not fatal, a MISMATCHING one is.
func InstallHedCore(sink func(string)) error {
	url, err := HedCoreDownloadURL()
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "portguard-hedcore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	binPath := tmp + "/hedioum-tunnel"

	say := func(line string) {
		if sink != nil {
			sink(line)
		}
	}

	say("[portguard] downloading " + url)
	if err := httpDownload(url, binPath, 10*time.Minute); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// checksum check: best-effort fetch of the release's .sha256; a real
	// mismatch aborts, an absent checksum file does not.
	sumURL := url + ".sha256"
	sumPath := binPath + ".sha256"
	if err := httpDownload(sumURL, sumPath, 30*time.Second); err == nil {
		if data, rerr := os.ReadFile(sumPath); rerr == nil {
			want := strings.Fields(string(data))
			if len(want) > 0 {
				got, herr := fileSHA256(binPath)
				if herr != nil {
					return fmt.Errorf("checksum compute failed: %w", herr)
				}
				if !strings.EqualFold(strings.TrimSpace(want[0]), got) {
					return fmt.Errorf("sha256 MISMATCH: want %s got %s — download corrupted or tampered, aborting", strings.TrimSpace(want[0]), got)
				}
				say("[portguard] sha256 verified: " + got[:16] + "…")
			}
		}
	} else {
		say("[portguard] no published checksum for this release — skipping hash verification")
	}

	if err := os.Chmod(binPath, 0o755); err != nil {
		return err
	}
	ver, _ := exec.Command(binPath, "version").CombinedOutput()
	say("[portguard] asset version: " + strings.TrimSpace(string(ver)))

	say("[portguard] running the binary's own installer (systemd unit + BBR)…")
	out, err := exec.Command(binPath, "install").CombinedOutput()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			say("[hedioum] " + line)
		}
	}
	if err != nil {
		return fmt.Errorf("core install failed: %s", strings.TrimSpace(string(out)))
	}
	say("[portguard] hedioum core installed → " + HedCoreBin)
	return nil
}

// RemoveHedCore uninstalls the core the way upstream recommends: prefer its
// own non-interactive uninstaller (`uninstall --yes`); when the binary is
// gone but unit files linger, clean those directly.
func RemoveHedCore() ([]string, error) {
	var removed []string
	bin := HedBinary()
	if bin != "" {
		out, err := exec.Command(bin, "uninstall", "--yes").CombinedOutput()
		if err == nil {
			removed = append(removed, "hedioum-tunnel uninstall --yes (binary + unit + config)")
			return removed, nil
		}
		// fall through to manual cleanup with the output as context
		removed = append(removed, "uninstall --yes failed: "+strings.TrimSpace(string(out)))
	}
	// manual best-effort cleanup (naming-guarded)
	for _, unit := range []string{"hedioum.service"} {
		_ = exec.Command("systemctl", "disable", "--now", unit).Run()
		for _, p := range []string{"/etc/systemd/system/" + unit, "/lib/systemd/system/" + unit} {
			if fileExists(p) {
				if err := os.Remove(p); err == nil {
					removed = append(removed, p)
				}
			}
		}
	}
	if fileExists(HedCoreBin) {
		if err := os.Remove(HedCoreBin); err == nil {
			removed = append(removed, HedCoreBin)
		}
	}
	if fileExists("/etc/hedioum") {
		if err := os.RemoveAll("/etc/hedioum"); err == nil {
			removed = append(removed, "/etc/hedioum")
		}
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	return removed, nil
}

// HedCoreServiceControl restarts / tails the daemon (start/stop/restart).
func HedCoreServiceControl(action string) (string, error) {
	switch action {
	case "start", "stop", "restart":
	default:
		return "", fmt.Errorf("action must be start, stop or restart")
	}
	out, err := exec.Command("systemctl", action, "hedioum").CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// --- helpers ---

func httpDownload(url, dst string, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
