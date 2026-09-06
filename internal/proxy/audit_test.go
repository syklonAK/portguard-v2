package proxy

// Audit regression tests: staging path containment, unicode control
// character rejection, HAProxy dynamic-port regsub rendering and the
// deny/default_backend interaction for path-routed mappings.

import (
	"strings"
	"testing"

	"portguard/internal/store"
)

func TestSecureJoin(t *testing.T) {
	tests := []struct {
		name    string
		rel     string
		wantOK  bool
		wantSub string // substring the accepted path must contain
	}{
		{name: "normal relative path", rel: "portguard/decoy/index.html", wantOK: true, wantSub: "decoy"},
		{name: "nested relative path", rel: "portguard/a/b/c.conf", wantOK: true, wantSub: "a/b/c.conf"},
		{name: "bare name", rel: "portguard/x.conf", wantOK: true, wantSub: "x.conf"},
		{name: "single parent hop", rel: "portguard/../x.conf", wantOK: false},
		{name: "escape to /etc/passwd", rel: "../../etc/passwd", wantOK: false},
		{name: "absolute path", rel: "/etc/passwd", wantOK: false},
		{name: "portguard prefix then escape", rel: "portguard/../../etc/shadow", wantOK: false},
		{name: "root itself", rel: "portguard/", wantOK: false},
		{name: "dot", rel: ".", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := secureJoin("/etc/nginx/conf.d", tc.rel)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("expected %q to be accepted, got error: %v", tc.rel, err)
				}
				if tc.wantSub != "" && !strings.Contains(got, tc.wantSub) {
					t.Fatalf("path %q should contain %q, got %q", tc.rel, tc.wantSub, got)
				}
			} else if err == nil {
				t.Fatalf("expected %q to be rejected, got %q", tc.rel, got)
			}
		})
	}
}

// The accepted paths must never leave the configured root — including the
// similar-prefix trap where "/etc/portguard-evil" shares a string prefix
// with "/etc/portguard".
func TestSecureJoinPrefixBoundary(t *testing.T) {
	got, err := secureJoin("/etc/portguard", "../portguard-evil/x.conf")
	if err == nil {
		t.Fatalf("similar-prefix sibling must be rejected, got %q", got)
	}
	if strings.Contains(got, "portguard-evil") && strings.HasPrefix(got, "/etc/portguard/") {
		t.Fatalf("escape succeeded: %q", got)
	}
}

func TestHasControlChars(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "empty", in: "", want: false},
		{name: "plain ascii", in: "hello-world_1", want: false},
		{name: "newline", in: "a\nb", want: true},
		{name: "carriage return", in: "a\rb", want: true},
		{name: "tab", in: "a\tb", want: true},
		{name: "nul", in: "a\x00b", want: true},
		{name: "ascii unit separator", in: "a\x1fb", want: true},
		{name: "rtl override U+202E", in: "safe\u202evil", want: true},
		{name: "lrm U+200E", in: "safe\u200evil", want: true},
		{name: "zwsp U+200B (Cf)", in: "safe\u200bpot", want: true},
		{name: "c1 control U+0085", in: "a\u0085b", want: true},
		{name: "persian text allowed", in: "سرور-مین", want: false},
		{name: "emoji allowed", in: "prod-🚀", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasControlChars(tc.in); got != tc.want {
				t.Fatalf("hasControlChars(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// The dynamic backend must quote the user-controlled prefix inside its
// regexes and must guard the port extraction with the numeric-port ACL,
// otherwise a non-numeric path would leave txn.pgport as the raw path.
func TestRenderHADynBackendQuotesPrefixAndRequiresPort(t *testing.T) {
	m := store.Mapping{
		Name: "dyn",
		PathRoutes: []store.PathRoute{
			{Transport: store.PathTransportWS, Prefix: "ws.x", MinPort: 1, MaxPort: 65535},
		},
		Targets: []store.Target{{Host: "127.0.0.1", Port: 1}},
	}
	out := renderHADynBackend("m_dyn", m)
	if !strings.Contains(out, `path,regsub(^/ws\.x/([0-9]+)(/.*)?$,\1)`) {
		t.Fatalf("prefix must be regex-quoted in the regsub, got:\n%s", out)
	}
	if !strings.Contains(out, `{ path_reg ^/ws\.x/[0-9]+(/.*)?$ }`) {
		t.Fatalf("guard ACL must require the numeric port, got:\n%s", out)
	}
}

// Path-routed mappings without static targets deny unmatched requests with
// 404 before default_backend can pick anything; mappings WITH targets must
// not emit the deny (unmatched traffic falls through to default_backend).
func TestRenderHAPathACLDenyInteraction(t *testing.T) {
	routes := []store.PathRoute{{Transport: store.PathTransportWS, Prefix: "ws", MinPort: 1, MaxPort: 65535}}

	noTargets := store.Mapping{Name: "p", PathRoutes: routes}
	out := renderHAPathRoutes("fe", "be", noTargets)
	if !strings.Contains(out, "http-request deny deny_status 404 if !fe_ws") {
		t.Fatalf("targets=0 mapping must deny unmatched requests, got:\n%s", out)
	}

	withTargets := store.Mapping{Name: "p", PathRoutes: routes, Targets: []store.Target{{Host: "10.0.0.1", Port: 80}}}
	out = renderHAPathRoutes("fe", "be", withTargets)
	if strings.Contains(out, "deny_status 404 if !fe_ws") {
		t.Fatalf("targets>0 mapping must keep default_backend reachable, got:\n%s", out)
	}
	if !strings.Contains(out, "use_backend be_dyn if fe_ws") {
		t.Fatalf("matched route must use the dynamic backend, got:\n%s", out)
	}
}
