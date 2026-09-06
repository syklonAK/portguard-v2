package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Code in this file was split out of store.go verbatim (v2.11 audit):
// same package, same API — only the file boundaries changed.

func (s *Store) ListMappingsPaged(limit, offset int) ([]Mapping, error) {
	q := `SELECT ` + mappingCols + ` FROM mappings ORDER BY listen_port, id LIMIT ? OFFSET ?`
	rows, err := s.DB.Query(q, limit, offset)
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
func (s *Store) ListMappings() ([]Mapping, error) {
	return s.ListMappingsPaged(-1, 0)
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
// ---- ports (scan snapshot) ----
// ReplacePorts synchronizes the ports table to the scanner's snapshot.
func (s *Store) ReplacePorts(entries []PortEntry) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	type portRow struct {
		port, pid      int
		proto, ip, proc, user, class string
		managed, self  int
	}
	const us = "\x1f" // unit separator: process names may contain any byte
	rowKey := func(r portRow) string {
		return fmt.Sprintf("%d%s%s%s%s%s%s%s%d%s%s%s%s%s%d%s%d", r.port, us, r.proto, us, r.ip, us, r.proc, us, r.pid, us, r.user, us, r.class, us, r.managed, us, r.self)
	}
	newRows := map[string]int{} // key -> remaining copies to match
	for _, p := range entries {
		r := portRow{p.Port, p.PID, p.Proto, p.ListenIP, p.Process, p.User, p.Classification, b2i(p.Managed), b2i(p.Self)}
		newRows[rowKey(r)]++
	}

	rows, err := tx.Query(`SELECT port, proto, listen_ip, process, pid, user_name, classification, managed, self, id FROM ports`)
	if err != nil {
		return err
	}
	var keepIDs, delIDs []int64
	for rows.Next() {
		var r portRow
		var id int64
		if err := rows.Scan(&r.port, &r.proto, &r.ip, &r.proc, &r.pid, &r.user, &r.class, &r.managed, &r.self, &id); err != nil {
			rows.Close()
			return err
		}
		k := rowKey(r)
		if n := newRows[k]; n > 0 {
			newRows[k] = n - 1
			keepIDs = append(keepIDs, id)
		} else {
			delIDs = append(delIDs, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	// insert entries whose newRows count is still positive: those had no
	// matching existing row after the keep/diff pass
	insertStmt, err := tx.Prepare(`INSERT INTO ports(port, proto, listen_ip, process, pid, user_name, classification, managed, self)
		VALUES(?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer insertStmt.Close()
	for _, p := range entries {
		r := portRow{p.Port, p.PID, p.Proto, p.ListenIP, p.Process, p.User, p.Classification, b2i(p.Managed), b2i(p.Self)}
		if newRows[rowKey(r)] > 0 {
			newRows[rowKey(r)]--
			if _, err := insertStmt.Exec(p.Port, p.Proto, p.ListenIP, p.Process, p.PID, p.User, p.Classification, b2i(p.Managed), b2i(p.Self)); err != nil {
				return err
			}
		}
	}

	delStmt, err := tx.Prepare(`DELETE FROM ports WHERE id=?`)
	if err != nil {
		return err
	}
	defer delStmt.Close()
	for _, id := range delIDs {
		if _, err := delStmt.Exec(id); err != nil {
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
func (s *Store) UpsertHealth(h TargetHealth) error {
	_, err := s.DB.Exec(`INSERT INTO target_health(mapping_id, target_index, host, port, status, latency_ms, fail_count, last_check_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(mapping_id, target_index) DO UPDATE SET
		host=excluded.host, port=excluded.port, status=excluded.status,
		latency_ms=excluded.latency_ms, fail_count=excluded.fail_count, last_check_at=excluded.last_check_at`,
		h.MappingID, h.TargetIndex, h.Host, h.Port, h.Status, h.LatencyMS, h.FailCount, h.LastCheckAt.Unix())
	return err
}
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
// ---- live connections ----
func (s *Store) ReplaceConnections(conns []ConnEntry, now int64) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Synchronize the connections table on the (src, dst) tuple: existing
	// rows are updated in place (preserving first_seen), new tuples are
	// inserted and tuples that disappeared are deleted — one transaction,
	// no full-table rewrite. Same-row duplicates cannot occur: the sampler
	// emits one entry per conntrack tuple.
	type key struct{ src, dstIP string; srcP, dstP int }
	prev := map[key]int64{}
	idByKey := map[key]int64{}
	rows, err := tx.Query(`SELECT id, src_ip, src_port, dst_ip, dst_port, first_seen FROM connections`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var k key
		var id int64
		var fs int64
		if err := rows.Scan(&id, &k.src, &k.srcP, &k.dstIP, &k.dstP, &fs); err != nil {
			rows.Close()
			return err
		}
		prev[k] = fs
		idByKey[k] = id
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	seen := map[key]bool{}
	for _, c := range conns {
		k := key{c.SrcIP, c.DstIP, c.SrcPort, c.DstPort}
		seen[k] = true
		fs, ok := prev[k]
		if !ok {
			fs = now
			if _, err := tx.Exec(`INSERT INTO connections
				(src_ip, src_port, dst_ip, dst_port, process, pid, uid, state, managed, inner, self, first_seen, last_seen)
				VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				c.SrcIP, c.SrcPort, c.DstIP, c.DstPort, c.Process, c.PID, c.UID, c.State,
				b2i(c.Managed), b2i(c.Inner), b2i(c.Self), fs, now); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(`UPDATE connections SET process=?, pid=?, uid=?, state=?, managed=?, inner=?, self=?, last_seen=? WHERE id=?`,
			c.Process, c.PID, c.UID, c.State, b2i(c.Managed), b2i(c.Inner), b2i(c.Self), now, idByKey[k]); err != nil {
			return err
		}
	}
	delStmt, err := tx.Prepare(`DELETE FROM connections WHERE id=?`)
	if err != nil {
		return err
	}
	defer delStmt.Close()
	for k, id := range idByKey {
		if !seen[k] {
			if _, err := delStmt.Exec(id); err != nil {
				return err
			}
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
