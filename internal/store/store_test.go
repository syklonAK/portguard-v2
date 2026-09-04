package store

import (
	"path/filepath"
	"testing"
	"time"
)

// TestMigrationFromV1Schema verifies that a v1 database (no balance/path_prefix/
// access_rules columns) is upgraded in place and keeps its rows.
func TestMigrationFromV1Schema(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "pg.db")

	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	st.DB.Close()

	// rebuild the pre-2.0 mappings table shape
	st2, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.DB.Close()
	if _, err := st2.DB.Exec(`DROP TABLE mappings`); err != nil {
		t.Fatal(err)
	}
	if _, err := st2.DB.Exec(`CREATE TABLE mappings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		engine TEXT NOT NULL,
		protocol TEXT NOT NULL,
		listen_ip TEXT NOT NULL DEFAULT '0.0.0.0',
		listen_port INTEGER NOT NULL,
		server_names TEXT NOT NULL DEFAULT '[]',
		ssl_cert_id INTEGER,
		redirect_to TEXT NOT NULL DEFAULT '',
		websocket INTEGER NOT NULL DEFAULT 0,
		http2 INTEGER NOT NULL DEFAULT 1,
		targets TEXT NOT NULL DEFAULT '[]',
		extra_headers TEXT NOT NULL DEFAULT '{}',
		notes TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Unix()
	if _, err := st2.DB.Exec(`INSERT INTO mappings(name,enabled,engine,protocol,listen_port,created_at,updated_at)
		VALUES('legacy',1,'nginx','http',8081,?,?)`, ts, ts); err != nil {
		t.Fatal(err)
	}
	st2.DB.Close()

	// reopening must run the ALTER TABLE migrations
	st3, err := Open(dbPath)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	defer st3.DB.Close()
	ms, err := st3.ListMappings()
	if err != nil {
		t.Fatalf("scan after migration failed: %v", err)
	}
	if len(ms) != 1 || ms[0].Name != "legacy" {
		t.Fatalf("legacy row lost: %+v", ms)
	}

	// new columns must be writable
	m := ms[0]
	m.Balance = "least_conn"
	m.PathPrefix = "/api"
	m.AccessRules = []ACLRule{{Action: "allow", Value: "10.0.0.0/8"}}
	if err := st3.UpdateMapping(&m); err != nil {
		t.Fatalf("update with new fields failed: %v", err)
	}
	got, err := st3.GetMapping(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Balance != "least_conn" || got.PathPrefix != "/api" || len(got.AccessRules) != 1 {
		t.Fatalf("roundtrip broken: %+v", got)
	}
}

func TestMappingRoundtripDefaults(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "pg.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.DB.Close()
	m := Mapping{
		Name: "web", Enabled: true, Engine: "nginx", Protocol: "http",
		ListenIP: "0.0.0.0", ListenPort: 8081, ServerNames: []string{"a.com"},
		Balance: "ip_hash", PathPrefix: "/app",
		AccessRules: []ACLRule{{Action: "deny", Value: "192.0.2.1"}},
		Targets:     []Target{{Host: "127.0.0.1", Port: 3000}},
	}
	id, err := st.CreateMapping(&m)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetMapping(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Balance != "ip_hash" || got.PathPrefix != "/app" || len(got.AccessRules) != 1 || got.AccessRules[0].Value != "192.0.2.1" {
		t.Fatalf("roundtrip broken: %+v", got)
	}
}
