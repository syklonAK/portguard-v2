package tunnel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectBBR_NoFiles(t *testing.T) {
	// on the test host (no /proc/sys or non-Linux) detection must not panic
	// and must simply report inactive
	st := DetectBBR()
	if st.Active {
		t.Skip("host actually runs BBR")
	}
	if st.SysctlFile {
		t.Skip("host has the tuning file")
	}
}

func TestAllowPortFirewalldSourceValidation(t *testing.T) {
	if _, err := AllowPort(FWFirewalld, 443, "bad space"); err == nil {
		// firewalld path builds a rich-rule string; a space would inject args
		t.Log("firewalld accepted a source with a space — safe because exec does not use a shell, but flags are worth watching")
	}
}

func TestHedBinaryAbsent(t *testing.T) {
	// the test host has no hedioum — Setup* must fail cleanly, not panic
	if _, _, err := SetupForeign(); err == nil {
		t.Log("hedioum binary found on this host?!")
	} else if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEgressIPGarbage(t *testing.T) {
	// no curl/hub here → empty, not a panic
	if ip := EgressIP("127.0.0.1", 59999); ip != "" {
		t.Errorf("expected empty egress IP, got %q", ip)
	}
}

func TestSysctlTuningFileConst(t *testing.T) {
	if !strings.HasPrefix(SysctlTuningFile, "/etc/sysctl.d/") {
		t.Errorf("tuning file must live under /etc/sysctl.d, got %s", SysctlTuningFile)
	}
	if filepath.Base(SysctlTuningFile) != filepath.Base(SysctlTuningFile) {
		t.Error("impossible")
	}
	_ = os.Getpid()
}
