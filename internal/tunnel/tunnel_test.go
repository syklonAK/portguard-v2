package tunnel

import (
	"encoding/json"
	"testing"

	"portguard/internal/store"
)

func TestBuildBridgeConfig(t *testing.T) {
	cid := int64(7)
	relays := []store.TunnelRelay{
		{ID: 2, Name: "de-01", Mode: "tls", Enabled: true, TargetHost: "1.2.3.4", TargetPort: 443, BridgePort: 21001, HostHeader: "node.example.com"},
		{ID: 1, Name: "raw-01", Mode: "raw", Enabled: true, TargetHost: "5.6.7.8", TargetPort: 2053, ListenIP: "0.0.0.0", ListenPort: 8443, UDP: true},
		{ID: 3, Name: "disabled", Mode: "tls", Enabled: false, TargetHost: "9.9.9.9", TargetPort: 443, BridgePort: 21002},
	}
	_ = cid
	out, err := BuildBridgeConfig(relays, "127.0.0.1", 40001)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("bridge config is not valid JSON: %v", err)
	}
	inbounds, _ := cfg["inbounds"].([]any)
	if len(inbounds) != 2 {
		t.Fatalf("expected 2 inbounds (disabled relay skipped), got %d", len(inbounds))
	}
	// sorted by ID: raw-01 first
	first := inbounds[0].(map[string]any)
	if first["listen"] != "0.0.0.0" || first["port"] != float64(8443) {
		t.Errorf("raw relay inbound wrong: %v", first)
	}
	set := first["settings"].(map[string]any)
	if set["address"] != "5.6.7.8" || set["network"] != "tcp,udp" {
		t.Errorf("raw relay settings wrong: %v", set)
	}
	second := inbounds[1].(map[string]any)
	if second["listen"] != "127.0.0.1" || second["port"] != float64(21001) {
		t.Errorf("tls relay inbound wrong: %v", second)
	}
	// outbound: socks to hub
	outbounds := cfg["outbounds"].([]any)
	tun := outbounds[0].(map[string]any)
	if tun["tag"] != "tunnel" || tun["protocol"] != "socks" {
		t.Errorf("tunnel outbound wrong: %v", tun)
	}
	servers := tun["settings"].(map[string]any)["servers"].([]any)
	srv := servers[0].(map[string]any)
	if srv["address"] != "127.0.0.1" || srv["port"] != float64(40001) {
		t.Errorf("socks server wrong: %v", srv)
	}
}

func TestBuildBridgeConfigEmpty(t *testing.T) {
	out, err := BuildBridgeConfig(nil, "127.0.0.1", 40001)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	inbounds, _ := cfg["inbounds"].([]any)
	if len(inbounds) != 0 {
		t.Errorf("expected 0 inbounds, got %d", len(inbounds))
	}
}
