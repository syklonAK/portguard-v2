package ops

import (
	"strings"
	"testing"
	"time"
)

func TestUnifiedDiffNoChange(t *testing.T) {
	if d := UnifiedDiff("a\nb\n", "a\nb\n", "old", "new"); d != "" {
		t.Errorf("identical texts must produce empty diff, got:\n%s", d)
	}
}

func TestUnifiedDiffAddRemove(t *testing.T) {
	oldText := "server one\nserver two\nglobal\n"
	newText := "server one\nserver three\nglobal\n"
	d := UnifiedDiff(oldText, newText, "current", "new")
	for _, want := range []string{
		"--- current",
		"+++ new",
		"@@ -",
		"-server two",
		"+server three",
		" global",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("diff missing %q:\n%s", want, d)
		}
	}
	if strings.Contains(d, "-server one") || strings.Contains(d, "+server one") {
		t.Errorf("unchanged lines must not be marked:\n%s", d)
	}
}

func TestUnifiedDiffNewFile(t *testing.T) {
	d := UnifiedDiff("", "line1\nline2\n", "live", "new")
	if !strings.Contains(d, "+line1") || !strings.Contains(d, "+line2") {
		t.Errorf("new file diff broken:\n%s", d)
	}
}

func TestUnifiedDiffContextMerging(t *testing.T) {
	// two changes only 2 equal lines apart must merge into one hunk
	oldText := "a\nb\nc\nd\ne\n"
	newText := "A\nb\nc\nD\ne\n"
	d := UnifiedDiff(oldText, newText, "o", "n")
	if strings.Count(d, "@@ -") != 1 {
		t.Errorf("expected a single merged hunk, got:\n%s", d)
	}
}

func TestUnifiedDiffSeparateHunks(t *testing.T) {
	// changes far apart must produce two hunks
	oldText := "a\nb\nc\nd\ne\nf\ng\nX\ny\nz\n"
	newText := "A\nb\nc\nd\ne\nf\ng\nX\ny\nZ\n"
	d := UnifiedDiff(oldText, newText, "o", "n")
	if strings.Count(d, "@@ -") != 2 {
		t.Errorf("expected two hunks, got:\n%s", d)
	}
}

func TestServiceManagerActions(t *testing.T) {
	var calls []string
	fake := func(timeout time.Duration, name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if len(args) >= 1 && args[0] == "reload" {
			return "reload failed", errFake // force fallback
		}
		return "ok", nil
	}
	m := &ServiceManager{Run: fake, Service: "haproxy"}
	if _, err := m.Action("reload"); err != nil {
		t.Errorf("reload fallback failed: %v", err)
	}
	if len(calls) != 2 || !strings.Contains(calls[1], "reload-or-restart") {
		t.Errorf("reload must fall back to reload-or-restart, calls: %v", calls)
	}
	if _, err := m.Action("reboot"); err == nil {
		t.Error("non-whitelisted action must be rejected")
	}
	bad := &ServiceManager{Run: fake, Service: "bad name; rm"}
	if _, err := bad.Action("start"); err == nil {
		t.Error("invalid service name must be rejected")
	}
}

var errFake = errFakeType{}

type errFakeType struct{}

func (errFakeType) Error() string { return "fake failure" }
