package proxy

import (
	"strings"
	"testing"

	"portguard/internal/store"
)

func TestNginxRenderPathRoutes(t *testing.T) {
	cid := int64(1)
	m := store.Mapping{
		ID: 60, Name: "xray", Enabled: true, Engine: "nginx", Protocol: "https",
		ListenIP: "0.0.0.0", ListenPort: 443, HTTP2: false, SSLCertID: &cid,
		ServerNames: []string{"node.example.com"},
		PathRoutes: []store.PathRoute{
			{Transport: store.PathTransportWS, Prefix: "ws", MinPort: 10000, MaxPort: 10003},
			{Transport: store.PathTransportHU, Prefix: "hu", MinPort: 10000, MaxPort: 10003},
			{Transport: store.PathTransportXHTTP, Prefix: "xhttp", MinPort: 10000, MaxPort: 10003},
		},
	}
	files, err := NginxEngine{}.Render([]store.Mapping{m}, nil, samplePaths())
	if err != nil {
		t.Fatal(err)
	}
	http := files["portguard/http.conf"]
	for _, want := range []string{
		`location ~ ^/ws/(?<pgport>\d+) {`,
		`if ($http_upgrade = "") { return 404; }`,
		`proxy_pass http://127.0.0.1:$pgport;`,
		`proxy_set_header Upgrade $http_upgrade;`,
		`proxy_set_header Connection "upgrade";`,
		`location ~ ^/hu/(?<pgport>\d+) {`,
		`location ~ ^/xhttp/(?<pgport>\d+)(/.*)? {`,
		`proxy_set_header Connection "";`,
		`proxy_buffering off;`,
		`proxy_request_buffering off;`,
		`proxy_read_timeout 1w;`,
	} {
		if !strings.Contains(http, want) {
			t.Errorf("http.conf missing %q\n---\n%s", want, http)
		}
	}
}

func TestNginxRenderPathRoutesRelayHost(t *testing.T) {
	m := store.Mapping{
		ID: 61, Name: "relay", Enabled: true, Engine: "nginx", Protocol: "http",
		ListenIP: "0.0.0.0", ListenPort: 2053,
		ServerNames: []string{"_"},
		Targets:   []store.Target{{Host: "10.144.144.1", Port: 1}}, // host part is what matters
		PathRoutes: []store.PathRoute{
			{Transport: store.PathTransportWS, Prefix: "ws"},
			{Transport: store.PathTransportXHTTP, Prefix: "xh"},
		},
	}
	files, err := NginxEngine{}.Render([]store.Mapping{m}, nil, samplePaths())
	if err != nil {
		t.Fatal(err)
	}
	http := files["portguard/http.conf"]
	if !strings.Contains(http, "proxy_pass http://10.144.144.1:$pgport;") {
		t.Errorf("relay host not used:\n%s", http)
	}
}

func TestHAProxyRenderPathRoutes(t *testing.T) {
	cid := int64(1)
	m := store.Mapping{
		ID: 62, Name: "xray-ha", Enabled: true, Engine: "haproxy", Protocol: "https",
		ListenIP: "0.0.0.0", ListenPort: 443, HTTP2: false, SSLCertID: &cid,
		ServerNames: []string{"node.example.com"},
		PathRoutes: []store.PathRoute{
			{Transport: store.PathTransportWS, Prefix: "ws", MinPort: 10000, MaxPort: 10003},
			{Transport: store.PathTransportHU, Prefix: "hu", MinPort: 10000, MaxPort: 10003},
			{Transport: store.PathTransportXHTTP, Prefix: "xhttp", MinPort: 10000, MaxPort: 10003},
		},
	}
	files, err := HAProxyEngine{}.Render([]store.Mapping{m}, nil, samplePaths())
	if err != nil {
		t.Fatal(err)
	}
	cfg := files["haproxy.cfg"]
	for _, want := range []string{
		// frontend: per-route ACLs with port-range regex
		"acl fe_pg62_ws path_reg ^/ws/(10000|10001|10002|10003)(/.*)?$",
		"acl fe_pg62_hu path_reg ^/hu/(10000|10001|10002|10003)(/.*)?$",
		"acl fe_pg62_xhttp path_reg ^/xhttp/(10000|10001|10002|10003)(/.*)?$",
		// Upgrade header required for ws and hu
		"acl fe_pg62_up req.hdr(Upgrade) -m found",
		"http-request deny deny_status 404 if fe_pg62_ws !fe_pg62_up",
		"http-request deny deny_status 404 if fe_pg62_hu !fe_pg62_up",
		// route to the dynamic backend
		"use_backend be_pg62_dyn if fe_pg62_ws || fe_pg62_hu || fe_pg62_xhttp",
		// 404 for everything else (no static targets)
		"http-request deny deny_status 404 if !fe_pg62_ws !fe_pg62_hu !fe_pg62_xhttp",
		// dynamic backend extracts the port and dials it
		"backend be_pg62_dyn",
		"http-request set-var(txn.pgport) path,regsub(^/ws/([0-9]+)(/.*)?$,\\1) if { path_reg ^/ws/(10000|10001|10002|10003)(/.*)?$ }",
		"http-request set-var(txn.pgport) path,regsub(^/hu/([0-9]+)(/.*)?$,\\1) if { path_reg ^/hu/(10000|10001|10002|10003)(/.*)?$ }",
		"http-request set-var(txn.pgport) path,regsub(^/xhttp/([0-9]+)(/.*)?$,\\1) if { path_reg ^/xhttp/(10000|10001|10002|10003)(/.*)?$ }",
		"http-request set-dst-port var(txn.pgport)",
		"server dynamic 127.0.0.1:1",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("haproxy.cfg missing %q\n---\n%s", want, cfg)
		}
	}
}

