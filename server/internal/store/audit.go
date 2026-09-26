package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/audit"
)

// Audit returns the audit-trail persistence over this database.
func (db *DB) Audit() *AuditStore { return &AuditStore{db: db} }

// AuditStore is the SQLite implementation of audit.Store: append-only rows, newest
// first, pruned by retention.
type AuditStore struct {
	db *DB
}

var _ audit.Store = (*AuditStore)(nil)

// Append implements audit.Store.
func (s *AuditStore) Append(ev audit.Event) (int64, error) {
	var details []byte
	if len(ev.Details) > 0 {
		encoded, err := json.Marshal(ev.Details)
		if err != nil {
			return 0, fmt.Errorf("store: encode audit details: %w", err)
		}
		details = encoded
	}
	result, err := s.db.db.Exec(`
		INSERT INTO audit (
			at_ms, action, outcome, actor_device_id, actor_name, actor_scope,
			session_id, target, remote_addr, details
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		millis(ev.At), string(ev.Action), string(ev.Outcome),
		ev.ActorDeviceID, ev.ActorName, ev.ActorScope,
		ev.SessionID, ev.Target, ev.RemoteAddr, details)
	if err != nil {
		return 0, fmt.Errorf("store: append audit entry: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: audit row id: %w", err)
	}
	return id, nil
}

// Query implements audit.Store: the filters become an index-friendly WHERE clause and
// the rows come back newest first.
func (s *AuditStore) Query(filter audit.Filter) ([]audit.Event, error) {
	var conditions []string
	var args []any
	if !filter.Since.IsZero() {
		conditions = append(conditions, "at_ms > ?")
		args = append(args, millis(filter.Since))
	}
	if filter.Action != "" {
		conditions = append(conditions, "action = ?")
		args = append(args, string(filter.Action))
	}
	if filter.DeviceID != "" {
		conditions = append(conditions, "actor_device_id = ?")
		args = append(args, filter.DeviceID)
	}
	if filter.SessionID != "" {
		conditions = append(conditions, "session_id = ?")
		args = append(args, filter.SessionID)
	}
	query := `SELECT id, at_ms, action, outcome, actor_device_id, actor_name, actor_scope,
	                 session_id, target, remote_addr, details
	          FROM audit`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY id DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query audit: %w", err)
	}
	defer rows.Close()

	events := make([]audit.Event, 0)
	for rows.Next() {
		var (
			event       audit.Event
			atMS        int64
			action      string
			outcome     string
			details     []byte
			actorDevice string
			actorName   string
			actorScope  string
			sessionID   string
			target      string
			remoteAddr  string
		)
		if err := rows.Scan(&event.ID, &atMS, &action, &outcome, &actorDevice, &actorName,
			&actorScope, &sessionID, &target, &remoteAddr, &details); err != nil {
			return nil, fmt.Errorf("store: scan audit row: %w", err)
		}
		event.At = timeFromMillis(atMS)
		event.Action = audit.Action(action)
		event.Outcome = audit.Outcome(outcome)
		event.ActorDeviceID = actorDevice
		event.ActorName = actorName
		event.ActorScope = actorScope
		event.SessionID = sessionID
		event.Target = target
		event.RemoteAddr = remoteAddr
		if len(details) > 0 {
			if err := json.Unmarshal(details, &event.Details); err != nil {
				// A row nobody can decode is still a row that happened: keep it with an
				// empty context instead of failing the whole page.
				event.Details = map[string]any{"detailEncoding": "unreadable"}
			}
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read audit: %w", err)
	}
	return events, nil
}

// Prune implements audit.Store.
func (s *AuditStore) Prune(before time.Time) (int, error) {
	result, err := s.db.db.Exec(`DELETE FROM audit WHERE at_ms < ?`, millis(before))
	if err != nil {
		return 0, fmt.Errorf("store: prune audit: %w", err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: prune audit: %w", err)
	}
	return int(removed), nil
}
