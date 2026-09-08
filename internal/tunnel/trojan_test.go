package tunnel

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIsValidTrojanName(t *testing.T) {
	for _, ok := range []string{"de-01", "relay_A", "tj123"} {
		if !IsValidTrojanName(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "has space", "dot.name", "unicode؟", strings.Repeat("x", 65)} {
		if IsValidTrojanName(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestIsPrivateOrLoopback(t *testing.T) {
	for _, priv := range []string{"127.0.0.1", "127.8.8.8", "10.1.2.3", "192.168.1.1", "172.16.0.1", "172.31.255.1", "100.64.0.1", "100.100.1.1"} {
		if !IsPrivateOrLoopback(priv) {
			t.Errorf("%q should be private", priv)
		}
	}
	for _, pub := range []string{"1.2.3.4", "5.6.7.8", "172.32.0.1", "100.128.0.1", "9.9.9.9"} {
		if IsPrivateOrLoopback(pub) {
			t.Errorf("%q should NOT be private", pub)
		}
	}
}

func TestResolveTrojanRoute(t *testing.T) {
	if got := ResolveTrojanRoute("127.0.0.1", ""); got != "direct" {
		t.Errorf("loopback should be direct, got %s", got)
	}
	if got := ResolveTrojanRoute("10.0.0.5", ""); got != "direct" {
		t.Errorf("private should be direct, got %s", got)
	}
	if got := ResolveTrojanRoute("1.2.3.4", ""); got != "tunnel" {
		t.Errorf("public should be tunnel, got %s", got)
	}
	if got := ResolveTrojanRoute("1.2.3.4", "direct"); got != "direct" {
		t.Errorf("explicit request must win, got %s", got)
	}
}

func TestNextTrojanBridgePort(t *testing.T) {
	if got := NextTrojanBridgePort(nil, 22000); got != 22000 {
		t.Errorf("first free = %d", got)
	}
	used := []int{22000, 22001, 22003}
	if got := NextTrojanBridgePort(used, 22000); got != 22002 {
		t.Errorf("gap-finding failed: %d", got)
	}
	if got := NextTrojanBridgePort(nil, 1000); got != 22000 {
		t.Errorf("base below floor must be raised, got %d", got)
	}
}

func TestBuildTrojanBridgeConfig(t *testing.T) {
	relays := []TrojanRelaySpec{
		{Name: "de-01", Domain: "de.example.com", HTTPSPort: 443, ForeignIP: "5.6.7.8", ForeignPort: 10000, BridgePort: 22001, Route: "tunnel", SocksPort: 40001},
		{Name: "local", Domain: "lo.example.com", HTTPSPort: 8443, ForeignIP: "127.0.0.1", ForeignPort: 10000, BridgePort: 22002, Route: "", SocksPort: 0},
		{Name: "mesh", Domain: "mesh.example.com", HTTPSPort: 444, ForeignIP: "10.0.0.5", ForeignPort: 10000, BridgePort: 22003, Route: "tunnel", SocksPort: 40001},
	}
	out, err := BuildTrojanBridgeConfig(relays, 40001)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	inbounds, _ := cfg["inbounds"].([]any)
	if len(inbounds) != 3 {
		t.Fatalf("expected 3 inbounds, got %d", len(inbounds))
	}
	outbounds, _ := cfg["outbounds"].([]any)
	// distinct socks ports: 40001 only → 1 tunnel outbound + direct
	if len(outbounds) != 2 {
		t.Fatalf("expected 2 outbounds (tunnel + direct), got %d", len(outbounds))
	}
	rules, _ := cfg["routing"].(map[string]any)["rules"].([]any)
	if len(rules) != 4 { // 3 relays + catch-all
		t.Fatalf("expected 4 routing rules, got %d", len(rules))
	}
	// local (loopback) relay must route direct despite route=""
	rule0 := rules[0].(map[string]any)
	if rule0["outboundTag"] != "tunnel-40001" {
		t.Errorf("public relay should use the tunnel, got %v", rule0["outboundTag"])
	}
	rule1 := rules[1].(map[string]any)
	if rule1["outboundTag"] != "direct" {
		t.Errorf("loopback relay must go direct, got %v", rule1["outboundTag"])
	}
	rule2 := rules[2].(map[string]any)
	if rule2["outboundTag"] != "direct" {
		t.Errorf("private mesh relay must go direct, got %v", rule2["outboundTag"])
	}
}

func TestBuildTrojanBridgeConfigMultiSocks(t *testing.T) {
	relays := []TrojanRelaySpec{
		{Name: "a", ForeignIP: "1.2.3.4", ForeignPort: 10000, BridgePort: 22001, SocksPort: 40001},
		{Name: "b", ForeignIP: "5.6.7.8", ForeignPort: 10000, BridgePort: 22002, SocksPort: 40002},
	}
	out, err := BuildTrojanBridgeConfig(relays, 40001)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	outbounds, _ := cfg["outbounds"].([]any)
	if len(outbounds) != 3 { // tunnel-40001 + tunnel-40002 + direct
		t.Fatalf("expected 3 outbounds for 2 hub ports, got %d", len(outbounds))
	}
}

func TestBuildTrojanIngressConfig(t *testing.T) {
	fwd := []TrojanIngressSpec{
		{Name: "ing1", ListenIP: "0.0.0.0", ListenPort: 35001, TargetHost: "127.0.0.1", TargetPort: 10000},
		{Name: "ing2", ListenIP: "10.0.0.5", ListenPort: 35002, TargetHost: "192.168.1.10", TargetPort: 10001, UDP: true},
	}
	out, err := BuildTrojanIngressConfig(fwd)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	inbounds, _ := cfg["inbounds"].([]any)
	if len(inbounds) != 2 {
		t.Fatalf("expected 2 inbounds, got %d", len(inbounds))
	}
	ib := inbounds[0].(map[string]any)
	if ib["listen"] != "0.0.0.0" || ib["port"] != float64(35001) {
		t.Errorf("ingress inbound wrong: %v", ib)
	}
	ib2 := inbounds[1].(map[string]any)
	if ib2["listen"] != "10.0.0.5" || ib2["port"] != float64(35002) {
		t.Errorf("ingress inbound 2 wrong: %v", ib2)
	}
	set := ib2["settings"].(map[string]any)
	if set["address"] != "192.168.1.10" || set["network"] != "tcp,udp" {
		t.Errorf("udp target settings wrong: %v", set)
	}
}

func TestTrojanUnitBodies(t *testing.T) {
	body := TrojanBridgeUnitBody("/usr/local/bin/xray", "/etc/hedioum-suite/portguard-trojan-bridge.json")
	if !strings.Contains(body, "ExecStart=/usr/local/bin/xray run -config /etc/hedioum-suite/portguard-trojan-bridge.json") {
		t.Errorf("bridge unit ExecStart wrong:\n%s", body)
	}
	if !strings.Contains(body, "After=network-online.target hedioum.service") {
		t.Errorf("bridge unit must start after hedioum")
	}
	ing := TrojanIngressUnitBody("", "/tmp/x.json")
	if !strings.Contains(ing, "ExecStart=/usr/local/bin/xray run -config /tmp/x.json") {
		t.Errorf("ingress unit default binary wrong:\n%s", ing)
	}
}

func TestBBRSysctlContent(t *testing.T) {
	// the tuning file must carry the exact values from the bash original
	for _, want := range []string{
		"net.core.default_qdisc = fq",
		"net.ipv4.tcp_congestion_control = bbr",
		"net.core.somaxconn = 65535",
		"net.ipv4.tcp_fastopen = 3",
		"fs.file-max = 1000000",
	} {
		if !strings.Contains(bbrSysctl, want) {
			t.Errorf("sysctl content missing %q", want)
		}
	}
}

func TestAllowPortValidation(t *testing.T) {
	if _, err := AllowPort(FWUfw, 0, ""); err == nil {
		t.Error("port 0 must be rejected")
	}
	if _, err := AllowPort(FWIptables, 8080, "evil;reboot"); err == nil {
		t.Error("source injection must be rejected")
	}
	// "none" firewall is a no-op with a message
	msg, err := AllowPort(FWNone, 443, "")
	if err != nil || msg == "" {
		t.Errorf("FWNone should be a no-op message, got %q, %v", msg, err)
	}
}
