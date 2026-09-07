package tunnel

import (
	"strings"
	"testing"
)

func TestUnitPort(t *testing.T) {
	if got := unitPort("pingtunnel-iran8443.service"); got != 8443 {
		t.Errorf("unitPort iran8443 = %d, want 8443", got)
	}
	if got := unitPort("pingtunnel-iran80.service"); got != 80 {
		t.Errorf("unitPort iran80 = %d, want 80", got)
	}
	if got := unitPort("pingtunnel-kharej.service"); got != 0 {
		t.Errorf("unitPort kharej = %d, want 0", got)
	}
	if got := unitPort("pingtunnel-iranabc.service"); got != 0 {
		t.Errorf("unitPort iranabc = %d, want 0", got)
	}
}

func TestPingtunnelTarget(t *testing.T) {
	line := `/usr/local/bin/pingtunnel -type client -l :8443 -s 1.2.3.4 -t 127.0.0.1:8443 -tcp 1`
	if got := pingtunnelTarget(line); got != "1.2.3.4" {
		t.Errorf("pingtunnelTarget = %q, want 1.2.3.4", got)
	}
	if got := pingtunnelTarget("/usr/local/bin/pingtunnel -type server"); got != "" {
		t.Errorf("server ExecStart should have no -s, got %q", got)
	}
}

func TestBuildPingTunnelUnitIran(t *testing.T) {
	name, unit, err := BuildPingTunnelUnit(0, 8443, "5.6.7.8", 8443)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "pingtunnel-iran8443.service" {
		t.Errorf("unit name = %q", name)
	}
	for _, want := range []string{
		"-type client",
		"-l :8443",
		"-s 5.6.7.8",
		"-t 127.0.0.1:8443",
		"-tcp 1",
		"Restart=always",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q", want)
		}
	}
	// validation errors
	if _, _, err := BuildPingTunnelUnit(0, 8443, "", 8443); err == nil {
		t.Error("empty foreign IP must fail")
	}
	if _, _, err := BuildPingTunnelUnit(0, 80, "5.6.7.8", 8443); err == nil {
		t.Error("port < 1025 must fail")
	}
	if _, _, err := BuildPingTunnelUnit(0, 70000, "5.6.7.8", 8443); err == nil {
		t.Error("port > 65535 must fail")
	}
}

func TestBuildPingTunnelUnitForeign(t *testing.T) {
	name, unit, err := BuildPingTunnelUnit(1, 0, "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "pingtunnel-kharej.service" {
		t.Errorf("unit name = %q", name)
	}
	if !strings.Contains(unit, "-type server") {
		t.Error("foreign unit must run -type server")
	}
}

func TestDeletePingTunnelUnitRejectsForeignNames(t *testing.T) {
	for _, bad := range []string{"sshd.service", "../etc/passwd", "nginx.service", "pingtunnel"} {
		if err := DeletePingTunnelUnit(bad); err == nil {
			t.Errorf("DeletePingTunnelUnit(%q) must reject non-pingtunnel units", bad)
		}
	}
}

func TestPingTunnelDownloadURL(t *testing.T) {
	url, err := PingTunnelDownloadURL()
	if err != nil {
		t.Skipf("unsupported arch on this host: %v", err)
	}
	if !strings.Contains(url, "esrrhs/pingtunnel/releases/download/2.8/pingtunnel_linux_") {
		t.Errorf("unexpected URL: %s", url)
	}
}
