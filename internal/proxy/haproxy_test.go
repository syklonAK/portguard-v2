package proxy

import (
	"strings"
	"testing"

	"portguard/internal/store"
)

func TestHAProxyRenderHTTPAndTCP(t *testing.T) {
	ms := []store.Mapping{
		{ID: 10, Name: "web", Enabled: true, Engine: "haproxy", Protocol: "http",
			ListenIP: "0.0.0.0", ListenPort: 8082, WebSocket: true,
			Targets: []store.Target{{Host: "127.0.0.1", Port: 3000}, {Host: "127.0.0.1", Port: 3001, Backup: true}}},
		{ID: 11, Name: "dbfwd", Enabled: true, Engine: "haproxy", Protocol: "tcp",
			ListenIP: "0.0.0.0", ListenPort: 15432,
			Targets: []store.Target{{Host: "10.0.0.9", Port: 5432}}},
		{ID: 12, Name: "disabled", Enabled: false, Engine: "haproxy", Protocol: "http",
			ListenIP: "0.0.0.0", ListenPort: 9999, Targets: []store.Target{{Host: "x", Port: 1}}},
	}
	files, err := HAProxyEngine{}.Render(ms, nil, samplePaths())
	if err != nil {
		t.Fatal(err)
	}
	cfg := files["haproxy.cfg"]
	for _, want := range []string{
		"frontend fe_pg10",
		"bind *:8082",
		"default_backend be_pg10",
		"backend be_pg10",
		"server s0 127.0.0.1:3000 check",
		"server s1 127.0.0.1:3001 backup check",
		"mode tcp",
		"bind *:15432",
		"server s0 10.0.0.9:5432 check",
		"timeout tunnel 3600s",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("haproxy.cfg missing %q\n---\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "9999") {
		t.Errorf("disabled mapping must not be rendered")
	}
}

func TestHAProxyRenderHTTPS(t *testing.T) {
	cid := int64(3)
	m := store.Mapping{ID: 20, Name: "tls", Enabled: true, Engine: "haproxy", Protocol: "https",
		ListenIP: "0.0.0.0", ListenPort: 8443, SSLCertID: &cid, HTTP2: true,
		Targets: []store.Target{{Host: "127.0.0.1", Port: 8080}}}
	files, err := HAProxyEngine{}.Render([]store.Mapping{m}, nil, samplePaths())
	if err != nil {
		t.Fatal(err)
	}
	cfg := files["haproxy.cfg"]
	if !strings.Contains(cfg, "bind *:8443 ssl crt /var/lib/portguard/certs/3.pem alpn h2,http/1.1") {
		t.Errorf("https bind missing:\n%s", cfg)
	}
}

func TestHAProxyUDPRejected(t *testing.T) {
	m := store.Mapping{ID: 30, Name: "u", Enabled: true, Engine: "haproxy", Protocol: "udp",
		ListenIP: "0.0.0.0", ListenPort: 5353, Targets: []store.Target{{Host: "1.1.1.1", Port: 53}}}
	eng := HAProxyEngine{}
	if _, err := eng.Render([]store.Mapping{m}, nil, samplePaths()); err == nil {
		t.Error("expected udp render error for haproxy")
	}
}
