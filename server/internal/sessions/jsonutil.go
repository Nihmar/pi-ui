package sessions

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// entryAppendedType is the pi record type that carries a durable session entry; its
// payload.entry.id is the cursor a client replays from (§7).
const entryAppendedType = "entry_appended"

// timestamp formats the ts of every event and of the request frames (§6, §7): RFC3339 in
// UTC with millisecond precision.
func timestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// now returns the current timestamp in the wire format.
func now() string { return timestamp(time.Now()) }

// setField sets one field of a JSON object under construction. The value is marshalled,
// so a marshal failure is a programmer error; it is impossible for the scalar shapes used
// here and therefore ignored deliberately.
func setField(fields map[string]json.RawMessage, key string, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return
	}
	fields[key] = encoded
}

// decodeObject parses a JSON object into fields, tolerating unknown members (lenient
// pass-through) but rejecting anything that is not an object. An empty document is an
// empty object, because several ops legitimately carry no payload — and so is the JSON
// literal null, which would otherwise decode into a nil map that the callers' setField
// panics on (a client sending `"payload":null` must not be able to kill the server).
func decodeObject(what string, raw json.RawMessage, fields *map[string]json.RawMessage) error {
	*fields = map[string]json.RawMessage{}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, fields); err != nil {
		return Codedf(CodeBadRequest, "%s is not a JSON object: %v", what, err)
	}
	if *fields == nil {
		*fields = map[string]json.RawMessage{}
	}
	return nil
}

// decodeInto parses payload into target, tolerating unknown members. A payload that does
// not carry the expected shape is a bad_request.
func decodeInto(what string, payload json.RawMessage, target any) error {
	if len(strings.TrimSpace(string(payload))) == 0 {
		return Codedf(CodeBadRequest, "%s is missing", what)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return Codedf(CodeBadRequest, "%s is not readable: %v", what, err)
	}
	return nil
}

// entryIDOf extracts the replay cursor of one record: payload.entry.id for
// entry_appended, a top-level entryId for anything else that carries one, "" otherwise.
func entryIDOf(raw json.RawMessage, recordType string) string {
	var fields struct {
		EntryID string `json:"entryId"`
		Entry   struct {
			ID string `json:"id"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	if recordType == entryAppendedType && fields.Entry.ID != "" {
		return fields.Entry.ID
	}
	return fields.EntryID
}

// firstNonEmpty returns the first argument that is not empty.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// mustJSON marshals a payload that cannot fail to marshal; it panics instead of returning
// an error the caller would have to invent a policy for.
func mustJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("sessions: marshal %T: %v", value, err))
	}
	return data
}
