package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SettingsStore is the key/value table the server keeps its own policy in.
//
// It is deliberately dumb: keys, opaque JSON values and a timestamp. The meaning of a
// key lives in internal/settings, which is also what validates a value before it is
// written; this store only refuses to lose data.
type SettingsStore struct {
	db *DB
}

// Settings returns the settings store of this database.
func (db *DB) Settings() *SettingsStore { return &SettingsStore{db: db} }

// Get returns the stored value of one key.
func (s *SettingsStore) Get(key string) ([]byte, bool, error) {
	row := s.db.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key)
	var value []byte
	switch err := row.Scan(&value); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, false, nil
	case err != nil:
		return nil, false, fmt.Errorf("store: reading setting %q: %w", key, err)
	}
	return value, true, nil
}

// All returns every stored setting, oldest key first.
func (s *SettingsStore) All() (map[string][]byte, error) {
	rows, err := s.db.db.Query(`SELECT key, value FROM settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("store: listing settings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	settings := map[string][]byte{}
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("store: reading settings: %w", err)
		}
		settings[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: reading settings: %w", err)
	}
	return settings, nil
}

// Set stores one value, replacing whatever was there.
func (s *SettingsStore) Set(key string, value []byte, at time.Time) error {
	_, err := s.db.db.Exec(
		`INSERT INTO settings (key, value, updated_at_ms) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at_ms = excluded.updated_at_ms`,
		key, value, at.UTC().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("store: writing setting %q: %w", key, err)
	}
	return nil
}

// Delete removes one key. A key that was never stored is not an error.
func (s *SettingsStore) Delete(key string) error {
	if _, err := s.db.db.Exec(`DELETE FROM settings WHERE key = ?`, key); err != nil {
		return fmt.Errorf("store: deleting setting %q: %w", key, err)
	}
	return nil
}
