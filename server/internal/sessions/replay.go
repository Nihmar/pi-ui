package sessions

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// entryEnvelope is the payload of one pi.entry_appended event: the durable entry under the
// documented {"entry": {...}} shape (§5.3).
type entryEnvelope struct {
	Entry json.RawMessage `json:"entry"`
}

// ReplayFromEntry implements ws.Replayer (§5.3, §7). The durable cursor is pi's own entry
// id, so the hub delegates here: this method calls get_entries{since} and emits one
// pi.entry_appended event per entry, in child order, with the entry id as the cursor.
//
// An unknown cursor is not an error the client can act on: it is returned as a coded
// replay_cursor_invalid error with complete=false, which tells the client to reload
// through REST instead of replaying. The hub reports the returned error to the replaying
// connection before replay.end, so the failure never enters the session event stream
// where every other subscriber of the session would see it.
func (m *Manager) ReplayFromEntry(ctx context.Context, sessionID, entryID string, emit func(ws.Event)) (bool, error) {
	s, ok := m.find(sessionID)
	if !ok {
		return false, Codedf(CodeSessionNotFound, "no session %q", sessionID)
	}

	data, err := s.callPi(ctx, commandTypeGetEntries, map[string]any{"since": entryID})
	if err != nil {
		var coded *CodedError
		if errors.As(err, &coded) && coded.Code == CodePiRejected {
			return false, Codedf(CodeReplayCursorInvalid, "%s", coded.Msg)
		}
		return false, err
	}
	if len(data) == 0 {
		return true, nil
	}

	var page struct {
		Entries []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return false, Codedf(CodePiError, "get_entries payload is not readable: %v", err)
	}
	if emit == nil {
		return true, nil
	}
	for _, entry := range page.Entries {
		emit(ws.Event{
			Type:      ws.EventEntryAppended,
			SessionID: sessionID,
			EntryID:   entryCursorOf(entry),
			Payload:   mustJSON(entryEnvelope{Entry: entry}),
		})
	}
	return true, nil
}

// entryCursorOf returns the id of one durable session entry — the cursor a client passes
// back as since.entryId — or "" when the entry carries none.
func entryCursorOf(entry json.RawMessage) string {
	var fields struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(entry, &fields); err != nil {
		return ""
	}
	return fields.ID
}
