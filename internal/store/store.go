package store

import (
	"database/sql"
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

// ListMappingsPaged is ListMappings with LIMIT/OFFSET; a negative limit
// returns every row (the previous, still-default behavior of ListMappings).
// ---- certs ----

// ListCertsPaged is ListCerts with LIMIT/OFFSET; a negative limit returns
// every row (the previous, still-default behavior of ListCerts).
// UpdateCertPEM swaps the stored PEM pair (used by ACME renewals); expiry is
// Rows are diffed on their full tuple (the same port/proto/ip can appear
// twice under SO_REUSEPORT with different PIDs, so the natural key is the
// entire row): unchanged rows are left untouched, vanished rows are deleted
// and new rows inserted — all inside one transaction. The final state is
// exactly the discovered set, without the churn of DELETE-all + re-INSERT
// on every scan.
// ---- health ----

// DeleteHealth removes one target's health row (pruning stale targets).
// ---- audit ----

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

// given dedup key (0 when none) — the alerter uses it for cooldowns.
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

// to at most ~200 rows for charting.
// ---- rate limiting ----

// UpsertPasarguardUser syncs one user by UUID (idempotent).
// UpsertRatePolicy creates or updates the (uuid, node) policy. The version
// bump makes the push loop pick it up incrementally.
// SetPolicyStatus records the push result for a policy.
// PolicyPlanVersion returns the max updated_at of policies for change
// detection (monotonic plan version across all edits).
// ---- server nodes (multi-server management) ----

// ListServerNodesPaged is ListServerNodes with LIMIT/OFFSET; a negative
// limit returns every row (the previous, still-default behavior).
// ListServerNodesPublicPaged is the paginated redacted variant.
// TouchServerNode updates the probe status of a node without touching other fields.
func (s *Store) SetSettingSilently(key, val string) {
	_ = s.SetSetting(key, val)
}

// ReplaceConnections atomically swaps the live-connection snapshot: new rows
// are inserted (keeping their original first_seen), missing ones deleted.
// returning the noisiest clients with their most-used destinations.
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
