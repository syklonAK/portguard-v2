package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Code in this file was split out of store.go verbatim (v2.11 audit):
// same package, same API — only the file boundaries changed.

func migrate(db *sql.DB) error {
	// target_health gained host/port columns in v1.0.1; it is a disposable cache, so
	// recreate it if the old shape is present.
	var thCols string
	if err := db.QueryRow(`SELECT group_concat(name) FROM pragma_table_info('target_health')`).Scan(&thCols); err == nil {
		if !strings.Contains(thCols, "host") {
			_, _ = db.Exec(`DROP TABLE target_health`)
		}
	}
	// v2.0.0 mappings gained balance/path_prefix/access_rules columns; v2.1.0 added
	// path_routes; v2.2.0 added host_header/decoy/decoy_html; v2.7.0 added admins.role.
	// v2.11: server_nodes gained conn_mode and uid.
	// ALTER for older DBs.
	for _, col := range []struct{ table, name, ddl string }{
		{"mappings", "balance", `ALTER TABLE mappings ADD COLUMN balance TEXT NOT NULL DEFAULT ''`},
		{"mappings", "path_prefix", `ALTER TABLE mappings ADD COLUMN path_prefix TEXT NOT NULL DEFAULT ''`},
		{"mappings", "access_rules", `ALTER TABLE mappings ADD COLUMN access_rules TEXT NOT NULL DEFAULT '[]'`},
		{"mappings", "path_routes", `ALTER TABLE mappings ADD COLUMN path_routes TEXT NOT NULL DEFAULT '[]'`},
		{"mappings", "host_header", `ALTER TABLE mappings ADD COLUMN host_header TEXT NOT NULL DEFAULT ''`},
		{"mappings", "decoy", `ALTER TABLE mappings ADD COLUMN decoy TEXT NOT NULL DEFAULT ''`},
		{"mappings", "decoy_html", `ALTER TABLE mappings ADD COLUMN decoy_html TEXT NOT NULL DEFAULT ''`},
		{"admins", "role", `ALTER TABLE admins ADD COLUMN role TEXT NOT NULL DEFAULT 'owner'`},
		{"mappings", "service_id", `ALTER TABLE mappings ADD COLUMN service_id INTEGER`},
		{"mappings", "routes", `ALTER TABLE mappings ADD COLUMN routes TEXT NOT NULL DEFAULT '[]'`},
		{"server_nodes", "conn_mode", `ALTER TABLE server_nodes ADD COLUMN conn_mode TEXT NOT NULL DEFAULT 'direct'`},
		{"server_nodes", "uid", `ALTER TABLE server_nodes ADD COLUMN uid TEXT NOT NULL DEFAULT ''`},
		// v2.13 trojan ingresses gained listen_ip/target/udp (the old shape had
		// only listen_port+node_port). The table is disposable (no history), so
		// migrate by widening; rows keep working since target defaults loopback.
		{"trojan_ingresses", "listen_ip", `ALTER TABLE trojan_ingresses ADD COLUMN listen_ip TEXT NOT NULL DEFAULT '0.0.0.0'`},
		{"trojan_ingresses", "target_host", `ALTER TABLE trojan_ingresses ADD COLUMN target_host TEXT NOT NULL DEFAULT '127.0.0.1'`},
		{"trojan_ingresses", "target_port", `ALTER TABLE trojan_ingresses ADD COLUMN target_port INTEGER NOT NULL DEFAULT 10000`},
		{"trojan_ingresses", "udp", `ALTER TABLE trojan_ingresses ADD COLUMN udp INTEGER NOT NULL DEFAULT 0`},
	} {
		// pragma functions cannot be parameterized reliably across drivers —
		// the table name is from our fixed list above, never user input
		var cols string
		if err := db.QueryRow(`SELECT group_concat(name) FROM pragma_table_info('` + col.table + `')`).Scan(&cols); err == nil {
			if !hasColumn(cols, col.name) {
				_, _ = db.Exec(col.ddl)
			}
		}
	}
	schema := `
CREATE TABLE IF NOT EXISTS admins (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	role TEXT NOT NULL DEFAULT 'owner',
	created_at INTEGER NOT NULL,
	last_login_at INTEGER
);CREATE TABLE IF NOT EXISTS settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS ssl_certs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	type TEXT NOT NULL DEFAULT 'manual',
	cert_pem TEXT NOT NULL DEFAULT '',
	key_pem TEXT NOT NULL DEFAULT '',
	domains TEXT NOT NULL DEFAULT '[]',
	expires_at INTEGER,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS mappings (
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
		balance TEXT NOT NULL DEFAULT '',
	path_prefix TEXT NOT NULL DEFAULT '',
	access_rules TEXT NOT NULL DEFAULT '[]',
	extra_headers TEXT NOT NULL DEFAULT '{}',
	path_routes TEXT NOT NULL DEFAULT '[]',
	host_header TEXT NOT NULL DEFAULT '',
	decoy TEXT NOT NULL DEFAULT '',
	decoy_html TEXT NOT NULL DEFAULT '',
	routes TEXT NOT NULL DEFAULT '[]',
	service_id INTEGER,
	notes TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS tunnel_relays (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	mode TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1,
	target_host TEXT NOT NULL,
	target_port INTEGER NOT NULL,
	listen_ip TEXT NOT NULL DEFAULT '0.0.0.0',
	listen_port INTEGER NOT NULL DEFAULT 0,
	bridge_port INTEGER NOT NULL DEFAULT 0,
	udp INTEGER NOT NULL DEFAULT 0,
	host_header TEXT NOT NULL DEFAULT '',
	domain TEXT NOT NULL DEFAULT '',
	ssl_cert_id INTEGER,
	notes TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS trojan_relays (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	domain TEXT NOT NULL DEFAULT '',
	https_port INTEGER NOT NULL DEFAULT 443,
	foreign_ip TEXT NOT NULL DEFAULT '',
	foreign_port INTEGER NOT NULL DEFAULT 10000,
	bridge_port INTEGER NOT NULL DEFAULT 0,
	route TEXT NOT NULL DEFAULT '',
	socks_port INTEGER NOT NULL DEFAULT 40001,
	enabled INTEGER NOT NULL DEFAULT 1,
	notes TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS trojan_ingresses (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	listen_ip TEXT NOT NULL DEFAULT '0.0.0.0',
	listen_port INTEGER NOT NULL,
	target_host TEXT NOT NULL DEFAULT '127.0.0.1',
	target_port INTEGER NOT NULL DEFAULT 10000,
	udp INTEGER NOT NULL DEFAULT 0,
	enabled INTEGER NOT NULL DEFAULT 1,
	notes TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS connections (
	src_ip TEXT NOT NULL,
	src_port INTEGER NOT NULL,
	dst_ip TEXT NOT NULL,
	dst_port INTEGER NOT NULL,
	process TEXT NOT NULL DEFAULT '',
	pid INTEGER NOT NULL DEFAULT 0,
	uid INTEGER NOT NULL DEFAULT 0,
	state TEXT NOT NULL DEFAULT '',
	managed INTEGER NOT NULL DEFAULT 0,
	inner INTEGER NOT NULL DEFAULT 0,
	self INTEGER NOT NULL DEFAULT 0,
	first_seen INTEGER NOT NULL,
	last_seen INTEGER NOT NULL,
	PRIMARY KEY (src_ip, src_port, dst_ip, dst_port)
);
CREATE TABLE IF NOT EXISTS server_nodes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	host TEXT NOT NULL,
	port INTEGER NOT NULL DEFAULT 8080,
	api_token TEXT NOT NULL DEFAULT '',
	role TEXT NOT NULL DEFAULT 'generic',
	enabled INTEGER NOT NULL DEFAULT 1,
	notes TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'unknown',
	last_seen INTEGER,
	conn_mode TEXT NOT NULL DEFAULT 'direct',
	uid TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS rate_profiles (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	download_bps INTEGER NOT NULL DEFAULT 0,
	upload_bps INTEGER NOT NULL DEFAULT 0,
	enabled INTEGER NOT NULL DEFAULT 1,
	notes TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS pasarguard_users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	uuid TEXT NOT NULL UNIQUE,
	username TEXT NOT NULL,
	node_id INTEGER,
	enabled INTEGER NOT NULL DEFAULT 1,
	expired INTEGER NOT NULL DEFAULT 0,
	last_ip TEXT NOT NULL DEFAULT '',
	synced_at INTEGER NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS rate_limit_policies (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	uuid TEXT NOT NULL,
	node_id INTEGER NOT NULL,
	profile_id INTEGER,
	download_bps INTEGER NOT NULL DEFAULT 0,
	upload_bps INTEGER NOT NULL DEFAULT 0,
	custom INTEGER NOT NULL DEFAULT 0,
	enabled INTEGER NOT NULL DEFAULT 1,
	status TEXT NOT NULL DEFAULT 'pending',
	last_error TEXT NOT NULL DEFAULT '',
	last_pushed_version INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	UNIQUE(uuid, node_id)
);
CREATE INDEX IF NOT EXISTS idx_rlp_uuid ON rate_limit_policies(uuid);
CREATE INDEX IF NOT EXISTS idx_rlp_node ON rate_limit_policies(node_id);
CREATE INDEX IF NOT EXISTS idx_rlp_status ON rate_limit_policies(status);
CREATE INDEX IF NOT EXISTS idx_pgu_uuid ON pasarguard_users(uuid);
CREATE TABLE IF NOT EXISTS config_versions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	version INTEGER NOT NULL UNIQUE,
	author TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	mappings_json TEXT NOT NULL DEFAULT '[]',
	relays_json TEXT NOT NULL DEFAULT '[]',
	node_count INTEGER NOT NULL DEFAULT 0,
	deploy_result TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS alerts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	severity TEXT NOT NULL DEFAULT 'info',
	category TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL,
	detail TEXT NOT NULL DEFAULT '',
	target TEXT NOT NULL DEFAULT '',
	dedup_key TEXT NOT NULL DEFAULT '',
	acknowledged INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_alerts_created ON alerts(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_alerts_dedup ON alerts(dedup_key);
CREATE TABLE IF NOT EXISTS node_metrics (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	node_id INTEGER NOT NULL DEFAULT 0,
	cpu_percent REAL NOT NULL DEFAULT 0,
	mem_percent REAL NOT NULL DEFAULT 0,
	disk_percent REAL NOT NULL DEFAULT 0,
	rx_bytes INTEGER NOT NULL DEFAULT 0,
	tx_bytes INTEGER NOT NULL DEFAULT 0,
	rx_bps INTEGER NOT NULL DEFAULT 0,
	tx_bps INTEGER NOT NULL DEFAULT 0,
	conns INTEGER NOT NULL DEFAULT 0,
	ts INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_metrics_node_ts ON node_metrics(node_id, ts);
CREATE TABLE IF NOT EXISTS services (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	description TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1,
	notes TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_metrics_ts ON node_metrics(ts);CREATE TABLE IF NOT EXISTS ports (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	port INTEGER NOT NULL,
	proto TEXT NOT NULL,
	listen_ip TEXT NOT NULL,
	process TEXT NOT NULL,
	pid INTEGER NOT NULL,
	user_name TEXT NOT NULL,
	classification TEXT NOT NULL,
	managed INTEGER NOT NULL DEFAULT 0,
	self INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS target_health (
	mapping_id INTEGER NOT NULL,
	target_index INTEGER NOT NULL,
	host TEXT NOT NULL DEFAULT '',
	port INTEGER NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT 'unknown',
	latency_ms REAL NOT NULL DEFAULT 0,
	fail_count INTEGER NOT NULL DEFAULT 0,
	last_check_at INTEGER,
	PRIMARY KEY (mapping_id, target_index)
);
CREATE TABLE IF NOT EXISTS audit_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	actor TEXT NOT NULL,
	action TEXT NOT NULL,
	detail TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'ok',
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ports_port ON ports(port);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_conns_src ON connections(src_ip);
CREATE INDEX IF NOT EXISTS idx_conns_dst ON connections(dst_port);
CREATE INDEX IF NOT EXISTS idx_health_mapping ON target_health(mapping_id, target_index);
`
	_, err := db.Exec(schema)
	return err
}
func (s *Store) ListCertsPaged(limit, offset int) ([]Cert, error) {
	rows, err := s.DB.Query(`SELECT id, name, type, cert_pem, key_pem, domains, expires_at, created_at FROM ssl_certs ORDER BY id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Cert
	for rows.Next() {
		var c Cert
		var domains string
		var exp sql.NullInt64
		var createdTS int64
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &c.CertPEM, &c.KeyPEM, &domains, &exp, &createdTS); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(domains), &c.Domains)
		if exp.Valid {
			t := time.Unix(exp.Int64, 0)
			c.ExpiresAt = &t
		}
		c.CreatedAt = time.Unix(createdTS, 0)
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) ListCerts() ([]Cert, error) {
	return s.ListCertsPaged(-1, 0)
}
func (s *Store) GetCert(id int64) (Cert, error) {
	var c Cert
	var domains string
	var exp sql.NullInt64
	var createdTS int64
	err := s.DB.QueryRow(`SELECT id, name, type, cert_pem, key_pem, domains, expires_at, created_at FROM ssl_certs WHERE id=?`, id).
		Scan(&c.ID, &c.Name, &c.Type, &c.CertPEM, &c.KeyPEM, &domains, &exp, &createdTS)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	_ = json.Unmarshal([]byte(domains), &c.Domains)
	if exp.Valid {
		t := time.Unix(exp.Int64, 0)
		c.ExpiresAt = &t
	}
	c.CreatedAt = time.Unix(createdTS, 0)
	return c, nil
}
func (s *Store) CreateCert(c *Cert) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO ssl_certs(name,type,cert_pem,key_pem,domains,expires_at,created_at) VALUES(?,?,?,?,?,?,?)`,
		c.Name, c.Type, c.CertPEM, c.KeyPEM, mustJSON(c.Domains), nullTime(c.ExpiresAt), nowTS())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// refreshed so the Certs page shows the new validity window.
func (s *Store) UpdateCertPEM(id int64, certPEM, keyPEM string, expiresAt *time.Time) error {
	res, err := s.DB.Exec(`UPDATE ssl_certs SET cert_pem=?, key_pem=?, expires_at=? WHERE id=?`,
		certPEM, keyPEM, nullTime(expiresAt), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) DeleteCert(id int64) error {
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM mappings WHERE ssl_cert_id=?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("certificate is used by %d mapping(s)", n)
	}
	_, err := s.DB.Exec(`DELETE FROM ssl_certs WHERE id=?`, id)
	return err
}
