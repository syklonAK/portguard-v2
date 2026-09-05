package proxy

import (
	"strings"
	"testing"

	"portguard/internal/store"
)

// B2 regression: control characters in user strings must be rejected at
// validation time (they could break out of config directives).
func TestValidateMappingRejectsControlChars(t *testing.T) {
	base := store.Mapping{Name: "x", Enabled: true, Engine: "nginx", Protocol: "http",
		ListenIP: "0.0.0.0", ListenPort: 9090, ServerNames: []string{"x.com"},
		Targets: []store.Target{{Host: "127.0.0.1", Port: 80}}}

	if err := ValidateMapping(base, nil, 8080); err != nil {
		t.Fatalf("clean mapping rejected: %v", err)
	}

	inj := base
	inj.Name = "foo\nlocation /x { alias /etc; }"
	if err := ValidateMapping(inj, nil, 8080); err == nil {
		t.Error("newline in name must be rejected (config injection)")
	}
	inj = base
	inj.HostHeader = "evil\r\nroot /;"
	if err := ValidateMapping(inj, nil, 8080); err == nil {
		t.Error("CR in host_header must be rejected")
	}
	inj = base
	inj.ServerNames = []string{"ok.com", "bad\x00name"}
	if err := ValidateMapping(inj, nil, 8080); err == nil {
		t.Error("NUL in server_names must be rejected")
	}
	inj = base
	inj.ExtraHeaders = map[string]string{"X-\nFoo": "bar"}
	if err := ValidateMapping(inj, nil, 8080); err == nil {
		t.Error("newline in header key must be rejected")
	}
	inj = base
	inj.RedirectTo = "https://x\n}"
	if err := ValidateMapping(inj, nil, 8080); err == nil {
		t.Error("newline in redirect_to must be rejected")
	}
}

// B2 render-time defence: comment text must stay on one line — a newline
// would terminate the comment and inject live directives.
func TestEscapeNginxCommentStripsNewlines(t *testing.T) {
	got := escapeNginxComment("foo\nlocation /x { alias /etc; }\r\nbar")
	if strings.ContainsAny(got, "\n\r") {
		t.Errorf("comment still contains line breaks: %q", got)
	}
}

// B7 regression: IPv6 listen addresses must be bracketed in both engines.
func TestNginxListenAddrIPv6(t *testing.T) {
	if got := nginxListenAddr("2001:db8::1", 443); got != "[2001:db8::1]:443" {
		t.Errorf("specific v6 = %q, want [2001:db8::1]:443", got)
	}
	if got := nginxListenAddr("::", 80); got != "[::]:80" {
		t.Errorf("v6 wildcard = %q, want [::]:80", got)
	}
	if got := nginxListenAddr("", 80); got != "[::]:80" {
		t.Errorf("empty = %q, want [::]:80", got)
	}
	if got := nginxListenAddr("10.0.0.5", 8081); got != "10.0.0.5:8081" {
		t.Errorf("v4 = %q, want 10.0.0.5:8081", got)
	}
}

func TestBindIPIPv6(t *testing.T) {
	if got := bindIP("2001:db8::1"); got != "[2001:db8::1]" {
		t.Errorf("haproxy v6 bind = %q, want [2001:db8::1]", got)
	}
}

// B12 regression: wildcard binds must conflict with specific IPs on the
// same port.
func TestListenConflictWildcardOverlap(t *testing.T) {
	a := store.Mapping{ID: 1, Name: "a", Enabled: true, Engine: "nginx", Protocol: "http",
		ListenIP: "0.0.0.0", ListenPort: 8081, ServerNames: []string{"a.com"},
		Targets: []store.Target{{Host: "127.0.0.1", Port: 80}}}
	b := store.Mapping{ID: 2, Name: "b", Enabled: true, Engine: "haproxy", Protocol: "http",
		ListenIP: "10.0.0.5", ListenPort: 8081, ServerNames: []string{"b.com"},
		Targets: []store.Target{{Host: "127.0.0.1", Port: 80}}}
	if err := ValidateMapping(b, []store.Mapping{a}, 8080); err == nil {
		t.Error("wildcard + specific ip on same port must conflict")
	}
	// different ports: fine
	c := b
	c.ListenPort = 8082
	if err := ValidateMapping(c, []store.Mapping{a}, 8080); err != nil {
		t.Errorf("different port must not conflict: %v", err)
	}
	// both specific, different ips: fine
	d := store.Mapping{ID: 3, Name: "d", Enabled: true, Engine: "nginx", Protocol: "http",
		ListenIP: "10.0.0.6", ListenPort: 8081, ServerNames: []string{"d.com"},
		Targets: b.Targets}
	if err := ValidateMapping(d, []store.Mapping{b}, 8080); err != nil {
		t.Errorf("different specific ips must not conflict: %v", err)
	}
}
