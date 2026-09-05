package proxy

import (
	"testing"

	"portguard/internal/store"
)

const sampleNginxConf = `
http {
    upstream myapp {
        server 10.0.0.5:3000;
        server 10.0.0.6:3000;
    }
    server {
        listen 443 ssl;
        server_name shop.example.com;
        location / {
            proxy_pass http://myapp;
        }
    }
    server {
        listen 8080;
        server_name old.example.com;
        return 301 https://new.example.com$request_uri;
    }
    server {
        listen 80;
        location /api/ {
            proxy_pass http://127.0.0.1:9000;
        }
    }
}
stream {
    server {
        listen 8443;
        proxy_pass 10.0.0.9:2053;
    }
}
`

func TestParseNginxConfig(t *testing.T) {
	sites := ParseNginxConfig(sampleNginxConf)
	if len(sites.HTTPServers) != 3 {
		t.Fatalf("http servers = %d, want 3: %+v", len(sites.HTTPServers), sites.HTTPServers)
	}
	if len(sites.StreamServers) != 1 {
		t.Fatalf("stream servers = %d, want 1", len(sites.StreamServers))
	}
	// upstream resolution
	if ups, ok := sites.UpstreamsByName["myapp"]; !ok || len(ups) != 2 {
		t.Fatalf("upstream myapp = %v", sites.UpstreamsByName)
	}
	// first server: 443 ssl shop.example.com -> myapp targets
	s0 := sites.HTTPServers[0]
	if s0.ListenPort != 443 || !s0.ListenSSL || s0.ServerNames[0] != "shop.example.com" {
		t.Errorf("server0 wrong: %+v", s0)
	}
	// second: redirect
	s1 := sites.HTTPServers[1]
	if s1.ListenPort != 8080 || s1.Return == "" {
		t.Errorf("server1 (redirect) wrong: %+v", s1)
	}
}

func TestNginxSitesToMappings(t *testing.T) {
	sites := ParseNginxConfig(sampleNginxConf)
	mappings, _ := NginxSitesToMappings(sites, nil)
	if len(mappings) != 4 { // 3 http + 1 stream
		t.Fatalf("mappings = %d, want 4", len(mappings))
	}
	for _, m := range mappings {
		if m.Enabled {
			t.Error("imports must be created disabled")
		}
		if m.Engine != "nginx" {
			t.Errorf("engine = %s", m.Engine)
		}
	}
	// the https one
	var https *store.Mapping
	for i := range mappings {
		if mappings[i].Protocol == "https" {
			https = &mappings[i]
		}
	}
	if https == nil {
		t.Fatal("no https mapping found")
	}
	if len(https.Targets) != 2 || https.Targets[0].Host != "10.0.0.5" {
		t.Errorf("upstream targets not resolved: %+v", https.Targets)
	}
	// stream import resolves its proxy_pass target
	var stream *store.Mapping
	for i := range mappings {
		if mappings[i].Protocol == "tcp" {
			stream = &mappings[i]
		}
	}
	if stream == nil {
		t.Fatal("no stream mapping found")
	}
	if len(stream.Targets) != 1 || stream.Targets[0].Host != "10.0.0.9" || stream.Targets[0].Port != 2053 {
		t.Errorf("stream target not resolved: %+v", stream.Targets)
	}
}
