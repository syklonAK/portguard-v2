package scanner

import (
	"testing"
)

func TestParseSSLineTCPv4(t *testing.T) {
	line := `LISTEN 0      4096               *:62050            *:*    users:(("main",pid=9248,fd=4))`
	// ss -H output starts with Netid; prepend it
	line = "tcp " + line
	e, ok := parseSSLine(line)
	if !ok {
		t.Fatalf("failed to parse: %s", line)
	}
	if e.Proto != "tcp" || e.Port != 62050 || e.ListenIP != "0.0.0.0" {
		t.Errorf("bad entry: %+v", e)
	}
	if e.PID != 9248 || e.Process != "main" {
		t.Errorf("bad process info: %+v", e)
	}
}

func TestParseSSLineTCPv6(t *testing.T) {
	line := `tcp LISTEN 0 128 [::]:22 [::]:* users:(("sshd",pid=1054,fd=3),("systemd",pid=1,fd=167))`
	e, ok := parseSSLine(line)
	if !ok {
		t.Fatalf("failed to parse: %s", line)
	}
	if e.Port != 22 || e.ListenIP != "::" {
		t.Errorf("bad entry: %+v", e)
	}
	if e.Process != "sshd, systemd" {
		t.Errorf("expected merged processes, got %q", e.Process)
	}
}

func TestParseSSLineUDP(t *testing.T) {
	line := `udp UNCONN 0 0 0.0.0.0:5353 0.0.0.0:* users:(("dnsmasq",pid=999,fd=5))`
	e, ok := parseSSLine(line)
	if !ok {
		t.Fatalf("failed to parse: %s", line)
	}
	if e.Proto != "udp" || e.Port != 5353 || e.Process != "dnsmasq" {
		t.Errorf("bad entry: %+v", e)
	}
}

func TestParseSSLineLoopback(t *testing.T) {
	line := `tcp LISTEN 0 4096 127.0.0.1:17878 0.0.0.0:* users:(("xray",pid=153292,fd=12))`
	e, ok := parseSSLine(line)
	if !ok {
		t.Fatalf("failed to parse")
	}
	if e.ListenIP != "127.0.0.1" || e.Port != 17878 || e.Classification != "" {
		t.Errorf("bad entry: %+v", e)
	}
	if got := classify(e.Process); got != "xray-proxy" {
		t.Errorf("classify(xray) = %q", got)
	}
}

func TestClassify(t *testing.T) {
	cases := map[string]string{
		"nginx":    "web-server",
		"haproxy":  "load-balancer",
		"sshd":     "ssh",
		"xray":     "xray-proxy",
		"main":     "pasarguard-node",
		"unknown1": "other",
		"":         "unknown",
	}
	for proc, want := range cases {
		if got := classify(proc); got != want {
			t.Errorf("classify(%q) = %q, want %q", proc, got, want)
		}
	}
}

func TestParseSSLineNonListen(t *testing.T) {
	if _, ok := parseSSLine("tcp ESTAB 0 0 1.2.3.4:22 5.6.7.8:9999"); ok {
		t.Error("non-LISTEN tcp line must be skipped")
	}
}

// TestSocketStatePredicates documents the /proc/net state model used by the
// scanner: TCP listeners are state 0A; UDP sockets report TCP-like states
// where 07 marks bound (listening) sockets and 01 marks connect()ed sockets
// (excluded: those are usually ephemeral client sockets, not services).
func TestSocketStatePredicates(t *testing.T) {
	tcpCases := []struct {
		state string
		want  bool
	}{
		{"0A", true},  // TCP_LISTEN
		{"01", false}, // ESTABLISHED
		{"08", false}, // CLOSE_WAIT
		{"", false},
		{"FF", false},
	}
	for _, tc := range tcpCases {
		if got := isTCPListening(tc.state); got != tc.want {
			t.Errorf("isTCPListening(%q) = %v, want %v", tc.state, got, tc.want)
		}
	}

	udpCases := []struct {
		state  string
		listen bool // expected discovery outcome
	}{
		{"07", true},  // unconnected/bound: every UDP listener
		{"01", false}, // connected UDP client socket (ephemeral)
		{"", false},
		{"FF", false},
	}
	for _, tc := range udpCases {
		if got := isUDPListening(tc.state); got != tc.listen {
			t.Errorf("isUDPListening(%q) = %v, want %v", tc.state, got, tc.listen)
		}
	}
}
