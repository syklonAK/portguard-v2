package store

import (
	"database/sql"
	"errors"
	"time"
)

// Code in this file was split out of store.go verbatim (v2.11 audit):
// same package, same API — only the file boundaries changed.

func scanServerNode(row interface{ Scan(...any) error }) (ServerNode, error) {
	var n ServerNode
	var enabled int
	var lastSeen sql.NullInt64
	var createdTS, updatedTS int64
	var connMode, uid sql.NullString
	err := row.Scan(&n.ID, &n.Name, &n.Host, &n.Port, &n.APIToken, &n.Role, &enabled,
		&n.Notes, &n.Status, &lastSeen, &createdTS, &updatedTS, &connMode, &uid)
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
	if connMode.Valid {
		n.ConnMode = connMode.String
	} else {
		n.ConnMode = "direct"
	}
	if uid.Valid {
		n.UID = uid.String
	}
	return n, nil
}

const serverNodeCols = `id, name, host, port, api_token, role, enabled, notes, status, last_seen, created_at, updated_at, conn_mode, uid`

func (s *Store) ListServerNodesPaged(limit, offset int) ([]ServerNode, error) {
	rows, err := s.DB.Query(`SELECT `+serverNodeCols+` FROM server_nodes ORDER BY id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServerNode{} // never nil — nil marshals to JSON null and the UI maps over it
	for rows.Next() {
		n, err := scanServerNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
func (s *Store) ListServerNodes() ([]ServerNode, error) {
	return s.ListServerNodesPaged(-1, 0)
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
func (s *Store) ListServerNodesPublicPaged(limit, offset int) ([]ServerNode, error) {
	nodes, err := s.ListServerNodesPaged(limit, offset)
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
// UpsertServerNodeByHostPort creates or refreshes a node keyed on
// (host, port) — used by the agent self-registration flow so re-running the
// installer on the same server updates the row instead of duplicating it.
// Returns (id, created).
func (s *Store) UpsertServerNodeByHostPort(n *ServerNode) (int64, bool, error) {
	var id int64
	err := s.DB.QueryRow(`SELECT id FROM server_nodes WHERE host=? AND port=?`, n.Host, n.Port).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		ts := nowTS()
		connMode := n.ConnMode
		if connMode == "" {
			connMode = "direct"
		}
		res, err := s.DB.Exec(`INSERT INTO server_nodes(name, host, port, api_token, role, enabled, notes, status, conn_mode, uid, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,'unknown',?,?,?)`,
			n.Name, n.Host, n.Port, n.APIToken, n.Role, b2i(n.Enabled), n.Notes, connMode, n.UID, ts, ts)
		if err != nil {
			return 0, false, err
		}
		id, err = res.LastInsertId()
		return id, true, err
	}
	if err != nil {
		return 0, false, err
	}
	_, err = s.DB.Exec(`UPDATE server_nodes SET api_token=?, role=?, conn_mode=?, uid=?, enabled=1, updated_at=? WHERE id=?`,
		n.APIToken, n.Role, n.ConnMode, n.UID, nowTS(), id)
	return id, false, err
}

func (s *Store) CreateServerNode(n *ServerNode) (int64, error) {
	ts := nowTS()
	connMode := n.ConnMode
	if connMode == "" {
		connMode = "direct"
	}
	res, err := s.DB.Exec(`INSERT INTO server_nodes(name, host, port, api_token, role, enabled, notes, status, conn_mode, uid, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		n.Name, n.Host, n.Port, n.APIToken, n.Role, b2i(n.Enabled), n.Notes, "unknown", connMode, n.UID, ts, ts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
func (s *Store) UpdateServerNode(n *ServerNode) error {
	// api_token is only replaced when a non-empty one is provided
	connMode := n.ConnMode
	if connMode == "" {
		connMode = "direct"
	}
	_, err := s.DB.Exec(`UPDATE server_nodes SET name=?, host=?, port=?, role=?, enabled=?, notes=?, status=?, last_seen=?, conn_mode=?, uid=?, updated_at=?
		WHERE id=?`,
		n.Name, n.Host, n.Port, n.Role, b2i(n.Enabled), n.Notes, n.Status, nullTime(n.LastSeen), connMode, n.UID, nowTS(), n.ID)
	return err
}
func (s *Store) UpdateServerNodeToken(id int64, token string) error {
	_, err := s.DB.Exec(`UPDATE server_nodes SET api_token=?, updated_at=? WHERE id=?`, token, nowTS(), id)
	return err
}

// SetServerNodeConnMode updates how the master reaches this node
// ("direct" = master dials the node API, "reverse" = the node holds a WS
// tunnel into the panel hub).
func (s *Store) SetServerNodeConnMode(id int64, mode, uid string) error {
	if mode == "" {
		mode = "direct"
	}
	_, err := s.DB.Exec(`UPDATE server_nodes SET conn_mode=?, uid=?, updated_at=? WHERE id=?`, mode, uid, nowTS(), id)
	return err
}
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
