package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/auth"
)

// adminPasswordKey is the settings row that holds the admin password verifier.
const adminPasswordKey = "admin.password"

// Auth returns the auth persistence over this database: the devices, the admin
// password and the pairing invitations. It implements auth.Store and
// auth.InviteStore, so a server can run entirely on the file.
func (db *DB) Auth() *AuthStore { return &AuthStore{db: db} }

// AuthStore is the SQLite implementation of the auth persistence.
type AuthStore struct {
	db *DB
}

var (
	_ auth.Store       = (*AuthStore)(nil)
	_ auth.InviteStore = (*AuthStore)(nil)
)

// SaveDevice implements auth.Store.
func (s *AuthStore) SaveDevice(rec auth.DeviceRecord) error {
	_, err := s.db.db.Exec(`
		INSERT INTO devices (
			id, name, platform, scope, created_at_ms, last_seen_at_ms, expires_at_ms,
			revoked, salt, hash, argon_time, argon_memory, argon_threads, argon_keylen
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			platform = excluded.platform,
			scope = excluded.scope,
			created_at_ms = excluded.created_at_ms,
			last_seen_at_ms = excluded.last_seen_at_ms,
			expires_at_ms = excluded.expires_at_ms,
			revoked = excluded.revoked,
			salt = excluded.salt,
			hash = excluded.hash,
			argon_time = excluded.argon_time,
			argon_memory = excluded.argon_memory,
			argon_threads = excluded.argon_threads,
			argon_keylen = excluded.argon_keylen`,
		rec.ID, rec.Name, rec.Platform, string(rec.Scope),
		millis(rec.CreatedAt), millis(rec.LastSeenAt), millis(rec.ExpiresAt),
		boolToInt(rec.Revoked), rec.Salt, rec.Hash,
		rec.Params.Time, rec.Params.Memory, rec.Params.Threads, rec.Params.KeyLen,
	)
	if err != nil {
		return fmt.Errorf("store: save device %s: %w", rec.ID, err)
	}
	return nil
}

// Device implements auth.Store.
func (s *AuthStore) Device(id string) (auth.DeviceRecord, bool) {
	row := s.db.db.QueryRow(`
		SELECT name, platform, scope, created_at_ms, last_seen_at_ms, expires_at_ms,
		       revoked, salt, hash, argon_time, argon_memory, argon_threads, argon_keylen
		FROM devices WHERE id = ?`, id)
	rec, err := scanDevice(id, row)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.DeviceRecord{}, false
	}
	if err != nil {
		return auth.DeviceRecord{}, false
	}
	return rec, true
}

// Devices implements auth.Store in creation order, the order the device list shows.
func (s *AuthStore) Devices() []auth.DeviceRecord {
	rows, err := s.db.db.Query(`
		SELECT id, name, platform, scope, created_at_ms, last_seen_at_ms, expires_at_ms,
		       revoked, salt, hash, argon_time, argon_memory, argon_threads, argon_keylen
		FROM devices ORDER BY created_at_ms, id`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	records := make([]auth.DeviceRecord, 0)
	for rows.Next() {
		var (
			id, name, platform, scope string
			created, seen, expires    int64
			revoked                   int
			salt, hash                []byte
			argon                     auth.Argon2Params
		)
		if err := rows.Scan(&id, &name, &platform, &scope, &created, &seen, &expires,
			&revoked, &salt, &hash, &argon.Time, &argon.Memory, &argon.Threads, &argon.KeyLen); err != nil {
			return records
		}
		records = append(records, auth.DeviceRecord{
			Device: auth.Device{
				ID:         id,
				Name:       name,
				Platform:   platform,
				Scope:      auth.Scope(scope),
				CreatedAt:  timeFromMillis(created),
				LastSeenAt: timeFromMillis(seen),
				ExpiresAt:  timeFromMillis(expires),
				Revoked:    revoked != 0,
			},
			Salt:   salt,
			Hash:   hash,
			Params: argon,
		})
	}
	return records
}

// RevokeDevice implements auth.Store.
func (s *AuthStore) RevokeDevice(id string, at time.Time) error {
	result, err := s.db.db.Exec(`UPDATE devices SET revoked = 1, expires_at_ms = ? WHERE id = ?`, millis(at), id)
	if err != nil {
		return fmt.Errorf("store: revoke device %s: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: revoke device %s: %w", id, err)
	}
	if affected == 0 {
		return auth.ErrNotFound
	}
	return nil
}

// adminPasswordRecord is the JSON stored under adminPasswordKey.
type adminPasswordRecord struct {
	Salt   string            `json:"salt"` // base64
	Hash   string            `json:"hash"` // base64
	Params auth.Argon2Params `json:"params"`
	SetAt  time.Time         `json:"setAt"`
}

// AdminPassword implements auth.Store.
func (s *AuthStore) AdminPassword() (auth.AdminPassword, bool) {
	var raw []byte
	err := s.db.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, adminPasswordKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.AdminPassword{}, false
	}
	if err != nil {
		return auth.AdminPassword{}, false
	}
	var record adminPasswordRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return auth.AdminPassword{}, false
	}
	salt, errSalt := base64.StdEncoding.DecodeString(record.Salt)
	hash, errHash := base64.StdEncoding.DecodeString(record.Hash)
	if errSalt != nil || errHash != nil {
		return auth.AdminPassword{}, false
	}
	return auth.AdminPassword{Salt: salt, Hash: hash, Params: record.Params, SetAt: record.SetAt}, true
}

