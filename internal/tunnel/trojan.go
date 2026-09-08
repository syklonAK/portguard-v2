// Package tunnel: L4 Trojan relays — the PortGuard port of the
// hedioum-allinone "trojan relay" suite. One relay = one nginx stream{}
// TLS listener (certificate terminates on this server) + one local xray
// dokodemo bridge port that forwards into the tunnel (or directly to a
// local/mesh node). Everything is managed like every other PortGuard
// resource: rows in the DB, rendered by the apply pipeline, validated with
// `xray run -test` and `nginx -t` before anything touches the live box.
package tunnel

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	// TrojanBridgeUnit is the systemd unit running the trojan dokodemo bridge.
	// Named for discoverability: unit-name guards elsewhere accept
	// "portguard-*" so it stays inside the managed family.
	TrojanBridgeUnit = "portguard-trojan-bridge.service"
	// TrojanIngressUnit is the foreign-side ingress forwarder unit
	// (tunnel port → node inbound on 127.0.0.1).
	TrojanIngressUnit = "portguard-trojan-ingress.service"
	// TrojanBridgePortBase is where free dokodemo bridge ports are scanned from.
	TrojanBridgePortBase = 22000
	// TrojanIngressPortSuggest is the suggested public tunnel port for ingresses.
	TrojanIngressPortSuggest = 35000
	// NodeIngressPortDefault is the default trojan inbound port on the node
	// (127.0.0.1:10000 in the hedioum-suite topology).
	NodeIngressPortDefault = 10000
)

// TrojanRelaySpec is the input needed to render one relay's pieces. It maps
// 1:1 onto the TJ state records of the bash script:
//
//	domain, https_port, foreign_ip, foreign_port, bridge_port,
//	route (tunnel|direct), socks_port, cert, key
type TrojanRelaySpec struct {
	Name        string
	Domain      string
	HTTPSPort   int
	ForeignIP   string
	ForeignPort int
	BridgePort  int
	Route       string // tunnel | direct
	SocksPort   int
	CertPath    string
	KeyPath     string
}

// IsValidName enforces the same charset as the bash script (tr -cd 'A-Za-z0-9_-').
func IsValidTrojanName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// IsPrivateOrLoopback mirrors the bash is_private_ip + 127.0.0.1 check: such
// targets are dialled DIRECTLY (the hedioum egress refuses private targets).
func IsPrivateOrLoopback(ip string) bool {
	if strings.HasPrefix(ip, "127.") {
		return true
	}
	octets := strings.Split(ip, ".")
	if len(octets) != 4 {
		return false
	}
	o0, o1, o2 := atoiIP(octets[0]), atoiIP(octets[1]), atoiIP(octets[2])
	if o0 < 0 || o1 < 0 || o2 < 0 {
		return false
	}
	switch {
	case o0 == 10:
		return true
	case o0 == 172 && o1 >= 16 && o1 <= 31:
		return true
	case o0 == 192 && o1 == 168:
		return true
	case o0 == 100 && o1 >= 64 && o1 <= 127: // CGNAT / mesh ranges (EasyTier etc.)
		return true
	}
	return false
}

