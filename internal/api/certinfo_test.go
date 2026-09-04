package api

import (
	"strings"
	"testing"
)

// generateSelfSigned is defined in settings.go; it yields an EC cert + matching key.

func TestValidateCertPEMAndKeyMatch(t *testing.T) {
	certPEM, keyPEM, err := generateSelfSigned([]string{"example.com"}, 365)
	if err != nil {
		t.Fatal(err)
	}
	res := validateCertPEMAndKey(certPEM, keyPEM)
	if overall, _ := res["overall"].(bool); !overall {
		t.Fatalf("overall must be true for a fresh cert: %+v", res)
	}
	km, _ := res["key_match"].(map[string]any)
	if ok, _ := km["ok"].(bool); !ok {
		t.Fatalf("key must match: %+v", km)
	}
	ci, _ := res["cert"].(map[string]any)
	if ci["dns_names"] == nil {
		t.Fatalf("dns_names missing: %+v", ci)
	}
	if days, ok := ci["days_left"].(int); !ok || days < 360 {
		t.Fatalf("days_left broken: %+v", ci)
	}
}

func TestValidateCertPEMKeyMismatch(t *testing.T) {
	certPEM, _, err := generateSelfSigned([]string{"example.com"}, 365)
	if err != nil {
		t.Fatal(err)
	}
	_, otherKey, err := generateSelfSigned([]string{"other.example.com"}, 365)
	if err != nil {
		t.Fatal(err)
	}
	res := validateCertPEMAndKey(certPEM, otherKey)
	km, _ := res["key_match"].(map[string]any)
	if ok, _ := km["ok"].(bool); ok {
		t.Fatalf("mismatched keys must be detected: %+v", res)
	}
	if overall, _ := res["overall"].(bool); overall {
		t.Error("overall must be false on key mismatch")
	}
}

func TestValidateCertPEMGarbage(t *testing.T) {
	res := validateCertPEMAndKey("not a pem", "")
	if overall, _ := res["overall"].(bool); overall {
		t.Error("garbage must not validate")
	}
	if msg, _ := res["error"].(string); !strings.Contains(msg, "CERTIFICATE") {
		t.Errorf("expected clear error, got %v", res["error"])
	}
}
