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

func (s *Store) ListServerNodesPaged(limit, offset int) ([]ServerNode, error) {
	rows, err := s.DB.Query(`SELECT `+serverNodeCols+` FROM server_nodes ORDER BY id LIMIT ? OFFSET ?`, limit, offset)
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
