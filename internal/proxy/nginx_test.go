package proxy

import (
	"strings"
	"testing"

	"portguard/internal/store"
)

func samplePaths() Paths {
	return Paths{NginxConf: "/etc/nginx/nginx.conf", HAProxyConf: "/etc/haproxy/haproxy.cfg",
		CertsDir: "/var/lib/portguard/certs", BackupsDir: "/var/lib/portguard/backups",
		NginxBin: "/usr/sbin/nginx", HAProxyBin: "/usr/sbin/haproxy"}
}

func TestNginxRenderHTTPWithUpstream(t *testing.T) {
	m := store.Mapping{
		ID: 1, Name: "web", Enabled: true, Engine: "nginx", Protocol: "http",
		ListenIP: "0.0.0.0", ListenPort: 8081,
		ServerNames: []string{"example.com", "www.example.com"},
		WebSocket:   true,
		Targets:     []store.Target{{Host: "127.0.0.1", Port: 3000}, {Host: "127.0.0.1", Port: 3001, Weight: 2}},
	}
	files, err := NginxEngine{}.Render([]store.Mapping{m}, nil, samplePaths())
	if err != nil {
		t.Fatal(err)
	}
	http := files["portguard/http.conf"]
	for _, want := range []string{
		"listen 0.0.0.0:8081;",
		"server_name example.com www.example.com;",
		"upstream pg_upstream_1 {",
		"ip_hash;",
		"server 127.0.0.1:3000;",
		"server 127.0.0.1:3001 weight=2;",
		"proxy_pass http://pg_upstream_1;",
		"proxy_set_header Upgrade $http_upgrade;",
	} {
		if !strings.Contains(http, want) {
			t.Errorf("http.conf missing %q\n---\n%s", want, http)
		}
	}
	main := files["nginx.conf"]
	if !strings.Contains(main, "include /etc/nginx/portguard/http.conf;") ||
		!strings.Contains(main, "stream {") {
		t.Errorf("nginx.conf malformed:\n%s", main)
	}
}

func TestNginxRenderHTTPSAndStream(t *testing.T) {
	cid := int64(7)
	ms := []store.Mapping{
		{ID: 2, Name: "tls", Enabled: true, Engine: "nginx", Protocol: "https",
			ListenIP: "0.0.0.0", ListenPort: 8443, SSLCertID: &cid, HTTP2: true,
			ServerNames: []string{"a.example.com"},
			Targets:     []store.Target{{Host: "10.0.0.5", Port: 9000}}},
		{ID: 3, Name: "tcpfwd", Enabled: true, Engine: "nginx", Protocol: "tcp",
			ListenIP: "0.0.0.0", ListenPort: 9001,
			Targets: []store.Target{{Host: "10.0.0.6", Port: 9100}}},
		{ID: 4, Name: "udpfwd", Enabled: true, Engine: "nginx", Protocol: "udp",
			ListenIP: "0.0.0.0", ListenPort: 5353,
			Targets: []store.Target{{Host: "1.1.1.1", Port: 53}}},
	}
	files, err := NginxEngine{}.Render(ms, nil, samplePaths())
	if err != nil {
		t.Fatal(err)
	}
	http := files["portguard/http.conf"]
	for _, want := range []string{
		"listen 0.0.0.0:8443 ssl http2;",
		"ssl_certificate     /var/lib/portguard/certs/7.crt;",
		"ssl_certificate_key /var/lib/portguard/certs/7.key;",
		"proxy_pass http://10.0.0.5:9000;",
	} {
		if !strings.Contains(http, want) {
			t.Errorf("http.conf missing %q", want)
		}
	}
	stream := files["portguard/stream.conf"]
	for _, want := range []string{
		"upstream pg_stream_3 {",
		"listen 0.0.0.0:9001;",
		"listen 0.0.0.0:5353 udp;",
		"proxy_pass pg_stream_4;",
	} {
		if !strings.Contains(stream, want) {
			t.Errorf("stream.conf missing %q", want)
		}
	}
}

