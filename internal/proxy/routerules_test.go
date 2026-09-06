package proxy

import (
	"strings"
	"testing"

	"portguard/internal/store"
)

// v2.8 route rules: validation
func TestValidateRouteRules(t *testing.T) {
	base := store.Mapping{Name: "x", Enabled: true, Engine: "nginx", Protocol: "https",
		ListenIP: "0.0.0.0", ListenPort: 443, ServerNames: []string{"a.com"}, SSLCertID: int64Ptr(1),
		Targets: []store.Target{{Host: "127.0.0.1", Port: 80}}}

	// clean rules pass
	ok := base
	ok.Routes = []store.RouteRule{
		{ID: 1, Path: "/ws/*", Enabled: true, Targets: []store.Target{{Host: "10.0.0.1", Port: 10001}}},
		{ID: 2, Path: "/api/*", Enabled: true, Redirect: "https://api.example.com"},
	}
	if err := ValidateMapping(ok, nil, 8080); err != nil {
		t.Fatalf("valid route rules rejected: %v", err)
	}

	// dup paths
	bad := base
	bad.Routes = []store.RouteRule{
		{ID: 1, Path: "/x", Enabled: true, Targets: base.Targets},
		{ID: 2, Path: "/x", Enabled: true, Targets: base.Targets},
	}
	if err := ValidateMapping(bad, nil, 8080); err == nil {
		t.Error("duplicate paths must be rejected")
	}
	// no path slash
	bad = base
	bad.Routes = []store.RouteRule{{ID: 1, Path: "ws", Enabled: true, Targets: base.Targets}}
	if err := ValidateMapping(bad, nil, 8080); err == nil {
		t.Error("path without leading slash must be rejected")
	}
	// empty rule
	bad = base
	bad.Routes = []store.RouteRule{{ID: 1, Path: "/x", Enabled: true}}
	if err := ValidateMapping(bad, nil, 8080); err == nil {
		t.Error("rule without targets/redirect must be rejected")
	}
	// tcp mapping with rules
	bad = base
	bad.Protocol = "tcp"
	bad.ServerNames = nil
	bad.SSLCertID = nil
	bad.Routes = []store.RouteRule{{ID: 1, Path: "/x", Enabled: true, Targets: base.Targets}}
	if err := ValidateMapping(bad, nil, 8080); err == nil {
		t.Error("route rules on tcp must be rejected")
	}
	// haproxy wildcard
	bad = base
	bad.Engine = "haproxy"
	bad.Routes = []store.RouteRule{{ID: 1, Path: "/ws/*", Enabled: true, Targets: base.Targets}}
	if err := ValidateMapping(bad, nil, 8080); err == nil {
		t.Error("haproxy wildcard path must be rejected")
	}
}

// v2.8 route rules: nginx render
func TestNginxRenderRouteRules(t *testing.T) {
	m := store.Mapping{
		ID: 70, Name: "router", Enabled: true, Engine: "nginx", Protocol: "https",
		ListenIP: "0.0.0.0", ListenPort: 443, HTTP2: false, SSLCertID: int64Ptr(1),
		ServerNames: []string{"edge.example.com"},
		Targets: []store.Target{{Host: "127.0.0.1", Port: 8080}}, // fallback
		Routes: []store.RouteRule{
			{ID: 1, Path: "/api/*", Enabled: true, Targets: []store.Target{{Host: "10.0.0.1", Port: 9000}}},
			{ID: 2, Path: "/ws/*", Enabled: true, Targets: []store.Target{{Host: "10.0.0.2", Port: 10001}, {Host: "10.0.0.3", Port: 10001}}},
			{ID: 3, Path: "/old", Enabled: true, Redirect: "https://new.example.com"},
			{ID: 4, Path: "/disabled", Enabled: false, Targets: []store.Target{{Host: "9.9.9.9", Port: 9}}},
		},
	}
	files, err := NginxEngine{}.Render([]store.Mapping{m}, nil, samplePaths())
	if err != nil {
		t.Fatal(err)
	}
	http := files["portguard/http.conf"]
	// single-target rule renders a direct proxy_pass
	if !strings.Contains(http, "location /api/ {\n            proxy_pass http://10.0.0.1:9000;") {
		t.Errorf("single-target rule not rendered:\n%s", http)
	}
	// multi-target rule gets its own upstream with both members
	if !strings.Contains(http, "upstream pg_up_70_r2 {") {
		t.Errorf("multi-target upstream missing:\n%s", http)
	}
	if strings.Count(http, "10.0.0.2:10001") < 1 || strings.Count(http, "10.0.0.3:10001") < 1 {
		t.Errorf("upstream members missing:\n%s", http)
	}
	if !strings.Contains(http, "location /ws/ {\n            proxy_pass http://pg_up_70_r2;") {
		t.Errorf("multi-target rule location missing:\n%s", http)
	}
	// redirect rule
	if !strings.Contains(http, "location /old {\n            return 301 https://new.example.com;") {
		t.Errorf("redirect rule missing:\n%s", http)
	}
	// disabled rule must NOT appear
	if strings.Contains(http, "9.9.9.9") {
		t.Errorf("disabled rule leaked into config:\n%s", http)
	}
	// fallback still present
	if !strings.Contains(http, "location / {\n") {
		t.Errorf("fallback /* location missing:\n%s", http)
	}
}

// v2.8 route rules: haproxy render
func TestHAProxyRenderRouteRules(t *testing.T) {
	m := store.Mapping{
		ID: 71, Name: "router-ha", Enabled: true, Engine: "haproxy", Protocol: "http",
		ListenIP: "0.0.0.0", ListenPort: 8081,
		ServerNames: []string{"_"},
		Targets: []store.Target{{Host: "127.0.0.1", Port: 8080}},
		Routes: []store.RouteRule{
			{ID: 1, Path: "/api", Enabled: true, Targets: []store.Target{{Host: "10.0.0.1", Port: 9000}}},
			{ID: 2, Path: "/ws", Enabled: true, Targets: []store.Target{{Host: "10.0.0.2", Port: 10001}}},
		},
	}
	files, err := HAProxyEngine{}.Render([]store.Mapping{m}, nil, samplePaths())
	if err != nil {
		t.Fatal(err)
	}
	cfg := files["haproxy.cfg"]
	if !strings.Contains(cfg, "acl fe_pg71_r1 path_beg /api") {
		t.Errorf("rule acl missing:\n%s", cfg)
	}
	if !strings.Contains(cfg, "use_backend be_pg71_r1 if fe_pg71_r1") {
		t.Errorf("use_backend missing:\n%s", cfg)
	}
	if !strings.Contains(cfg, "backend be_pg71_r1") {
		t.Errorf("rule backend missing:\n%s", cfg)
	}
	if !strings.Contains(cfg, "default_backend be_pg71") {
		t.Errorf("default backend missing:\n%s", cfg)
	}
}

func TestRouteRulePathForNginx(t *testing.T) {
	if got := RouteRulePathForNginx("/ws/*"); got != "/ws/" {
		t.Errorf("wildcard conversion = %q, want /ws/", got)
	}
	if got := RouteRulePathForNginx("/api"); got != "/api" {
		t.Errorf("plain path = %q", got)
	}
}
