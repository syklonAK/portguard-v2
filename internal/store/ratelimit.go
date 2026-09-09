package store

import (
	"strings"
	"database/sql"
	"time"
)

// Code in this file was split out of store.go verbatim (v2.11 audit):
// same package, same API — only the file boundaries changed.

func (s *Store) ListRateProfiles() ([]RateProfile, error) {
	rows, err := s.DB.Query(`SELECT id, name, download_bps, upload_bps, enabled, notes, created_at, updated_at FROM rate_profiles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RateProfile{} // never nil — nil marshals to JSON null and the UI maps over it
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
// UpsertPasarguardUsersBatch syncs a whole page of users inside one
// transaction (a single WAL commit instead of one per user — with thousands
// of users the per-row commits dominated the sync round). node_id and
// last_ip are preserved: the panel sync never owns those columns.
func (s *Store) UpsertPasarguardUsersBatch(users []PasarguardUser) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO pasarguard_users(uuid, username, node_id, enabled, expired, last_ip, synced_at, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(uuid) DO UPDATE SET
		username=excluded.username, enabled=excluded.enabled,
		expired=excluded.expired, synced_at=excluded.synced_at, updated_at=excluded.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	ts := nowTS()
	for i := range users {
		u := &users[i]
		if _, err := stmt.Exec(u.UUID, u.Username, nullInt64(u.NodeID), b2i(u.Enabled), b2i(u.Expired), u.LastIP, ts, ts, ts); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func pasarguardUserFilter(search, status string) (string, []any) {
	var conds []string
	var args []any
	if search != "" {
		conds = append(conds, `(username LIKE ? OR uuid LIKE ?)`)
		pat := "%" + search + "%"
		args = append(args, pat, pat)
	}
	switch status {
	case "active":
		conds = append(conds, `enabled=1 AND expired=0`)
	case "expired":
		conds = append(conds, `expired=1`)
	case "disabled":
		conds = append(conds, `enabled=0`)
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	return where, args
}

// ListPasarguardUsersPaged returns one page of synced users; search matches
// username/uuid substrings and status is active|expired|disabled ("" = all).
func (s *Store) ListPasarguardUsersPaged(search, status string, limit, offset int) ([]PasarguardUser, error) {
	where, args := pasarguardUserFilter(search, status)
	q := `SELECT id, uuid, username, node_id, enabled, expired, last_ip, synced_at, created_at, updated_at
		FROM pasarguard_users` + where + ` ORDER BY username LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PasarguardUser{} // never nil — nil marshals to JSON null and the UI maps over it
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

// CountPasarguardUsers returns the row count for the same filter set.
func (s *Store) CountPasarguardUsers(search, status string) (int, error) {
	where, args := pasarguardUserFilter(search, status)
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM pasarguard_users` + where, args...).Scan(&n)
	return n, err
}

func (s *Store) ListPasarguardUsers() ([]PasarguardUser, error) {
	rows, err := s.DB.Query(`SELECT id, uuid, username, node_id, enabled, expired, last_ip, synced_at, created_at, updated_at FROM pasarguard_users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PasarguardUser{} // never nil — nil marshals to JSON null and the UI maps over it
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
	out := []RateLimitPolicy{} // never nil — nil marshals to JSON null and the UI maps over it
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
func (s *Store) SetPolicyStatus(uuid string, nodeID int64, status, errMsg string, version int64) error {
	_, err := s.DB.Exec(`UPDATE rate_limit_policies SET status=?, last_error=?, last_pushed_version=?, updated_at=? WHERE uuid=? AND node_id=?`,
		status, errMsg, version, nowTS(), uuid, nodeID)
	return err
}
func (s *Store) PolicyPlanVersion() int64 {
	var v int64
	_ = s.DB.QueryRow(`SELECT COALESCE(MAX(updated_at), 0) FROM rate_limit_policies`).Scan(&v)
	return v
}