func TestValidatePathRoutes(t *testing.T) {
	base := store.Mapping{Name: "p", Enabled: true, Engine: "nginx", Protocol: "https",
		ListenIP: "0.0.0.0", ListenPort: 443, ServerNames: []string{"a.com"}, SSLCertID: int64Ptr(1),
		PathRoutes: []store.PathRoute{{Transport: store.PathTransportWS, Prefix: "ws"}}}

	// valid without targets (port comes from the path)
	if err := ValidateMapping(base, nil, 8080); err != nil {
		t.Errorf("valid path route rejected: %v", err)
	}
	// valid with port range
	base.PathRoutes = []store.PathRoute{{Transport: store.PathTransportWS, Prefix: "ws", MinPort: 10000, MaxPort: 10003}}
	if err := ValidateMapping(base, nil, 8080); err != nil {
		t.Errorf("valid ranged path route rejected: %v", err)
	}
	// invalid transport
	bad := base
	bad.PathRoutes = []store.PathRoute{{Transport: "grpc", Prefix: "ws"}}
	if err := ValidateMapping(bad, nil, 8080); err == nil {
		t.Error("invalid transport must be rejected")
	}
	// duplicate transport
	bad = base
	bad.PathRoutes = []store.PathRoute{
		{Transport: store.PathTransportWS, Prefix: "a"},
		{Transport: store.PathTransportWS, Prefix: "b"},
	}
	if err := ValidateMapping(bad, nil, 8080); err == nil {
		t.Error("duplicate transport must be rejected")
	}
	// prefix with slash
	bad = base
	bad.PathRoutes = []store.PathRoute{{Transport: store.PathTransportWS, Prefix: "a/b"}}
	if err := ValidateMapping(bad, nil, 8080); err == nil {
		t.Error("slash in prefix must be rejected")
	}
	// duplicate prefixes (different transports, same prefix)
	bad = base
	bad.PathRoutes = []store.PathRoute{
		{Transport: store.PathTransportWS, Prefix: "same"},
		{Transport: store.PathTransportXHTTP, Prefix: "same"},
	}
	if err := ValidateMapping(bad, nil, 8080); err == nil {
		t.Error("duplicate prefix must be rejected")
	}
	// bad port range
	bad = base
	bad.PathRoutes = []store.PathRoute{{Transport: store.PathTransportWS, Prefix: "ws", MinPort: 20000, MaxPort: 10000}}
	if err := ValidateMapping(bad, nil, 8080); err == nil {
		t.Error("inverted port range must be rejected")
	}
	// path routes on tcp
	bad = base
	bad.Protocol = "tcp"
	bad.ServerNames = nil
	bad.SSLCertID = nil
	if err := ValidateMapping(bad, nil, 8080); err == nil {
		t.Error("path routes on tcp must be rejected")
	}
	// combined with redirect
	bad = base
	bad.RedirectTo = "https://x.com"
	if err := ValidateMapping(bad, nil, 8080); err == nil {
		t.Error("path routes + redirect must be rejected")
	}
	// collides with path_prefix
	bad = base
	bad.PathPrefix = "/api"
	bad.PathRoutes = []store.PathRoute{{Transport: store.PathTransportWS, Prefix: "api"}}
	if err := ValidateMapping(bad, nil, 8080); err == nil {
		t.Error("prefix colliding with path_prefix must be rejected")
	}
}

func int64Ptr(v int64) *int64 { return &v }
