package store

import (
	"database/sql"
	"errors"
	"time"
)

// TrojanRelay is one L4 trojan relay entry (hedioum-allinone "tj" suite):
// users hit domain:https_port (nginx stream TLS) → local bridge port →
// tunnel/direct → foreign node inbound.
type TrojanRelay struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Domain      string    `json:"domain"`
	HTTPSPort   int       `json:"https_port"`
	ForeignIP   string    `json:"foreign_ip"`
	ForeignPort int       `json:"foreign_port"`
	BridgePort  int       `json:"bridge_port"`
	Route       string    `json:"route"` // "" (auto) | tunnel | direct
	SocksPort   int       `json:"socks_port"`
	Enabled     bool      `json:"enabled"`
	Notes       string    `json:"notes"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TrojanIngress is one foreign-side forwarder: public tunnel port → node
// inbound on 127.0.0.1.
type TrojanIngress struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	ListenPort int       `json:"listen_port"`
	NodePort   int       `json:"node_port"`
	Enabled    bool      `json:"enabled"`
	Notes      string    `json:"notes"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func scanTrojanRelay(row interface{ Scan(...any) error }) (TrojanRelay, error) {
	var r TrojanRelay
	var enabled int
	var createdTS, updatedTS int64
	err := row.Scan(&r.ID, &r.Name, &r.Domain, &r.HTTPSPort, &r.ForeignIP, &r.ForeignPort,
		&r.BridgePort, &r.Route, &r.SocksPort, &enabled, &r.Notes, &createdTS, &updatedTS)
	if err != nil {
		return r, err
	}
	r.Enabled = enabled == 1
	r.CreatedAt = time.Unix(createdTS, 0)
	r.UpdatedAt = time.Unix(updatedTS, 0)
	return r, nil
}

func scanTrojanIngress(row interface{ Scan(...any) error }) (TrojanIngress, error) {
	var r TrojanIngress
	var enabled int
	var createdTS, updatedTS int64
	err := row.Scan(&r.ID, &r.Name, &r.ListenPort, &r.NodePort, &enabled, &r.Notes, &createdTS, &updatedTS)
	if err != nil {
		return r, err
	}
	r.Enabled = enabled == 1
	r.CreatedAt = time.Unix(createdTS, 0)
	r.UpdatedAt = time.Unix(updatedTS, 0)
	return r, nil
}

const trojanRelayCols = `id, name, domain, https_port, foreign_ip, foreign_port,
bridge_port, route, socks_port, enabled, notes, created_at, updated_at`

const trojanIngressCols = `id, name, listen_port, node_port, enabled, notes, created_at, updated_at`

// ---- trojan relays ----

func (s *Store) ListTrojanRelays() ([]TrojanRelay, error) {
	rows, err := s.DB.Query(`SELECT ` + trojanRelayCols + ` FROM trojan_relays ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrojanRelay
	for rows.Next() {
		r, err := scanTrojanRelay(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetTrojanRelay(id int64) (TrojanRelay, error) {
	row := s.DB.QueryRow(`SELECT `+trojanRelayCols+` FROM trojan_relays WHERE id=?`, id)
	r, err := scanTrojanRelay(row)
	if errors.Is(err, sql.ErrNoRows) {
		return TrojanRelay{}, ErrNotFound
	}
	return r, err
}

func (s *Store) CreateTrojanRelay(r *TrojanRelay) (int64, error) {
	ts := nowTS()
	res, err := s.DB.Exec(`INSERT INTO trojan_relays
		(name, domain, https_port, foreign_ip, foreign_port, bridge_port, route, socks_port, enabled, notes, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.Name, r.Domain, r.HTTPSPort, r.ForeignIP, r.ForeignPort, r.BridgePort,
		r.Route, r.SocksPort, b2i(r.Enabled), r.Notes, ts, ts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateTrojanRelay(r *TrojanRelay) error {
	_, err := s.DB.Exec(`UPDATE trojan_relays SET name=?, domain=?, https_port=?, foreign_ip=?,
		foreign_port=?, bridge_port=?, route=?, socks_port=?, enabled=?, notes=?, updated_at=? WHERE id=?`,
		r.Name, r.Domain, r.HTTPSPort, r.ForeignIP, r.ForeignPort, r.BridgePort,
		r.Route, r.SocksPort, b2i(r.Enabled), r.Notes, nowTS(), r.ID)
	return err
}

func (s *Store) DeleteTrojanRelay(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM trojan_relays WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- trojan ingresses (foreign side) ----

func (s *Store) ListTrojanIngresses() ([]TrojanIngress, error) {
	rows, err := s.DB.Query(`SELECT ` + trojanIngressCols + ` FROM trojan_ingresses ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrojanIngress
	for rows.Next() {
		r, err := scanTrojanIngress(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetTrojanIngress(id int64) (TrojanIngress, error) {
	row := s.DB.QueryRow(`SELECT `+trojanIngressCols+` FROM trojan_ingresses WHERE id=?`, id)
	r, err := scanTrojanIngress(row)
	if errors.Is(err, sql.ErrNoRows) {
		return TrojanIngress{}, ErrNotFound
	}
	return r, err
}

func (s *Store) CreateTrojanIngress(r *TrojanIngress) (int64, error) {
	ts := nowTS()
	res, err := s.DB.Exec(`INSERT INTO trojan_ingresses
		(name, listen_port, node_port, enabled, notes, created_at, updated_at) VALUES(?,?,?,?,?,?,?)`,
		r.Name, r.ListenPort, r.NodePort, b2i(r.Enabled), r.Notes, ts, ts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateTrojanIngress(r *TrojanIngress) error {
	_, err := s.DB.Exec(`UPDATE trojan_ingresses SET name=?, listen_port=?, node_port=?, enabled=?, notes=?, updated_at=? WHERE id=?`,
		r.Name, r.ListenPort, r.NodePort, b2i(r.Enabled), r.Notes, nowTS(), r.ID)
	return err
}

func (s *Store) DeleteTrojanIngress(id int64) error {
	res, err := s.DB.Exec(`DELETE FROM trojan_ingresses WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