// atoiIP parses one IPv4 octet, -1 when invalid.
func atoiIP(s string) int {
	if len(s) == 0 || len(s) > 3 {
		return -1
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// ResolveTrojanRoute decides the routing mode the way the wizard does:
// private/loopback targets go direct, everything else rides the tunnel.
func ResolveTrojanRoute(foreignIP, requested string) string {
	if requested == "direct" || requested == "tunnel" {
		return requested
	}
	if IsPrivateOrLoopback(foreignIP) {
		return "direct"
	}
	return "tunnel"
}

// NextTrojanBridgePort returns the first free port >= base that no other
// relay uses. Pure function so it is unit-testable.
func NextTrojanBridgePort(used []int, base int) int {
	if base < TrojanBridgePortBase {
		base = TrojanBridgePortBase
	}
	taken := map[int]bool{}
	for _, p := range used {
		taken[p] = true
	}
	for p := base; p <= 65535; p++ {
		if !taken[p] {
			return p
		}
	}
	return 0
}

// BuildTrojanBridgeConfig renders the xray bridge JSON for all trojan
// relays: one dokodemo inbound per relay (127.0.0.1:bridge_port) and one
// SOCKS outbound per distinct hub port, with per-relay routing rules
// (direct for private/loopback or route=direct, tunnel otherwise) and a
// catch-all. Mirrors tj_build_bridge.
func BuildTrojanBridgeConfig(relays []TrojanRelaySpec, defaultSocksPort int) ([]byte, error) {
	if defaultSocksPort < 1 || defaultSocksPort > 65535 {
		defaultSocksPort = 40001
	}
	// distinct hub ports, sorted
	socksPorts := []int{}
	seen := map[int]bool{}
	for _, r := range relays {
		sp := r.SocksPort
		if sp < 1 {
			sp = defaultSocksPort
		}
		if !seen[sp] {
			seen[sp] = true
			socksPorts = append(socksPorts, sp)
		}
	}
	sortInts(socksPorts)

	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString("  \"log\": { \"loglevel\": \"warning\" },\n")

	// inbounds
	b.WriteString("  \"inbounds\": [")
	first := true
	for _, r := range relays {
		if !first {
			b.WriteString(",")
		}
		first = false
		fmt.Fprintf(&b, "\n    { \"tag\": \"tj-%s\", \"listen\": \"127.0.0.1\", \"port\": %d, "+
			"\"protocol\": \"dokodemo-door\", "+
			"\"settings\": { \"address\": \"%s\", \"port\": %d, \"network\": \"tcp\", \"followRedirect\": false }, "+
			"\"sniffing\": { \"enabled\": false } }",
			jsonEscape(r.Name), r.BridgePort, jsonEscape(r.ForeignIP), r.ForeignPort)
	}
	if !first {
		b.WriteString("\n  ")
	}
	b.WriteString("],\n")

	// outbounds: one socks per hub port + direct
	b.WriteString("  \"outbounds\": [")
	first = true
	for _, sp := range socksPorts {
		if !first {
			b.WriteString(",")
		}
		first = false
		fmt.Fprintf(&b, "\n    { \"tag\": \"tunnel-%d\", \"protocol\": \"socks\", "+
			"\"settings\": { \"servers\": [ { \"address\": \"127.0.0.1\", \"port\": %d } ] }, "+
			"\"streamSettings\": { \"sockopt\": { \"tcpKeepAliveIdle\": 30, \"tcpNoDelay\": true } } }", sp, sp)
	}
	if !first {
		b.WriteString(",")
	}
	b.WriteString("\n    { \"tag\": \"direct\", \"protocol\": \"freedom\", \"settings\": {} }\n  ],\n")

	// routing: one rule per relay inboundTag → its outbound, then catch-all
	b.WriteString("  \"routing\": { \"domainStrategy\": \"AsIs\", \"rules\": [")
	first = true
	for _, r := range relays {
		sp := r.SocksPort
		if sp < 1 {
			sp = defaultSocksPort
		}
		outTag := fmt.Sprintf("tunnel-%d", sp)
		if r.Route == "direct" || IsPrivateOrLoopback(r.ForeignIP) {
			outTag = "direct"
		}
		if !first {
			b.WriteString(",")
		}
		first = false
		fmt.Fprintf(&b, "\n    { \"type\": \"field\", \"inboundTag\": [\"tj-%s\"], \"outboundTag\": \"%s\" }",
			jsonEscape(r.Name), outTag)
	}
	if !first {
		b.WriteString(",")
	}
	b.WriteString("\n    { \"type\": \"field\", \"network\": \"tcp\", \"outboundTag\": \"direct\" }\n  ] },\n")

	b.WriteString("  \"policy\": { \"levels\": { \"0\": { \"handshake\": 8, \"connIdle\": 300, \"uplinkOnly\": 2, \"downlinkOnly\": 5 } } }\n")
	b.WriteString("}\n")
	return []byte(b.String()), nil
}

// TrojanIngressSpec is one foreign-side forwarder record — the hedioum-suite
// v3 ingress shape: any target host (not just loopback), a configurable
// listen address, and optional UDP relay.
type TrojanIngressSpec struct {
	Name       string
	ListenIP   string // "" = 0.0.0.0
	ListenPort int    // public tunnel port on the foreign node
	TargetHost string // node inbound address (127.0.0.1 or another box)
	TargetPort int    // node inbound port
	UDP        bool   // also forward UDP (QUIC/Hysteria style inbounds)
}

// BuildTrojanIngressConfig renders the foreign-side ingress xray config:
// one public dokodemo inbound per forwarder pointing at its node target —
// mirrors hedioum-suite build_ingress_config (udp → "tcp,udp" network).
func BuildTrojanIngressConfig(forwarders []TrojanIngressSpec) ([]byte, error) {
	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString("  \"log\": { \"loglevel\": \"warning\" },\n")
	b.WriteString("  \"inbounds\": [")
	first := true
	for _, f := range forwarders {
		if !first {
			b.WriteString(",")
		}
		first = false
		listenIP := f.ListenIP
		if listenIP == "" {
			listenIP = "0.0.0.0"
		}
		network := "tcp"
		if f.UDP {
			network = "tcp,udp"
		}
		fmt.Fprintf(&b, "\n    { \"tag\": \"ingress-%s\", \"listen\": \"%s\", \"port\": %d, "+
			"\"protocol\": \"dokodemo-door\", "+
			"\"settings\": { \"address\": \"%s\", \"port\": %d, \"network\": \"%s\", \"followRedirect\": false }, "+
			"\"sniffing\": { \"enabled\": false } }",
			jsonEscape(f.Name), jsonEscape(listenIP), f.ListenPort,
			jsonEscape(f.TargetHost), f.TargetPort, network)
	}
	if !first {
		b.WriteString("\n  ")
	}
	b.WriteString("],\n")
	b.WriteString("  \"outbounds\": [ { \"tag\": \"direct\", \"protocol\": \"freedom\", \"settings\": {}, " +
		"\"streamSettings\": { \"sockopt\": { \"tcpNoDelay\": true, \"tcpKeepAliveIdle\": 30 } } } ],\n")
	b.WriteString("  \"routing\": { \"domainStrategy\": \"AsIs\", \"rules\": " +
		"[ { \"type\": \"field\", \"network\": \"tcp,udp\", \"outboundTag\": \"direct\" } ] },\n")
	b.WriteString("  \"policy\": { \"levels\": { \"0\": { \"handshake\": 8, \"connIdle\": 300, \"uplinkOnly\": 2, \"downlinkOnly\": 5 } }, " +
		"\"system\": { \"statsInboundUplink\": false, \"statsInboundDownlink\": false } }\n")
	b.WriteString("}\n")
	return []byte(b.String()), nil
}

// TrojanBridgeUnitBody renders the systemd unit for the trojan bridge.
func TrojanBridgeUnitBody(xrayBin, confPath string) string {
	if xrayBin == "" {
		xrayBin = "/usr/local/bin/xray"
	}
	return fmt.Sprintf(`[Unit]
Description=PortGuard trojan bridge (dokodemo -> tunnel SOCKS)
After=network-online.target hedioum.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s run -config %s
Restart=always
RestartSec=3
LimitNOFILE=1000000
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
`, xrayBin, confPath)
}

// TrojanIngressUnitBody renders the systemd unit for the trojan ingress.
func TrojanIngressUnitBody(xrayBin, confPath string) string {
	if xrayBin == "" {
		xrayBin = "/usr/local/bin/xray"
	}
	return fmt.Sprintf(`[Unit]
Description=PortGuard trojan ingress (tunnel port -> node inbound)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s run -config %s
Restart=always
RestartSec=3
LimitNOFILE=1000000
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
`, xrayBin, confPath)
}

// ProbeThroughTunnel checks whether host:port answers THROUGH the local
// SOCKS hub — the relay_verify end-to-end check (curl exit codes 0/35/52/56/60
// all mean "the far side answered": 35=TLS handshake, 52=empty reply,
// 56=recv failure after connect, 60=cert verify — the port is alive either way).
func ProbeThroughTunnel(socksHost string, socksPort int, host string, port int) bool {
	if _, err := exec.LookPath("curl"); err != nil {
		return false
	}
	proxy := fmt.Sprintf("%s:%d", socksHost, socksPort)
	url := fmt.Sprintf("https://%s:%d/", host, port)
	_, err := exec.Command("curl", "-s", "-o", "/dev/null", "--max-time", "10", "-k",
		"--socks5-hostname", proxy, url).Output()
	code := exitCode(err)
	switch code {
	case 0, 35, 52, 56, 60:
		return true
	}
	url = fmt.Sprintf("http://%s:%d/", host, port)
	_, err = exec.Command("curl", "-s", "-o", "/dev/null", "--max-time", "10",
		"--socks5-hostname", proxy, url).Output()
	code = exitCode(err)
	return code == 0 || code == 52 || code == 56
}

// exitCode maps an exec error to the process exit status (0 when nil).
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

// RelayVerifyReport is the end-to-end verdict for one relay.
type RelayVerifyReport struct {
	Name         string `json:"name"`
	ListenUp     bool   `json:"listen_up"`
	HTTPSPortUp  bool   `json:"https_port_up,omitempty"`
	BridgeUp     bool   `json:"bridge_up,omitempty"`
	CertDaysLeft *int   `json:"cert_days_left,omitempty"`
	HubAlive     bool   `json:"hub_alive"`
	TargetOK     bool   `json:"target_ok"`
	TargetProbe  string `json:"target_probe"` // "through the tunnel" | "direct"
	Note         string `json:"note,omitempty"`
}

func sortInts(v []int) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}

// jsonEscape escapes a value that is interpolated INSIDE an existing JSON
// string literal in the template — it only escapes the special characters
// and does not add quotes (quotes come from the template).
func jsonEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