func TestNginxRenderRedirectOnly(t *testing.T) {
	m := store.Mapping{ID: 5, Name: "redir", Enabled: true, Engine: "nginx", Protocol: "http",
		ListenIP: "0.0.0.0", ListenPort: 80, ServerNames: []string{"old.example.com"},
		RedirectTo: "https://new.example.com"}
	files, err := NginxEngine{}.Render([]store.Mapping{m}, nil, samplePaths())
	if err != nil {
		t.Fatal(err)
	}
	http := files["portguard/http.conf"]
	if !strings.Contains(http, "return 301 https://new.example.com;") {
		t.Errorf("redirect missing:\n%s", http)
	}
	if strings.Contains(http, "proxy_pass") {
		t.Errorf("redirect mapping must not proxy_pass")
	}
}

func TestValidateMappingConflicts(t *testing.T) {
	existing := []store.Mapping{
		{ID: 1, Name: "a", Enabled: true, Engine: "nginx", Protocol: "http", ListenIP: "0.0.0.0", ListenPort: 80,
			ServerNames: []string{"x.com"}, Targets: []store.Target{{Host: "h", Port: 1}}},
	}
	// same port conflict
	err := ValidateMapping(store.Mapping{Name: "b", Enabled: true, Engine: "nginx", Protocol: "http",
		ListenIP: "0.0.0.0", ListenPort: 80, ServerNames: []string{"y.com"},
		Targets: []store.Target{{Host: "h", Port: 2}}}, existing, 8080)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Errorf("expected conflict error, got %v", err)
	}
	// disabled mapping does not conflict
	disabled := store.Mapping{ID: 1, Name: "a", Enabled: false, Engine: "nginx", Protocol: "http", ListenIP: "0.0.0.0", ListenPort: 80}
	err = ValidateMapping(store.Mapping{Name: "b", Enabled: true, Engine: "nginx", Protocol: "http",
		ListenIP: "0.0.0.0", ListenPort: 80, ServerNames: []string{"y.com"},
		Targets: []store.Target{{Host: "h", Port: 2}}}, []store.Mapping{disabled}, 8080)
	if err != nil {
		t.Errorf("disabled mapping should not conflict: %v", err)
	}
	// panel port guard
	err = ValidateMapping(store.Mapping{Name: "c", Enabled: true, Engine: "nginx", Protocol: "http",
		ListenIP: "0.0.0.0", ListenPort: 8080, ServerNames: []string{"y.com"},
		Targets: []store.Target{{Host: "h", Port: 2}}}, existing, 8080)
	if err == nil || !strings.Contains(err.Error(), "panel port") {
		t.Errorf("expected panel port error, got %v", err)
	}
	// https requires cert
	err = ValidateMapping(store.Mapping{Name: "d", Enabled: true, Engine: "nginx", Protocol: "https",
		ListenIP: "0.0.0.0", ListenPort: 444, ServerNames: []string{"y.com"},
		Targets: []store.Target{{Host: "h", Port: 2}}}, existing, 8080)
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Errorf("expected cert error, got %v", err)
	}
	// haproxy+udp rejected
	err = ValidateMapping(store.Mapping{Name: "e", Enabled: true, Engine: "haproxy", Protocol: "udp",
		ListenIP: "0.0.0.0", ListenPort: 5353, Targets: []store.Target{{Host: "h", Port: 53}}}, existing, 8080)
	if err == nil || !strings.Contains(err.Error(), "UDP") {
		t.Errorf("expected udp error, got %v", err)
	}
}

func TestValidHostnameAndIP(t *testing.T) {
	if !validHostname("sub.example.com") || !validHostname("my_host") {
		t.Error("valid hostname rejected")
	}
	if validHostname("-bad.com") || validHostname("bad..com") || validHostname("bad com") {
		t.Error("invalid hostname accepted")
	}
	m := store.Mapping{Name: "n", Engine: "nginx", Protocol: "tcp", ListenIP: "0.0.0.0", ListenPort: 7000,
		Targets: []store.Target{{Host: "not a host!", Port: 80}}}
	if err := ValidateMapping(m, nil, 8080); err == nil {
		t.Error("invalid target host accepted")
	}
}
