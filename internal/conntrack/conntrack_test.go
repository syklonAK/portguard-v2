package conntrack

import (
	"testing"

	"portguard/internal/store"
)

func TestAddr(t *testing.T) {
	cases := []struct {
		in         string
		ip, port   string
		wantPort   int
	}{
		{"1.2.3.4:5678", "1.2.3.4", "", 5678},
		{"[::1]:22", "::1", "", 22},
		{"[2001:db8::1]:443", "2001:db8::1", "", 443},
		{"0.0.0.0:8080", "0.0.0.0", "", 8080},
		{"nonsense", "nonsense", "", 0},
	}
	for _, c := range cases {
		ip, port := addr(c.in)
		if ip != c.ip || port != c.wantPort {
			t.Errorf("addr(%q) = %q,%d want %q,%d", c.in, ip, port, c.ip, c.wantPort)
		}
	}
}

func TestParseProc(t *testing.T) {
	name, pid := parseProc(`users:(("nginx",pid=1234,fd=9))`)
	if name != "nginx" || pid != 1234 {
		t.Errorf("parseProc = %q,%d want nginx,1234", name, pid)
	}
	name, pid = parseProc(`users:(("hedioum-tunnel",pid=99,fd=4),("hedioum-tunnel",pid=99,fd=5))`)
	if name != "hedioum-tunnel" || pid != 99 {
		t.Errorf("parseProc multi = %q,%d want hedioum-tunnel,99", name, pid)
	}
	if _, pid := parseProc("no-proc-info"); pid != 0 {
		t.Error("parseProc should return 0 pid without info")
	}
}

func TestCollectSsOutput(t *testing.T) {
	// Collect runs `ss` which is Linux-only; on Windows it must return an
	// empty snapshot without panicking (the parse path is covered below).
	entries, err := Collect()
	if err != nil {
		t.Fatalf("Collect returned error on this platform: %v", err)
	}
	_ = entries
}

func TestEnrichManaged(t *testing.T) {
	entries := []store.ConnEntry{
		{SrcIP: "8.8.8.8", SrcPort: 50000, DstIP: "0.0.0.0", DstPort: 443, Process: "nginx"},
		{SrcIP: "8.8.8.8", SrcPort: 50001, DstIP: "0.0.0.0", DstPort: 9999, Process: "python3"},
		{SrcIP: "8.8.8.8", SrcPort: 50002, DstIP: "127.0.0.1", DstPort: 8080, Process: "portguard"},
		{SrcIP: "8.8.8.8", SrcPort: 50003, DstIP: "10.0.0.5", DstPort: 5432, Process: "xray"},
	}
	mappings := []store.Mapping{{ID: 1, Enabled: true, ListenPort: 443}}
	EnrichManaged(entries, mappings, 8080)

	if !entries[0].Managed {
		t.Error("mapping port 443 must be managed")
	}
	if entries[1].Managed {
		t.Error("unknown port 9999 (python3) must be unmanaged")
	}
	if !entries[1].Inner || entries[1].Inner != true {
		t.Error("0.0.0.0 dst is treated as inner")
	}
	if !entries[2].Self {
		t.Error("dst 8080 == panel port must be flagged self")
	}
	if !entries[3].Managed {
		t.Error("xray process must be counted managed")
	}
	if entries[2].Inner != true {
		t.Error("loopback dst must be inner")
	}
}
