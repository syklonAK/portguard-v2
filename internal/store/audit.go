package store

import (
	"time"
)

// Code in this file was split out of store.go verbatim (v2.11 audit):
// same package, same API — only the file boundaries changed.

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
	out := []AuditLog{} // never nil — nil marshals to JSON null and the UI maps over it
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

// LastAlertByDedup returns the timestamp of the most recent alert with the
func (s *Store) LastAlertByDedup(key string) (int64, error) {
	var ts int64
	err := s.DB.QueryRow(`SELECT COALESCE(MAX(created_at), 0) FROM alerts WHERE dedup_key=?`, key).Scan(&ts)
	return ts, err
}
