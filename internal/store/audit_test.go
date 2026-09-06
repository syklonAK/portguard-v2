package store

// Regression tests from the v2.11 audit: incremental Replace* syncs,
// pagination helpers and the public-node token redaction.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// testStore opens a throwaway store, mirroring the existing test style.
func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "pg.db"))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestReplacePortsSync(t *testing.T) {
	st := testStore(t)
	defer st.Close()

	mk := func(port int, proc string) PortEntry {
		return PortEntry{Port: port, Proto: "tcp", ListenIP: "0.0.0.0", Process: proc, PID: 100 + port, User: "root", Classification: "other"}
	}

	// first scan: two ports
	if err := st.ReplacePorts([]PortEntry{mk(80, "nginx"), mk(22, "sshd")}); err != nil {
		t.Fatal(err)
	}
	ports, _ := st.ListPorts()
	if len(ports) != 2 {
		t.Fatalf("want 2 ports after first scan, got %d", len(ports))
	}

	// repeated identical scan: state unchanged, no duplicate rows
	if err := st.ReplacePorts([]PortEntry{mk(80, "nginx"), mk(22, "sshd")}); err != nil {
		t.Fatal(err)
	}
	ports, _ = st.ListPorts()
	if len(ports) != 2 {
		t.Fatalf("repeated identical scan must not duplicate rows, got %d", len(ports))
	}

	// changed row (same key, different process) + removed port + new port
	if err := st.ReplacePorts([]PortEntry{mk(80, "haproxy"), mk(443, "nginx")}); err != nil {
		t.Fatal(err)
	}
	ports, _ = st.ListPorts()
	if len(ports) != 2 {
		t.Fatalf("want 2 ports after churn, got %d", len(ports))
	}
	byPort := map[int]PortEntry{}
	for _, p := range ports {
		byPort[p.Port] = p
	}
	if byPort[80].Process != "haproxy" {
		t.Fatalf("port 80 process should update to haproxy, got %q", byPort[80].Process)
	}
	if _, ok := byPort[22]; ok {
		t.Fatal("port 22 should have been removed")
	}
	if _, ok := byPort[443]; !ok {
		t.Fatal("port 443 should have been added")
	}

	// empty scan clears the table
	if err := st.ReplacePorts(nil); err != nil {
		t.Fatal(err)
	}
	ports, _ = st.ListPorts()
	if len(ports) != 0 {
		t.Fatalf("empty scan should clear ports, got %d", len(ports))
	}

	// SO_REUSEPORT duplicates survive round-trips
	if err := st.ReplacePorts([]PortEntry{mk(8080, "a"), mk(8080, "b")}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplacePorts([]PortEntry{mk(8080, "a"), mk(8080, "b")}); err != nil {
		t.Fatal(err)
	}
	ports, _ = st.ListPorts()
	if len(ports) != 2 {
		t.Fatalf("duplicate bind tuples must both persist, got %d rows", len(ports))
	}
}

func TestReplaceConnectionsSync(t *testing.T) {
	st := testStore(t)
	defer st.Close()

	mk := func(srcP, dstP int) ConnEntry {
		return ConnEntry{SrcIP: "10.0.0.1", SrcPort: srcP, DstIP: "10.0.0.2", DstPort: dstP, Process: "p", PID: 1, UID: 0, State: "ESTABLISHED"}
	}

	if err := st.ReplaceConnections([]ConnEntry{mk(1, 80), mk(2, 80)}, 1000); err != nil {
		t.Fatal(err)
	}
	conns, _ := st.ListConnections()
	if len(conns) != 2 {
		t.Fatalf("want 2 connections, got %d", len(conns))
	}

	// repeated scan keeps first_seen and does not duplicate
	if err := st.ReplaceConnections([]ConnEntry{mk(1, 80), mk(2, 80)}, 1100); err != nil {
		t.Fatal(err)
	}
	conns, _ = st.ListConnections()
	if len(conns) != 2 {
		t.Fatalf("repeated scan must not duplicate, got %d", len(conns))
	}
	for _, c := range conns {
		if c.FirstSeen != 1000 {
			t.Fatalf("first_seen must survive refreshes, got %d", c.FirstSeen)
		}
		if c.LastSeen != 1100 {
			t.Fatalf("last_seen must advance, got %d", c.LastSeen)
		}
	}

	// vanished tuple removed, new tuple inserted
	if err := st.ReplaceConnections([]ConnEntry{mk(1, 80), mk(3, 443)}, 1200); err != nil {
		t.Fatal(err)
	}
	conns, _ = st.ListConnections()
	if len(conns) != 2 {
		t.Fatalf("want 2 connections after churn, got %d", len(conns))
	}
	found := map[int]bool{}
	for _, c := range conns {
		found[c.SrcPort] = true
	}
	if found[2] || !found[3] || !found[1] {
		t.Fatalf("unexpected connection set: %v", found)
	}
}

func TestPagination(t *testing.T) {
	st := testStore(t)
	defer st.Close()

	for i := 1; i <= 5; i++ {
		m := Mapping{Name: map[int]string{1: "a", 2: "b", 3: "c", 4: "d", 5: "e"}[i], Engine: "nginx", Protocol: "tcp", ListenPort: 10000 + i}
		if _, err := st.CreateMapping(&m); err != nil {
			t.Fatal(err)
		}
	}

	all, err := st.ListMappings()
	if err != nil || len(all) != 5 {
		t.Fatalf("ListMappings should return all 5, got %d (err %v)", len(all), err)
	}

	page1, err := st.ListMappingsPaged(2, 0)
	if err != nil || len(page1) != 2 {
		t.Fatalf("first page should have 2 rows, got %d (err %v)", len(page1), err)
	}
	mid, _ := st.ListMappingsPaged(2, 2)
	if len(mid) != 2 {
		t.Fatalf("middle page should have 2 rows, got %d", len(mid))
	}
	last, _ := st.ListMappingsPaged(2, 4)
	if len(last) != 1 {
		t.Fatalf("last page should have 1 row, got %d", len(last))
	}
	empty, _ := st.ListMappingsPaged(2, 100)
	if len(empty) != 0 {
		t.Fatalf("past-end page should be empty, got %d", len(empty))
	}
	big, _ := st.ListMappingsPaged(500, 0)
	if len(big) != 5 {
		t.Fatalf("max limit returns all rows, got %d", len(big))
	}
	// negative limit = legacy "all" behavior
	legacy, _ := st.ListMappingsPaged(-1, 0)
	if len(legacy) != 5 {
		t.Fatalf("negative limit must return all, got %d", len(legacy))
	}
}

// TestServerNodeTokenNeverSerialized: the shared node secret must never
// appear in any public JSON, even if the struct is marshalled directly.
func TestServerNodeTokenNeverSerialized(t *testing.T) {
	st := testStore(t)
	defer st.Close()

	n := ServerNode{Name: "secret-node", Host: "10.0.0.9", Port: 8080, APIToken: "super-secret-token-value", Enabled: true}
	if _, err := st.CreateServerNode(&n); err != nil {
		t.Fatal(err)
	}

	pub, err := st.ListServerNodesPublic()
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range pub {
		blob, _ := json.Marshal(node)
		if strings.Contains(string(blob), "super-secret-token-value") {
			t.Fatalf("APIToken leaked in public node JSON: %s", blob)
		}
	}

	// and even a direct marshal of the struct is protected by json:"-"
	blob, _ := json.Marshal(n)
	if strings.Contains(string(blob), "super-secret-token-value") {
		t.Fatal("APIToken must carry json:\"-\" so direct marshals cannot leak it")
	}
}
