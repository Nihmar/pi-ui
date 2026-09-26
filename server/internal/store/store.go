package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite" // pure-Go SQLite driver: keeps CGO_ENABLED=0
)

// migrations are applied in order, **append-only**: a schema change appends a string,
// and an existing migration is never edited or reordered. A database at user_version N
// applies migrations[N] next, so reordering would make an up-to-date file try to
// recreate what it already has. A migration runs in one transaction, so a crash leaves
// the database at a known version.
var migrations = []string{
	// 1: device identity and the admin password.
	`CREATE TABLE devices (
		id             TEXT PRIMARY KEY,
		name           TEXT NOT NULL,
		platform       TEXT NOT NULL DEFAULT '',
		scope          TEXT NOT NULL,
		created_at_ms  INTEGER NOT NULL,
		last_seen_at_ms INTEGER NOT NULL,
		expires_at_ms  INTEGER NOT NULL,
		revoked        INTEGER NOT NULL DEFAULT 0,
		salt           BLOB NOT NULL,
		hash           BLOB NOT NULL,
		argon_time     INTEGER NOT NULL,
		argon_memory   INTEGER NOT NULL,
		argon_threads  INTEGER NOT NULL,
		argon_keylen   INTEGER NOT NULL
	);
	CREATE INDEX idx_devices_created ON devices (created_at_ms);
	CREATE TABLE settings (
		key           TEXT PRIMARY KEY,
		value         BLOB NOT NULL,
		updated_at_ms INTEGER NOT NULL
	);`,
	// 2: pairing invitations, shared between `pi-ui pair` and a running server.
	`CREATE TABLE pairing_invites (
		code          TEXT PRIMARY KEY,
		kind          TEXT NOT NULL,
		secret        TEXT NOT NULL DEFAULT '',
		expires_at_ms INTEGER NOT NULL
	);
	CREATE INDEX idx_invites_expires ON pairing_invites (expires_at_ms);`,
	// 3: the audit trail (internal/audit). Append-only, pruned by retention; the
	// indexes are the ones the reader filters on (newest first, one action, one
	// actor, one session).
	`CREATE TABLE audit (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		at_ms           INTEGER NOT NULL,
		action          TEXT NOT NULL,
		outcome         TEXT NOT NULL,
		actor_device_id TEXT NOT NULL DEFAULT '',
		actor_name      TEXT NOT NULL DEFAULT '',
		actor_scope     TEXT NOT NULL DEFAULT '',
		session_id      TEXT NOT NULL DEFAULT '',
		target          TEXT NOT NULL DEFAULT '',
		remote_addr     TEXT NOT NULL DEFAULT '',
		details         BLOB
	);
	CREATE INDEX idx_audit_at ON audit (at_ms DESC, id DESC);
	CREATE INDEX idx_audit_action ON audit (action, at_ms DESC);
	CREATE INDEX idx_audit_actor ON audit (actor_device_id, at_ms DESC);
	CREATE INDEX idx_audit_session ON audit (session_id, at_ms DESC);`,
}

// DB is an open state database.
type DB struct {
	db   *sql.DB
	path string
}

// Open opens (or creates) the state database at path and brings its schema to the
// latest version. The path must be absolute: the caller (the CLI) resolves the state
// directory, this package never guesses one.
func Open(ctx context.Context, path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("store: empty database path")
	}
	sqlDB, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// One connection: SQLite takes one writer at a time, and a single process does not
	// need more readers than its handlers. It also keeps an in-memory database
	// coherent, which is what tests use.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("store: ping %s: %w", path, err)
	}
	handle := &DB{db: sqlDB, path: path}
	if err := handle.migrate(ctx); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return handle, nil
}

// Path is the database file this handle is bound to.
func (db *DB) Path() string { return db.path }

// Close closes the database.
func (db *DB) Close() error { return db.db.Close() }

// migrate applies every migration the database has not seen yet.
func (db *DB) migrate(ctx context.Context) error {
	var version int
	if err := db.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	if version > len(migrations) {
		return fmt.Errorf("store: database is at schema %d, this binary knows %d", version, len(migrations))
	}
	for index := version; index < len(migrations); index++ {
		tx, err := db.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("store: begin migration %d: %w", index+1, err)
		}
		if _, err := tx.ExecContext(ctx, migrations[index]); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: migration %d: %w", index+1, err)
		}
		// PRAGMA does not take a placeholder; the value is the loop index, never input.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", index+1)); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: stamp migration %d: %w", index+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: commit migration %d: %w", index+1, err)
		}
	}
	return nil
}

// dsn builds the modernc SQLite DSN: the file URI plus the pragmas the server
// relies on (WAL for readers next to the writer, a busy timeout so a hidden write
// does not fail a request, foreign keys on for the tables that will need them).
func dsn(path string) string {
	u := url.URL{Scheme: "file", Path: path}
	query := url.Values{}
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "synchronous(NORMAL)")
	u.RawQuery = query.Encode()
	return u.String()
}
