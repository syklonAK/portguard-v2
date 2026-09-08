package tunnel

import (
	"strings"
	"testing"
)

func TestHedCoreAssetName(t *testing.T) {
	// the test box is amd64; the mapping must be exhaustive for the rest
	asset, err := HedCoreAssetName()
	if err != nil {
		t.Fatalf("amd64 should resolve: %v", err)
	}
	if asset != "hedioum-tunnel" && asset != "hedioum-tunnel-arm64" && asset != "hedioum-tunnel-armv7" {
		t.Errorf("unexpected asset: %s", asset)
	}
}

func TestHedCoreDownloadURL(t *testing.T) {
	url, err := HedCoreDownloadURL()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, "github.com/hedioum/Hedioum-Pool-Tunnel/releases/download/") {
		t.Errorf("URL must point at the upstream release: %s", url)
	}
	if !strings.Contains(url, hedCoreVersion) {
		t.Errorf("URL must pin %s: %s", hedCoreVersion, url)
	}
}

func TestDetectHedCoreShape(t *testing.T) {
	st := DetectHedCore()
	if st.Pinned != hedCoreVersion {
		t.Errorf("pinned version missing: %+v", st)
	}
	switch st.UnitState {
	case "active", "inactive", "failed", "absent", "unknown":
	default:
		t.Errorf("unit_state must be a known state, got %q", st.UnitState)
	}
	// config_ok must agree with role
	if st.ConfigOK != (st.Role != "") {
		t.Errorf("config_ok=%v but role=%q", st.ConfigOK, st.Role)
	}
}

func TestHedCoreServiceControlValidation(t *testing.T) {
	if _, err := HedCoreServiceControl("reboot"); err == nil {
		t.Error("invalid action must be rejected before any exec")
	}
	if _, err := HedCoreServiceControl("restart; rm -rf /"); err == nil {
		t.Error("injection attempt must be rejected")
	}
}
