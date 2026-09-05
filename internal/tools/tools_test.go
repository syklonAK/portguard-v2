package tools

import (
	"strings"
	"testing"
)

func TestByVersionToken(t *testing.T) {
	if ByID("xray") == nil {
		t.Error("xray missing from registry")
	}
	if ByID("hedioum") == nil {
		t.Error("hedioum missing from registry")
	}
	if ByID("nope") != nil {
		t.Error("ByID must return nil for unknown id")
	}
	seen := map[string]bool{}
	for _, tool := range Registry {
		if seen[tool.ID] {
			t.Errorf("duplicate tool id: %s", tool.ID)
		}
		seen[tool.ID] = true
		if tool.InstallCmd == "" {
			t.Errorf("tool %s has no install command", tool.ID)
		}
	}
}

func TestLooksLikeVersion(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"1.8.24", true},
		{"v2.11.3", true},
		{"Xray 24.11.11", false},
		{"24.11.11", true},
		{"abc", false},
		{"1", false},
		{"", false},
		{"1.2.3.4.5.6.7.8.9.10.11.12.13.14.15.16.17", false},
	}
	for _, c := range cases {
		if got := looksLikeVersion(c.in); got != c.want {
			t.Errorf("looksLikeVersion(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestFirstVersionToken(t *testing.T) {
	if v := firstVersionToken("Xray 24.11.11 (Custom) 12345678 go1.23\nRuntime go1.23"); v != "24.11.11" {
		t.Errorf("xray version extract = %q", v)
	}
	if v := firstVersionToken("HAProxy version 2.6.12 2023/08"); v != "2.6.12" {
		t.Errorf("haproxy version extract = %q, want 2.6.12", v)
	}
	if v := firstVersionToken("nothing here"); v != "" {
		t.Errorf("expected empty, got %q", v)
	}
}

// DetectAll must never panic even when none of the binaries exist (e.g. dev box).
func TestDetectAllSafe(t *testing.T) {
	states := DetectAll()
	if len(states) != len(Registry) {
		t.Fatalf("DetectAll returned %d states, want %d", len(states), len(Registry))
	}
	for _, s := range states {
		if s.Installed && s.Version != "" && !strings.ContainsAny(s.Version, ".") {
			t.Errorf("suspicious version for %s: %q", s.ID, s.Version)
		}
	}
}
