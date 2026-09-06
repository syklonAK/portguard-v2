package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	DB *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// modernc/sqlite is safest with a single writer connection for our write patterns
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{DB: db}, nil
}

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

func nowTS() int64 { return time.Now().Unix() }

// hasColumn reports whether a group_concat'd column list contains name as a
// whole entry (avoids "routes" matching "path_routes").
func hasColumn(concat, name string) bool {
	for _, c := range strings.Split(concat, ",") {
		if strings.TrimSpace(c) == name {
			return true
		}
	}
	return false
}

// ---- settings ----

func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.DB.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.DB.Exec(`INSERT INTO settings(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// Secret returns a persisted random secret, generating it on first use.
func (s *Store) Secret(key string) (string, error) {
	v, err := s.GetSetting(key)
	if err == nil && v != "" {
		return v, nil
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return "", err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	v = hex.EncodeToString(b)
	if err := s.SetSetting(key, v); err != nil {
		return "", err
	}
	return v, nil
}

// ---- admins ----

func (s *Store) AdminCount() (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM admins`).Scan(&n)
	return n, err
}

func (s *Store) CreateAdmin(username, hash string) (int64, error) {
	return s.CreateAdminRole(username, hash, "owner")
}

func (s *Store) CreateAdminRole(username, hash, role string) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO admins(username,password_hash,role,created_at) VALUES(?,?,?,?)`,
		username, hash, role, nowTS())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) GetAdminByUsername(username string) (Admin, error) {
	var a Admin
	var createdTS int64
	var lastLogin sql.NullInt64
	err := s.DB.QueryRow(`SELECT id, username, password_hash, role, created_at, last_login_at FROM admins WHERE username=?`, username).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &a.Role, &createdTS, &lastLogin)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.CreatedAt = time.Unix(createdTS, 0)
	if lastLogin.Valid {
		t := time.Unix(lastLogin.Int64, 0)
		a.LastLoginAt = &t
	}
	return a, nil
}

func (s *Store) GetAdminByID(id int64) (Admin, error) {
	var a Admin
	var createdTS int64
	var lastLogin sql.NullInt64
	err := s.DB.QueryRow(`SELECT id, username, password_hash, role, created_at, last_login_at FROM admins WHERE id=?`, id).
		Scan(&a.ID, &a.Username, &a.PasswordHash, &a.Role, &createdTS, &lastLogin)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.CreatedAt = time.Unix(createdTS, 0)
	if lastLogin.Valid {
		t := time.Unix(lastLogin.Int64, 0)
		a.LastLoginAt = &t
	}
	return a, nil
}

func (s *Store) SetAdminPassword(id int64, hash string) error {
	_, err := s.DB.Exec(`UPDATE admins SET password_hash=? WHERE id=?`, hash, id)
	return err
}

// SetAdminRole changes a user's RBAC role.
func (s *Store) SetAdminRole(id int64, role string) error {
	_, err := s.DB.Exec(`UPDATE admins SET role=? WHERE id=?`, role, id)
	return err
}

func (s *Store) DeleteAdmin(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM admins WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListAdmins returns users without password hashes.
func (s *Store) ListAdmins() ([]Admin, error) {
	rows, err := s.DB.Query(`SELECT id, username, role, created_at, COALESCE(last_login_at, 0) FROM admins ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Admin
	for rows.Next() {
		var a Admin
		var c, ll int64
		if err := rows.Scan(&a.ID, &a.Username, &a.Role, &c, &ll); err != nil {
			return nil, err
		}
		a.CreatedAt = time.Unix(c, 0)
		if ll > 0 {
			t := time.Unix(ll, 0)
			a.LastLoginAt = &t
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) TouchAdminLogin(id int64) {
	_, _ = s.DB.Exec(`UPDATE admins SET last_login_at=? WHERE id=?`, nowTS(), id)
}

// ---- mappings ----

func (s *Store) ListMappings() ([]Mapping, error) {
	rows, err := s.DB.Query(`SELECT ` + mappingCols + ` FROM mappings ORDER BY listen_port, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Mapping
	for rows.Next() {
		m, err := scanMapping(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) GetMapping(id int64) (Mapping, error) {
	m, err := scanMapping(s.DB.QueryRow(`SELECT `+mappingCols+` FROM mappings WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

func (s *Store) CreateMapping(m *Mapping) (int64, error) {
	ts := nowTS()
	res, err := s.DB.Exec(`INSERT INTO mappings
		(name, enabled, engine, protocol, listen_ip, listen_port, server_names, ssl_cert_id,
		 redirect_to, websocket, http2, targets, balance, path_prefix, access_rules, extra_headers, path_routes,
		 host_header, decoy, decoy_html, routes, service_id, notes, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.Name, b2i(m.Enabled), m.Engine, m.Protocol, m.ListenIP, m.ListenPort,
		mustJSON(m.ServerNames), nullInt64(m.SSLCertID), m.RedirectTo, b2i(m.WebSocket), b2i(m.HTTP2),
		mustJSON(m.Targets), m.Balance, m.PathPrefix, mustJSON(m.AccessRules), mustJSON(m.ExtraHeaders),
		mustJSON(m.PathRoutes), m.HostHeader, m.Decoy, m.DecoyHTML, mustJSON(m.Routes), nullInt64(m.ServiceID),
		m.Notes, ts, ts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateMapping(m *Mapping) error {
	_, err := s.DB.Exec(`UPDATE mappings SET name=?, enabled=?, engine=?, protocol=?, listen_ip=?,
		listen_port=?, server_names=?, ssl_cert_id=?, redirect_to=?, websocket=?, http2=?,
		targets=?, balance=?, path_prefix=?, access_rules=?, extra_headers=?, path_routes=?,
		host_header=?, decoy=?, decoy_html=?, routes=?, service_id=?, notes=?, updated_at=? WHERE id=?`,
		m.Name, b2i(m.Enabled), m.Engine, m.Protocol, m.ListenIP, m.ListenPort,
		mustJSON(m.ServerNames), nullInt64(m.SSLCertID), m.RedirectTo, b2i(m.WebSocket), b2i(m.HTTP2),
		mustJSON(m.Targets), m.Balance, m.PathPrefix, mustJSON(m.AccessRules), mustJSON(m.ExtraHeaders),
		mustJSON(m.PathRoutes), m.HostHeader, m.Decoy, m.DecoyHTML, mustJSON(m.Routes), nullInt64(m.ServiceID),
		m.Notes, nowTS(), m.ID)
	return err
}

func (s *Store) DeleteMapping(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM mappings WHERE id=?`, id)
	if err == nil {
		_, _ = s.DB.Exec(`DELETE FROM target_health WHERE mapping_id=?`, id)
	}
	return err
}

// ---- certs ----

func (s *Store) ListCerts() ([]Cert, error) {
	rows, err := s.DB.Query(`SELECT id, name, type, cert_pem, key_pem, domains, expires_at, created_at FROM ssl_certs ORDER BY id`)
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

// UpdateCertPEM swaps the stored PEM pair (used by ACME renewals); expiry is
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

func (s *Store) DeleteCert(id int64) error {	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM mappings WHERE ssl_cert_id=?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("certificate is used by %d mapping(s)", n)
	}
	_, err := s.DB.Exec(`DELETE FROM ssl_certs WHERE id=?`, id)
	return err
}

// ---- ports (scan snapshot) ----

func (s *Store) ReplacePorts(entries []PortEntry) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM ports`); err != nil {
		return err
	}
	for _, p := range entries {
		if _, err := tx.Exec(`INSERT INTO ports(port, proto, listen_ip, process, pid, user_name, classification, managed, self)
			VALUES(?,?,?,?,?,?,?,?,?)`,
			p.Port, p.Proto, p.ListenIP, p.Process, p.PID, p.User, p.Classification, b2i(p.Managed), b2i(p.Self)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListPorts() ([]PortEntry, error) {
	rows, err := s.DB.Query(`SELECT port, proto, listen_ip, process, pid, user_name, classification, managed, self FROM ports ORDER BY port, proto`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PortEntry
	for rows.Next() {
		var p PortEntry
		var managed, self int
		if err := rows.Scan(&p.Port, &p.Proto, &p.ListenIP, &p.Process, &p.PID, &p.User, &p.Classification, &managed, &self); err != nil {
			return nil, err
		}
		p.Managed = managed == 1
		p.Self = self == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- health ----

func (s *Store) UpsertHealth(h TargetHealth) error {
	_, err := s.DB.Exec(`INSERT INTO target_health(mapping_id, target_index, host, port, status, latency_ms, fail_count, last_check_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(mapping_id, target_index) DO UPDATE SET
		host=excluded.host, port=excluded.port, status=excluded.status,
		latency_ms=excluded.latency_ms, fail_count=excluded.fail_count, last_check_at=excluded.last_check_at`,
		h.MappingID, h.TargetIndex, h.Host, h.Port, h.Status, h.LatencyMS, h.FailCount, h.LastCheckAt.Unix())
	return err
}

// DeleteHealth removes one target's health row (pruning stale targets).
func (s *Store) DeleteHealth(mappingID, targetIndex int) error {
	_, err := s.DB.Exec(`DELETE FROM target_health WHERE mapping_id=? AND target_index=?`, mappingID, targetIndex)
	return err
}

func (s *Store) ListHealth() ([]TargetHealth, error) {
	rows, err := s.DB.Query(`SELECT mapping_id, target_index, host, port, status, latency_ms, fail_count, last_check_at FROM target_health`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TargetHealth
	for rows.Next() {
		var h TargetHealth
		var last sql.NullInt64
		if err := rows.Scan(&h.MappingID, &h.TargetIndex, &h.Host, &h.Port, &h.Status, &h.LatencyMS, &h.FailCount, &last); err != nil {
			return nil, err
		}
		if last.Valid {
			t := time.Unix(last.Int64, 0)
			h.LastCheckAt = &t
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ---- audit ----

func (s *Store) Audit(actor, action, detail, status string) {
	_, _ = s.DB.Exec(`INSERT INTO audit_logs(actor, action, detail, status, created_at) VALUES(?,?,?,?,?)`,
		actor, action, detail, status, nowTS())
}

func (s *Store) ListAudit(limit int) ([]AuditLog, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.DB.Query(`SELECT id, actor, action, detail, status, created_at FROM audit_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditLog
	for rows.Next() {
		var a AuditLog
		var createdTS int64
		if err := rows.Scan(&a.ID, &a.Actor, &a.Action, &a.Detail, &a.Status, &createdTS); err != nil {
			return nil, err
		}
		a.CreatedAt = time.Unix(createdTS, 0)
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---- tunnel relays (Hedioum) ----

func (s *Store) ListTunnelRelays() ([]TunnelRelay, error) {
	rows, err := s.DB.Query(`SELECT ` + tunnelRelayCols + ` FROM tunnel_relays ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TunnelRelay
	for rows.Next() {
		r, err := scanTunnelRelay(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetTunnelRelay(id int64) (TunnelRelay, error) {
	row := s.DB.QueryRow(`SELECT `+tunnelRelayCols+` FROM tunnel_relays WHERE id=?`, id)
	r, err := scanTunnelRelay(row)
	if errors.Is(err, sql.ErrNoRows) {
		return TunnelRelay{}, ErrNotFound
	}
	return r, err
}

func (s *Store) CreateTunnelRelay(r *TunnelRelay) (int64, error) {
	ts := nowTS()
	res, err := s.DB.Exec(`INSERT INTO tunnel_relays
		(name, mode, enabled, target_host, target_port, listen_ip, listen_port, bridge_port,
		 udp, host_header, domain, ssl_cert_id, notes, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.Name, r.Mode, b2i(r.Enabled), r.TargetHost, r.TargetPort, r.ListenIP, r.ListenPort, r.BridgePort,
		b2i(r.UDP), r.HostHeader, r.Domain, nullInt64(r.SSLCertID), r.Notes, ts, ts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateTunnelRelay(r *TunnelRelay) error {
	_, err := s.DB.Exec(`UPDATE tunnel_relays SET name=?, mode=?, enabled=?, target_host=?, target_port=?,
		listen_ip=?, listen_port=?, bridge_port=?, udp=?, host_header=?, domain=?, ssl_cert_id=?, notes=?, updated_at=? WHERE id=?`,
		r.Name, r.Mode, b2i(r.Enabled), r.TargetHost, r.TargetPort, r.ListenIP, r.ListenPort, r.BridgePort,
		b2i(r.UDP), r.HostHeader, r.Domain, nullInt64(r.SSLCertID), r.Notes, nowTS(), r.ID)
	return err
}

func (s *Store) DeleteTunnelRelay(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM tunnel_relays WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetSettingOr(key, def string) string {
	if v, err := s.GetSetting(key); err == nil && v != "" {
		return v
	}
	return def
}

// ---- alerts ----

func (s *Store) InsertAlert(a Alert) error {
	_, err := s.DB.Exec(`INSERT INTO alerts(severity, category, title, detail, target, dedup_key, created_at)
		VALUES(?,?,?,?,?,?,?)`, a.Severity, a.Category, a.Title, a.Detail, a.Target, a.DedupKey, nowTS())
	return err
}

func (s *Store) ListAlerts(limit int) ([]Alert, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.DB.Query(`SELECT id, severity, category, title, detail, target, dedup_key, acknowledged, created_at
		FROM alerts ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		var a Alert
		var ack, c int64
		if err := rows.Scan(&a.ID, &a.Severity, &a.Category, &a.Title, &a.Detail, &a.Target, &a.DedupKey, &ack, &c); err != nil {
			return nil, err
		}
		a.Ack = ack == 1
		a.CreatedAt = time.Unix(c, 0)
		out = append(out, a)
	}
	return out, rows.Err()
}

// LastAlertByDedup returns the timestamp of the most recent alert with the
// given dedup key (0 when none) — the alerter uses it for cooldowns.
func (s *Store) LastAlertByDedup(key string) (int64, error) {
	var ts int64
	err := s.DB.QueryRow(`SELECT COALESCE(MAX(created_at), 0) FROM alerts WHERE dedup_key=?`, key).Scan(&ts)
	return ts, err
}

func (s *Store) AckAlert(id int64) error {
	_, err := s.DB.Exec(`UPDATE alerts SET acknowledged=1 WHERE id=?`, id)
	return err
}

func (s *Store) UnackedAlertCount() int {
	var n int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM alerts WHERE acknowledged=0`).Scan(&n)
	return n
}

// ---- config versions ----

// CreateConfigVersion snapshots the current mappings/relays with a monotonic version.
func (s *Store) CreateConfigVersion(author, description, deployResult string) (int64, error) {
	mappings, err := s.ListMappings()
	if err != nil {
		return 0, err
	}
	mj, _ := json.Marshal(mappings)
	relays, err := s.ListTunnelRelays()
	if err != nil {
		return 0, err
	}
	rj, _ := json.Marshal(relays)
	nodes, _ := s.ListServerNodesPublic()
	var ver int64
	if err := s.DB.QueryRow(`SELECT COALESCE(MAX(version), 0) + 1 FROM config_versions`).Scan(&ver); err != nil {
		return 0, err
	}
	_, err = s.DB.Exec(`INSERT INTO config_versions(version, author, description, mappings_json, relays_json, node_count, deploy_result, created_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		ver, author, description, string(mj), string(rj), len(nodes), deployResult, nowTS())
	if err != nil {
		return 0, err
	}
	// retention: keep the most recent 50 versions
	_, _ = s.DB.Exec(`DELETE FROM config_versions WHERE version <= (SELECT MAX(version) - 50 FROM config_versions)`)
	return ver, nil
}

func (s *Store) ListConfigVersions() ([]ConfigVersion, error) {
	rows, err := s.DB.Query(`SELECT id, version, author, description, node_count, deploy_result, created_at
		FROM config_versions ORDER BY version DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConfigVersion
	for rows.Next() {
		var v ConfigVersion
		var c int64
		if err := rows.Scan(&v.ID, &v.Version, &v.Author, &v.Description, &v.NodeCount, &v.DeployResult, &c); err != nil {
			return nil, err
		}
		v.CreatedAt = time.Unix(c, 0)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) GetConfigVersion(version int64) (ConfigVersion, string, string, error) {
	var v ConfigVersion
	var mj, rj string
	var c int64
	err := s.DB.QueryRow(`SELECT id, version, author, description, mappings_json, relays_json, node_count, deploy_result, created_at
		FROM config_versions WHERE version=?`, version).
		Scan(&v.ID, &v.Version, &v.Author, &v.Description, &mj, &rj, &v.NodeCount, &v.DeployResult, &c)
	if errors.Is(err, sql.ErrNoRows) {
		return v, "", "", ErrNotFound
	}
	if err != nil {
		return v, "", "", err
	}
	v.CreatedAt = time.Unix(c, 0)
	return v, mj, rj, nil
}

// SetConfigVersionDeployResult stamps ok/rollback/error on a version row.
func (s *Store) SetConfigVersionDeployResult(version int64, result string) error {
	_, err := s.DB.Exec(`UPDATE config_versions SET deploy_result=? WHERE version=?`, result, version)
	return err
}

// ---- services ----

func (s *Store) ListServices() ([]Service, error) {
	rows, err := s.DB.Query(`SELECT id, name, description, enabled, notes, created_at, updated_at FROM services ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Service
	for rows.Next() {
		var sv Service
		var enabled int
		var c, u int64
		if err := rows.Scan(&sv.ID, &sv.Name, &sv.Description, &enabled, &sv.Notes, &c, &u); err != nil {
			return nil, err
		}
		sv.Enabled = enabled == 1
		sv.CreatedAt = time.Unix(c, 0)
		sv.UpdatedAt = time.Unix(u, 0)
		out = append(out, sv)
	}
	return out, rows.Err()
}

func (s *Store) CreateService(sv *Service) (int64, error) {
	ts := nowTS()
	res, err := s.DB.Exec(`INSERT INTO services(name, description, enabled, notes, created_at, updated_at)
		VALUES(?,?,?,?,?,?)`, sv.Name, sv.Description, b2i(sv.Enabled), sv.Notes, ts, ts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateService(sv *Service) error {
	_, err := s.DB.Exec(`UPDATE services SET name=?, description=?, enabled=?, notes=?, updated_at=? WHERE id=?`,
		sv.Name, sv.Description, b2i(sv.Enabled), sv.Notes, nowTS(), sv.ID)
	return err
}

func (s *Store) DeleteService(id int64) error {
	// detach member mappings instead of deleting them
	_, _ = s.DB.Exec(`UPDATE mappings SET service_id=NULL, updated_at=? WHERE service_id=?`, nowTS(), id)
	res, err := s.DB.Exec(`DELETE FROM services WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- node metrics ----

func (s *Store) InsertMetric(p MetricPoint) error {
	_, err := s.DB.Exec(`INSERT INTO node_metrics(node_id, cpu_percent, mem_percent, disk_percent, rx_bytes, tx_bytes, rx_bps, tx_bps, conns, ts)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		p.NodeID, p.CPUPercent, p.MemPercent, p.DiskPercent, p.RxBytes, p.TxBytes, p.RxBps, p.TxBps, p.Conns, p.TS)
	return err
}

// QueryMetrics returns sampled points for a node in [from, to], downsampled
// to at most ~200 rows for charting.
func (s *Store) QueryMetrics(nodeID int64, from, to int64) ([]MetricPoint, error) {
	rows, err := s.DB.Query(`SELECT node_id, cpu_percent, mem_percent, disk_percent, rx_bytes, tx_bytes, rx_bps, tx_bps, conns, ts
		FROM node_metrics WHERE node_id=? AND ts BETWEEN ? AND ? ORDER BY ts`, nodeID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MetricPoint
	for rows.Next() {
		var p MetricPoint
		if err := rows.Scan(&p.NodeID, &p.CPUPercent, &p.MemPercent, &p.DiskPercent, &p.RxBytes, &p.TxBytes, &p.RxBps, &p.TxBps, &p.Conns, &p.TS); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	// downsample: keep every Nth point so charts stay light
	if len(out) > 200 {
		step := len(out) / 200
		slim := make([]MetricPoint, 0, 201)
		for i := 0; i < len(out); i += step {
			slim = append(slim, out[i])
		}
		slim = append(slim, out[len(out)-1])
		out = slim
	}
	return out, rows.Err()
}

// PruneMetrics deletes samples older than the retention window (default 7d).
func (s *Store) PruneMetrics(retention time.Duration) {
	cutoff := time.Now().Add(-retention).Unix()
	_, _ = s.DB.Exec(`DELETE FROM node_metrics WHERE ts < ?`, cutoff)
}

// ---- rate limiting ----

func (s *Store) ListRateProfiles() ([]RateProfile, error) {
	rows, err := s.DB.Query(`SELECT id, name, download_bps, upload_bps, enabled, notes, created_at, updated_at FROM rate_profiles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RateProfile
	for rows.Next() {
		var p RateProfile
		var enabled int
		var c, u int64
		if err := rows.Scan(&p.ID, &p.Name, &p.DownloadBPS, &p.UploadBPS, &enabled, &p.Notes, &c, &u); err != nil {
			return nil, err
		}
		p.Enabled = enabled == 1
		p.CreatedAt = time.Unix(c, 0)
		p.UpdatedAt = time.Unix(u, 0)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) CreateRateProfile(p *RateProfile) (int64, error) {
	ts := nowTS()
	res, err := s.DB.Exec(`INSERT INTO rate_profiles(name, download_bps, upload_bps, enabled, notes, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?)`, p.Name, p.DownloadBPS, p.UploadBPS, b2i(p.Enabled), p.Notes, ts, ts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateRateProfile(p *RateProfile) error {
	_, err := s.DB.Exec(`UPDATE rate_profiles SET name=?, download_bps=?, upload_bps=?, enabled=?, notes=?, updated_at=? WHERE id=?`,
		p.Name, p.DownloadBPS, p.UploadBPS, b2i(p.Enabled), p.Notes, nowTS(), p.ID)
	return err
}

func (s *Store) DeleteRateProfile(id int64) error {
	// detach policies using this profile (they keep their bps values)
	_, err := s.DB.Exec(`UPDATE rate_limit_policies SET profile_id=NULL, updated_at=? WHERE profile_id=?`, nowTS(), id)
	if err != nil {
		return err
	}
	res, err := s.DB.Exec(`DELETE FROM rate_profiles WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpsertPasarguardUser syncs one user by UUID (idempotent).
func (s *Store) UpsertPasarguardUser(u *PasarguardUser) error {
	ts := nowTS()
	_, err := s.DB.Exec(`INSERT INTO pasarguard_users(uuid, username, node_id, enabled, expired, last_ip, synced_at, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(uuid) DO UPDATE SET
		username=excluded.username, node_id=excluded.node_id, enabled=excluded.enabled,
		expired=excluded.expired, last_ip=excluded.last_ip, synced_at=excluded.synced_at, updated_at=excluded.updated_at`,
		u.UUID, u.Username, nullInt64(u.NodeID), b2i(u.Enabled), b2i(u.Expired), u.LastIP, ts, ts, ts)
	return err
}

func (s *Store) ListPasarguardUsers() ([]PasarguardUser, error) {
	rows, err := s.DB.Query(`SELECT id, uuid, username, node_id, enabled, expired, last_ip, synced_at, created_at, updated_at FROM pasarguard_users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PasarguardUser
	for rows.Next() {
		var u PasarguardUser
		var enabled, expired int
		var nodeID sql.NullInt64
		var sy, c, up int64
		if err := rows.Scan(&u.ID, &u.UUID, &u.Username, &nodeID, &enabled, &expired, &u.LastIP, &sy, &c, &up); err != nil {
			return nil, err
		}
		u.Enabled = enabled == 1
		u.Expired = expired == 1
		if nodeID.Valid {
			nid := nodeID.Int64
			u.NodeID = &nid
		}
		u.SyncedAt = time.Unix(sy, 0)
		u.CreatedAt = time.Unix(c, 0)
		u.UpdatedAt = time.Unix(up, 0)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) ListRatePolicies() ([]RateLimitPolicy, error) {
	rows, err := s.DB.Query(`SELECT id, uuid, node_id, profile_id, download_bps, upload_bps, custom, enabled, status, last_error, last_pushed_version, created_at, updated_at FROM rate_limit_policies ORDER BY uuid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RateLimitPolicy
	for rows.Next() {
		var p RateLimitPolicy
		var enabled, custom int
		var profileID sql.NullInt64
		var c, u int64
		if err := rows.Scan(&p.ID, &p.UUID, &p.NodeID, &profileID, &p.DownloadBPS, &p.UploadBPS, &custom, &enabled, &p.Status, &p.LastError, &p.LastPushedVer, &c, &u); err != nil {
			return nil, err
		}
		p.Enabled = enabled == 1
		p.Custom = custom == 1
		if profileID.Valid {
			pid := profileID.Int64
			p.ProfileID = &pid
		}
		p.CreatedAt = time.Unix(c, 0)
		p.UpdatedAt = time.Unix(u, 0)
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpsertRatePolicy creates or updates the (uuid, node) policy. The version
// bump makes the push loop pick it up incrementally.
func (s *Store) UpsertRatePolicy(p *RateLimitPolicy) error {
	ts := nowTS()
	_, err := s.DB.Exec(`INSERT INTO rate_limit_policies(uuid, node_id, profile_id, download_bps, upload_bps, custom, enabled, status, last_error, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?, 'pending', '', ?, ?)
		ON CONFLICT(uuid, node_id) DO UPDATE SET
		profile_id=excluded.profile_id, download_bps=excluded.download_bps,
		upload_bps=excluded.upload_bps, custom=excluded.custom, enabled=excluded.enabled,
		status='pending', last_error='', updated_at=excluded.updated_at`,
		p.UUID, p.NodeID, nullInt64(p.ProfileID), p.DownloadBPS, p.UploadBPS, b2i(p.Custom), b2i(p.Enabled), ts, ts)
	return err
}

func (s *Store) DeleteRatePolicy(uuid string, nodeID int64) error {
	res, err := s.DB.Exec(`DELETE FROM rate_limit_policies WHERE uuid=? AND node_id=?`, uuid, nodeID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPolicyStatus records the push result for a policy.
func (s *Store) SetPolicyStatus(uuid string, nodeID int64, status, errMsg string, version int64) error {
	_, err := s.DB.Exec(`UPDATE rate_limit_policies SET status=?, last_error=?, last_pushed_version=?, updated_at=? WHERE uuid=? AND node_id=?`,
		status, errMsg, version, nowTS(), uuid, nodeID)
	return err
}

// PolicyPlanVersion returns the max updated_at of policies for change
// detection (monotonic plan version across all edits).
func (s *Store) PolicyPlanVersion() int64 {
	var v int64
	_ = s.DB.QueryRow(`SELECT COALESCE(MAX(updated_at), 0) FROM rate_limit_policies`).Scan(&v)
	return v
}

// ---- server nodes (multi-server management) ----

func scanServerNode(row interface{ Scan(...any) error }) (ServerNode, error) {
	var n ServerNode
	var enabled int
	var lastSeen sql.NullInt64
	var createdTS, updatedTS int64
	err := row.Scan(&n.ID, &n.Name, &n.Host, &n.Port, &n.APIToken, &n.Role, &enabled,
		&n.Notes, &n.Status, &lastSeen, &createdTS, &updatedTS)
	if err != nil {
		return n, err
	}
	n.Enabled = enabled == 1
	if lastSeen.Valid {
		t := time.Unix(lastSeen.Int64, 0)
		n.LastSeen = &t
	}
	n.CreatedAt = time.Unix(createdTS, 0)
	n.UpdatedAt = time.Unix(updatedTS, 0)
	return n, nil
}

const serverNodeCols = `id, name, host, port, api_token, role, enabled, notes, status, last_seen, created_at, updated_at`

func (s *Store) ListServerNodes() ([]ServerNode, error) {
	rows, err := s.DB.Query(`SELECT ` + serverNodeCols + ` FROM server_nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServerNode
	for rows.Next() {
		n, err := scanServerNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) ListServerNodesPublic() ([]ServerNode, error) {
	// same as ListServerNodes but the api_token column is scanned into a discard
	nodes, err := s.ListServerNodes()
	if err != nil {
		return nil, err
	}
	for i := range nodes {
		nodes[i].APIToken = ""
	}
	return nodes, nil
}

func (s *Store) GetServerNode(id int64) (ServerNode, error) {
	row := s.DB.QueryRow(`SELECT `+serverNodeCols+` FROM server_nodes WHERE id=?`, id)
	n, err := scanServerNode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ServerNode{}, ErrNotFound
	}
	return n, err
}

func (s *Store) CreateServerNode(n *ServerNode) (int64, error) {
	ts := nowTS()
	res, err := s.DB.Exec(`INSERT INTO server_nodes(name, host, port, api_token, role, enabled, notes, status, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		n.Name, n.Host, n.Port, n.APIToken, n.Role, b2i(n.Enabled), n.Notes, "unknown", ts, ts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateServerNode(n *ServerNode) error {
	// api_token is only replaced when a non-empty one is provided
	_, err := s.DB.Exec(`UPDATE server_nodes SET name=?, host=?, port=?, role=?, enabled=?, notes=?, status=?, last_seen=?, updated_at=?
		WHERE id=?`,
		n.Name, n.Host, n.Port, n.Role, b2i(n.Enabled), n.Notes, n.Status, nullTime(n.LastSeen), nowTS(), n.ID)
	return err
}

func (s *Store) UpdateServerNodeToken(id int64, token string) error {
	_, err := s.DB.Exec(`UPDATE server_nodes SET api_token=?, updated_at=? WHERE id=?`, token, nowTS(), id)
	return err
}

// TouchServerNode updates the probe status of a node without touching other fields.
func (s *Store) TouchServerNode(id int64, status string) error {
	_, err := s.DB.Exec(`UPDATE server_nodes SET status=?, last_seen=?, updated_at=? WHERE id=?`,
		status, nowTS(), nowTS(), id)
	return err
}

func (s *Store) DeleteServerNode(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM server_nodes WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetSettingSilently(key, val string) {
	_ = s.SetSetting(key, val)
}

// ---- live connections ----

// ReplaceConnections atomically swaps the live-connection snapshot: new rows
// are inserted (keeping their original first_seen), missing ones deleted.
func (s *Store) ReplaceConnections(conns []ConnEntry, now int64) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	type key struct{ src, dstIP string; srcP, dstP int }
	prev := map[key]int64{}
	rows, err := tx.Query(`SELECT src_ip, src_port, dst_ip, dst_port, first_seen FROM connections`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k key
		var fs int64
		if err := rows.Scan(&k.src, &k.srcP, &k.dstIP, &k.dstP, &fs); err != nil {
			rows.Close()
			return err
		}
		prev[k] = fs
	}
	rows.Close()

	if _, err := tx.Exec(`DELETE FROM connections`); err != nil {
		return err
	}
	for _, c := range conns {
		k := key{c.SrcIP, c.DstIP, c.SrcPort, c.DstPort}
		fs, ok := prev[k]
		if !ok {
			fs = now
		}
		if _, err := tx.Exec(`INSERT INTO connections
			(src_ip, src_port, dst_ip, dst_port, process, pid, uid, state, managed, inner, self, first_seen, last_seen)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			c.SrcIP, c.SrcPort, c.DstIP, c.DstPort, c.Process, c.PID, c.UID, c.State,
			b2i(c.Managed), b2i(c.Inner), b2i(c.Self), fs, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListConnections() ([]ConnEntry, error) {
	rows, err := s.DB.Query(`SELECT src_ip, src_port, dst_ip, dst_port, process, pid, uid, state, managed, inner, self, first_seen, last_seen
		FROM connections ORDER BY last_seen DESC, src_ip`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConnEntry
	for rows.Next() {
		var c ConnEntry
		var managed, inner, self int
		var firstSeen, lastSeen int64
		if err := rows.Scan(&c.SrcIP, &c.SrcPort, &c.DstIP, &c.DstPort, &c.Process, &c.PID, &c.UID, &c.State,
			&managed, &inner, &self, &firstSeen, &lastSeen); err != nil {
			return nil, err
		}
		c.Managed = managed == 1
		c.Inner = inner == 1
		c.Self = self == 1
		c.FirstSeen = firstSeen
		c.LastSeen = lastSeen
		out = append(out, c)
	}
	return out, rows.Err()
}

// TopTalkers aggregates current live connections per external source IP,
// returning the noisiest clients with their most-used destinations.
func (s *Store) TopTalkers(limit int) ([]TopTalker, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.DB.Query(`
		SELECT src_ip, COUNT(*) AS conns, MIN(first_seen) AS first_seen FROM connections
		WHERE inner = 0 AND self = 0
		GROUP BY src_ip ORDER BY conns DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TopTalker
	for rows.Next() {
		var t TopTalker
		if err := rows.Scan(&t.SrcIP, &t.Conns, &t.FirstSeen); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	// attach top destinations per talker
	for i := range out {
		dRows, err := s.DB.Query(`SELECT dst_ip || ':' || dst_port AS target, COUNT(*) c FROM connections
			WHERE src_ip = ? GROUP BY target ORDER BY c DESC LIMIT 3`, out[i].SrcIP)
		if err != nil {
			continue
		}
		var targets []string
		for dRows.Next() {
			var t string
			var c int
			if err := dRows.Scan(&t, &c); err == nil {
				targets = append(targets, t)
			}
		}
		dRows.Close()
		out[i].Targets = strings.Join(targets, ", ")
	}
	return out, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Unix()
}
