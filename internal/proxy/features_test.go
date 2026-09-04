package proxy

import (
	"strings"
	"testing"

	"portguard/internal/store"
)

func TestNginxRenderBalancePathACL(t *testing.T) {
	m := store.Mapping{
		ID: 40, Name: "api", Enabled: true, Engine: "nginx", Protocol: "http",
		ListenIP: "0.0.0.0", ListenPort: 8095,
		ServerNames: []string{"api.example.com"},
		Balance:     "round_robin",
		PathPrefix:  "/api/v2",
		AccessRules: []store.ACLRule{{Action: "allow", Value: "10.0.0.0/8"}, {Action: "deny", Value: "10.9.9.9"}},
		Targets:     []store.Target{{Host: "127.0.0.1", Port: 7000}, {Host: "127.0.0.1", Port: 7001}},
	}
	files, err := NginxEngine{}.Render([]store.Mapping{m}, nil, samplePaths())
	if err != nil {
		t.Fatal(err)
	}
	http := files["portguard/http.conf"]
	for _, want := range []string{
		"location /api/v2 {",
		"allow 10.0.0.0/8;",
		"deny 10.9.9.9;",
		"deny all;",
		"return 404;",
	} {
		if !strings.Contains(http, want) {
			t.Errorf("http.conf missing %q\n---\n%s", want, http)
		}
	}
	// round_robin is the nginx default: no balancing directive at all
	up := "upstream pg_upstream_40 {"
	if i := strings.Index(http, up); i >= 0 {
		body := http[i : i+200]
		if strings.Contains(body, "least_conn") || strings.Contains(body, "ip_hash") {
			t.Errorf("round_robin must not render a directive:\n%s", body)
		}
	}
}

func TestNginxRenderStreamBalance(t *testing.T) {
	m := store.Mapping{
		ID: 41, Name: "tcp-lc", Enabled: true, Engine: "nginx", Protocol: "tcp",
		ListenIP: "0.0.0.0", ListenPort: 9200, Balance: "least_conn",
		Targets: []store.Target{{Host: "127.0.0.1", Port: 9201}, {Host: "127.0.0.1", Port: 9202}},
	}
	files, err := NginxEngine{}.Render([]store.Mapping{m}, nil, samplePaths())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(files["portguard/stream.conf"], "least_conn;") {
		t.Errorf("stream least_conn missing:\n%s", files["portguard/stream.conf"])
	}
}

func TestHAProxyRenderBalancePathACL(t *testing.T) {
	m := store.Mapping{
		ID: 50, Name: "api", Enabled: true, Engine: "haproxy", Protocol: "http",
		ListenIP: "0.0.0.0", ListenPort: 8096,
		ServerNames: []string{"api.example.com"},
		Balance:     "leastconn",
		PathPrefix:  "/api",
		AccessRules: []store.ACLRule{{Action: "allow", Value: "192.168.0.0/16"}, {Action: "deny", Value: "192.168.5.5"}},
		Targets:     []store.Target{{Host: "127.0.0.1", Port: 7100}},
	}
	files, err := HAProxyEngine{}.Render([]store.Mapping{m}, nil, samplePaths())
	if err != nil {
		t.Fatal(err)
	}
	cfg := files["haproxy.cfg"]
	for _, want := range []string{
		"balance leastconn",
		"acl fe_pg50_path path_beg /api",
		"use_backend be_pg50 if fe_pg50_path",
		"http-request deny deny_status 404 if !fe_pg50_path",
		"http-request allow if { src 192.168.0.0/16 }",
		"http-request deny if { src 192.168.5.5 }",
		"http-request deny\n", // final whitelist deny
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("haproxy.cfg missing %q\n---\n%s", want, cfg)
		}
	}
}

func TestHAProxyRenderTCPACL(t *testing.T) {
	m := store.Mapping{
		ID: 51, Name: "db", Enabled: true, Engine: "haproxy", Protocol: "tcp",
		ListenIP: "0.0.0.0", ListenPort: 15433,
		AccessRules: []store.ACLRule{{Action: "deny", Value: "203.0.113.0/24"}},
		Targets:     []store.Target{{Host: "127.0.0.1", Port: 5432}},
	}
	files, err := HAProxyEngine{}.Render([]store.Mapping{m}, nil, samplePaths())
	if err != nil {
		t.Fatal(err)
	}
	cfg := files["haproxy.cfg"]
	for _, want := range []string{
		"tcp-request connection reject if { src 203.0.113.0/24 }",
		"mode tcp",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("haproxy.cfg missing %q\n---\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "http-request") {
		t.Errorf("tcp mapping must not render http-request rules:\n%s", cfg)
	}
}

func TestValidBalanceMatrix(t *testing.T) {
	haproxyOK := []string{"", "roundrobin", "leastconn", "source", "uri", "random", "first", "static-rr"}
	for _, b := range haproxyOK {
		if !ValidBalance("haproxy", "http", b) {
			t.Errorf("haproxy http balance %q must be valid", b)
		}
	}
	if ValidBalance("haproxy", "http", "least_conn") {
		t.Error("haproxy does not accept least_conn")
	}
	for _, b := range []string{"", "round_robin", "least_conn", "ip_hash", "random"} {
		if !ValidBalance("nginx", "http", b) {
			t.Errorf("nginx http balance %q must be valid", b)
		}
	}
	if !ValidBalance("nginx", "tcp", "least_conn") || ValidBalance("nginx", "tcp", "ip_hash") {
		t.Error("nginx stream balance validation broken")
	}
}

func TestValidateMappingNewFields(t *testing.T) {
	base := store.Mapping{Name: "x", Enabled: true, Engine: "haproxy", Protocol: "http",
		ListenIP: "0.0.0.0", ListenPort: 9001, ServerNames: []string{"a.com"},
		Targets: []store.Target{{Host: "h", Port: 1}}}
	// bad balance
	bad := base
	bad.Balance = "least_conn"
	if err := ValidateMapping(bad, nil, 8080); err == nil {
		t.Error("haproxy must reject least_conn")
	}
	// bad path prefix
	bad = base
	bad.PathPrefix = "api"
	if err := ValidateMapping(bad, nil, 8080); err == nil {
		t.Error("path_prefix without leading / must be rejected")
	}
	// path prefix on tcp
	bad = base
	bad.Protocol = "tcp"
	bad.ServerNames = nil
	bad.PathPrefix = "/api"
	if err := ValidateMapping(bad, nil, 8080); err == nil {
		t.Error("path_prefix on tcp must be rejected")
	}
	// bad ACL value
	bad = base
	bad.AccessRules = []store.ACLRule{{Action: "allow", Value: "not-an-ip"}}
	if err := ValidateMapping(bad, nil, 8080); err == nil {
		t.Error("invalid ACL CIDR must be rejected")
	}
	// valid ACL
	bad = base
	bad.AccessRules = []store.ACLRule{{Action: "deny", Value: "10.0.0.0/8"}}
	if err := ValidateMapping(bad, nil, 8080); err != nil {
		t.Errorf("valid ACL rejected: %v", err)
	}
}