// SetAdminPassword implements auth.Store.
func (s *AuthStore) SetAdminPassword(password auth.AdminPassword) error {
	record := adminPasswordRecord{
		Salt:   base64.StdEncoding.EncodeToString(password.Salt),
		Hash:   base64.StdEncoding.EncodeToString(password.Hash),
		Params: password.Params,
		SetAt:  password.SetAt,
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("store: encode admin password: %w", err)
	}
	_, err = s.db.db.Exec(`
		INSERT INTO settings (key, value, updated_at_ms) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at_ms = excluded.updated_at_ms`,
		adminPasswordKey, raw, millis(password.SetAt))
	if err != nil {
		return fmt.Errorf("store: save admin password: %w", err)
	}
	return nil
}

// Save implements auth.InviteStore. The pairing_invites table keeps its `kind` and
// `secret` columns — migrations are append-only, so they are never dropped — but
// after pairing was unified on a single invite kind every invitation is a code invite
// with no secret, so both are written as fixed literals.
func (s *AuthStore) Save(invite auth.Invite) error {
	_, err := s.db.db.Exec(`
		INSERT INTO pairing_invites (code, kind, secret, expires_at_ms) VALUES (?, 'code', '', ?)
		ON CONFLICT(code) DO UPDATE SET kind = 'code', secret = '', expires_at_ms = excluded.expires_at_ms`,
		invite.Code, millis(invite.ExpiresAt))
	if err != nil {
		return fmt.Errorf("store: save invitation: %w", err)
	}
	return nil
}

// Take implements auth.InviteStore. It selects only the code and the expiry,
// normalizes the incoming code and compares in Go rather than in SQL, so the
// comparison stays constant-time over fixed-length values and a wrong code leaves the
// invitation alive, exactly like the memory implementation; the delete of the matched
// row happens in the same transaction.
func (s *AuthStore) Take(code string, now time.Time) (auth.Invite, bool) {
	ctx := context.Background()
	tx, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return auth.Invite{}, false
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM pairing_invites WHERE expires_at_ms <= ?`, millis(now)); err != nil {
		return auth.Invite{}, false
	}
	rows, err := tx.QueryContext(ctx, `SELECT code, expires_at_ms FROM pairing_invites`)
	if err != nil {
		return auth.Invite{}, false
	}
	type candidate struct {
		code    string
		expires int64
	}
	candidates := make([]candidate, 0)
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.code, &c.expires); err != nil {
			rows.Close()
			return auth.Invite{}, false
		}
		candidates = append(candidates, c)
	}
	rows.Close()

	want := auth.NormalizeCode(code)
	matched := -1
	for index, c := range candidates {
		if constantTimeCodeEqual(c.code, want) {
			matched = index
			break
		}
	}
	if matched < 0 {
		return auth.Invite{}, false
	}
	found := candidates[matched]
	if _, err := tx.ExecContext(ctx, `DELETE FROM pairing_invites WHERE code = ?`, found.code); err != nil {
		return auth.Invite{}, false
	}
	if err := tx.Commit(); err != nil {
		return auth.Invite{}, false
	}
	return auth.Invite{
		Code:      found.code,
		ExpiresAt: timeFromMillis(found.expires),
	}, true
}

// Pending implements auth.InviteStore.
func (s *AuthStore) Pending(now time.Time) int {
	var count int
	if err := s.db.db.QueryRow(`SELECT COUNT(*) FROM pairing_invites WHERE expires_at_ms > ?`, millis(now)).Scan(&count); err != nil {
		return 0
	}
	return count
}

// scanDevice reads one device row selected without its id.
func scanDevice(id string, row *sql.Row) (auth.DeviceRecord, error) {
	var (
		name, platform, scope  string
		created, seen, expires int64
		revoked                int
		salt, hash             []byte
		argon                  auth.Argon2Params
	)
	if err := row.Scan(&name, &platform, &scope, &created, &seen, &expires,
		&revoked, &salt, &hash, &argon.Time, &argon.Memory, &argon.Threads, &argon.KeyLen); err != nil {
		return auth.DeviceRecord{}, err
	}
	return auth.DeviceRecord{
		Device: auth.Device{
			ID:         id,
			Name:       name,
			Platform:   platform,
			Scope:      auth.Scope(scope),
			CreatedAt:  timeFromMillis(created),
			LastSeenAt: timeFromMillis(seen),
			ExpiresAt:  timeFromMillis(expires),
			Revoked:    revoked != 0,
		},
		Salt:   salt,
		Hash:   hash,
		Params: argon,
	}, nil
}

// millis is the storage format of every timestamp: Unix milliseconds UTC, sortable
// as an integer and free of driver time-zone behaviour.
func millis(t time.Time) int64 { return t.UTC().UnixMilli() }

// timeFromMillis is the inverse of millis.
func timeFromMillis(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// inviteCodeBytes is the length of a normalized pairing code
// (auth.inviteCodeLength). It sizes the comparison buffer below; the two must agree.
const inviteCodeBytes = 6

// constantTimeCodeEqual compares a stored invitation code against an already
// normalized candidate in constant time, exactly like the memory InviteStore. Both are
// copied into buffers of the same fixed length first, so a short guess is padded and
// compared for the full length instead of making the underlying compare return early on
// a length mismatch.
func constantTimeCodeEqual(stored, candidate string) bool {
	var a, b [inviteCodeBytes]byte
	copy(a[:], stored)
	copy(b[:], candidate)
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}
