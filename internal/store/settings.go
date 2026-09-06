package store

import (
	"database/sql"
	"errors"
)

// Code in this file was split out of store.go verbatim (v2.11 audit):
// same package, same API — only the file boundaries changed.

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
