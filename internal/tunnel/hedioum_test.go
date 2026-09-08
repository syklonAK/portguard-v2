package tunnel

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// buildV2Token mints a v2 pairing token exactly like upstream pairing.Encode
// (compact JSON + base64 RawURL, no padding).
func buildV2Token(auth, ip, persona string, eps map[string]int) string {
	raw, _ := json.Marshal(HedPairingToken{Version: 2, ExitIP: ip, AuthKey: auth, Persona: persona, Endpoints: eps})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func TestDecodeHedTokenV2(t *testing.T) {
	tok := buildV2Token("d534a027b77e6cf6e246ff5da27b2ba4", "162.217.249.119", "directadmin",
		map[string]int{"ssh": 22, "tls": 443, "https-alt": 8443})
	got, ok, err := DecodeHedToken(tok)
	if err != nil || !ok {
		t.Fatalf("v2 token should decode: ok=%v err=%v", ok, err)
	}
	if got.ExitIP != "162.217.249.119" || got.AuthKey != "d534a027b77e6cf6e246ff5da27b2ba4" || got.Persona != "directadmin" {
		t.Errorf("payload mismatch: %+v", got)
	}
	if got.Endpoints["tls"] != 443 || got.Endpoints["ssh"] != 22 {
		t.Errorf("endpoints mismatch: %v", got.Endpoints)
	}
}

func TestDecodeHedTokenLegacyHex(t *testing.T) {
	tok := "5471b02a72b9e52dbaf95d2d601445e6"
	got, ok, err := DecodeHedToken(tok)
	if err != nil || ok {
		t.Fatalf("legacy hex must decode as (nil,false,nil), got ok=%v err=%v", ok, err)
	}
	if got != nil {
		t.Errorf("legacy token must not produce a payload")
	}
}

func TestDecodeHedTokenGarbage(t *testing.T) {
	if _, _, err := DecodeHedToken("hello world!!!"); err == nil {
		t.Error("garbage must error")
	}
	// wrong version
	bad := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"ip":"1.2.3.4","auth":"d534a027b77e6cf6e246ff5da27b2ba4","eps":{"tls":443}}`))
	if _, _, err := DecodeHedToken(bad); err == nil || !strings.Contains(err.Error(), "version") {
		t.Errorf("wrong version must error mentioning version, got %v", err)
	}
	// missing endpoints
	bad2 := base64.RawURLEncoding.EncodeToString([]byte(`{"v":2,"ip":"1.2.3.4","auth":"d534a027b77e6cf6e246ff5da27b2ba4","eps":{}}`))
	if _, _, err := DecodeHedToken(bad2); err == nil || !strings.Contains(err.Error(), "no endpoints") {
		t.Errorf("no-endpoints must error, got %v", err)
	}
}

func TestExtractHedTokenPrefersV2(t *testing.T) {
	v2 := buildV2Token("d534a027b77e6cf6e246ff5da27b2ba4", "5.6.7.8", "devops", map[string]int{"tls": 443})
	out := "some banner\n━━━ Hub onboarding ━━━\n" + v2 + "\n(Auth Token: deadbeefdeadbeefdeadbeefdeadbeef)\n"
	token, isV2, decoded := ExtractHedToken(out)
	if !isV2 || decoded == nil {
		t.Fatalf("v2 token must win over the raw hex line: %q isV2=%v", token, isV2)
	}
	if decoded.ExitIP != "5.6.7.8" {
		t.Errorf("wrong exit ip: %s", decoded.ExitIP)
	}
	// output with ONLY the legacy token (old upstream versions)
	token, isV2, decoded = ExtractHedToken("Auth Token: deadbeefdeadbeefdeadbeefdeadbeef\n")
	if isV2 || decoded != nil || token != "deadbeefdeadbeefdeadbeefdeadbeef" {
		t.Errorf("legacy extraction broken: %q isV2=%v", token, isV2)
	}
}

func TestHedRole(t *testing.T) {
	// /etc/hedioum/hedioum.json does not exist on this host → empty
	if got := HedRole(); got != "" && got != "iran" && got != "foreign" {
		t.Errorf("HedRole must be one of ''/iran/foreign, got %q", got)
	}
}

func TestSetupForeignRefusedOnIranHub(t *testing.T) {
	// hedioum binary exists on this box AND its config role is iran → the
	// wizard must refuse before touching anything
	if HedBinary() == "" || HedRole() != "iran" {
		t.Skip("box is not a live iran hub — guard covered by unit logic above")
	}
	_, _, _, _, err := SetupForeign(SetupForeignConfig{})
	if err == nil || !strings.Contains(err.Error(), "IRAN hub") {
		t.Fatalf("setup-foreign on an iran hub must refuse, got: %v", err)
	}
}

func TestSetupIranTokenValidation(t *testing.T) {
	// a legacy hex key must produce a precise error BEFORE any command runs
	_, err := SetupIran(SetupIranConfig{Token: "5471b02a72b9e52dbaf95d2d601445e6"})
	if err == nil || !strings.Contains(err.Error(), "legacy raw 32-hex") {
		t.Fatalf("legacy key must be rejected with guidance, got: %v", err)
	}
	// garbage must be rejected too
	_, err = SetupIran(SetupIranConfig{Token: "not-a-token"})
	if err == nil {
		t.Fatal("garbage token must be rejected")
	}
}
