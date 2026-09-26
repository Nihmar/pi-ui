package ws

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Frame type names of the v1 protocol. They mirror schemas/ws.json, which is the
// source of truth: these constants exist so the routing code and the tests do not
// carry bare string literals.
const (
	frameHello       = "hello"
	frameSubscribe   = "subscribe"
	frameUnsubscribe = "unsubscribe"
	frameCommand     = "command"
	frameUIResponse  = "ui_response"
	framePing        = "ping"

	frameWelcome  = "welcome"
	frameRequest  = "request"
	frameResponse = "response"
	framePong     = "pong"
)

// tsLayout is RFC3339 with milliseconds in UTC, the format schemas/ws.json pins
// for every ts field. Milliseconds, because an ordering a client can read off the
// wire is worth more than sub-millisecond precision it cannot use.
const tsLayout = "2006-01-02T15:04:05.000Z07:00"

// inbound is the routing view of a client frame. The schema decides whether a
// frame is acceptable; this struct only reads the fields the hub acts on, so an
// unknown field of a newer client is ignored here exactly as the schema allows.
type inbound struct {
	Type      string          `json:"type"`
	V         int             `json:"v"`
	ID        string          `json:"id"`
	SessionID string          `json:"sessionId"`
	Op        string          `json:"op"`
	Payload   json.RawMessage `json:"payload"`
	Since     json.RawMessage `json:"since"`
	Replay    *bool           `json:"replay"`
	Value     json.RawMessage `json:"value"`
	Confirmed *bool           `json:"confirmed"`
	Cancelled *bool           `json:"cancelled"`
	Client    *struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"client"`
}

// replayCursor is the decoded `since` field of a subscribe frame.
type replayCursor struct {
	hasSeq   bool
	seq      uint64
	hasEntry bool
	entryID  string
}

// requested reports whether the client named a point to resume from. Without one
// the server replays what the ring still holds instead of reporting a gap.
func (c replayCursor) requested() bool { return c.hasSeq || c.hasEntry }

// parseCursor decodes `since`, trusting the schema for exclusivity but still
// checking: a cursor that names neither point would otherwise silently turn a
// replay into a live-only subscription.
func parseCursor(raw json.RawMessage) (replayCursor, error) {
	if len(raw) == 0 {
		return replayCursor{}, nil
	}
	var decoded struct {
		Seq     *uint64 `json:"seq"`
		EntryID *string `json:"entryId"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return replayCursor{}, fmt.Errorf("ws: decode since: %w", err)
	}
	var cur replayCursor
	if decoded.Seq != nil {
		cur.hasSeq, cur.seq = true, *decoded.Seq
	}
	if decoded.EntryID != nil {
		cur.hasEntry, cur.entryID = true, *decoded.EntryID
	}
	return cur, nil
}

// answer builds the minimal answer object handed to the DialogHandler from a
// ui_response frame: only the keys the client actually sent, so the handler can
// tell "confirmed:false" from "cancelled:true".
func (f inbound) answer() json.RawMessage {
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	write := func(key string, raw []byte) {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteString(`"` + key + `":`)
		buf.Write(raw)
	}
	if len(f.Value) > 0 {
		write("value", f.Value)
	}
	if f.Confirmed != nil {
		write("confirmed", []byte(fmt.Sprintf("%t", *f.Confirmed)))
	}
	if f.Cancelled != nil {
		write("cancelled", []byte(fmt.Sprintf("%t", *f.Cancelled)))
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

// validateFrame reports whether raw is a well-formed client frame. It is the only
// gate between the socket and the handlers: everything downstream may assume the
// shapes schemas/ws.json describes.
func (h *hub) validateFrame(raw []byte) bool {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return false
	}
	return h.frames.Validate(doc) == nil
}

// frameID extracts the correlation id of a frame that may be malformed, so a
// rejected frame can still be answered. A frame without a readable id is dropped:
// there is nothing to correlate an answer with.
func frameID(raw []byte) string {
	var probe struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	return probe.ID
}

// outWelcome is the handshake response (schemas/ws.json#/$defs/WsWelcome).
type outWelcome struct {
	Type         string        `json:"type"`
	V            int           `json:"v"`
	Server       outServerInfo `json:"server"`
	HeartbeatSec int           `json:"heartbeatSec"`
}

// outServerInfo mirrors the server field of the welcome frame.
type outServerInfo struct {
	Version   string         `json:"version"`
	PiVersion string         `json:"piVersion"`
	Features  []string       `json:"features"`
	Limits    map[string]any `json:"limits"`
}

// outError is the error object of a failed response and of server.error.
type outError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// outResponse is the terminal outcome of one request
// (schemas/ws.json#/$defs/WsResponse).
type outResponse struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *outError       `json:"error,omitempty"`
}

// marshalWelcome builds the handshake response.
//
// heartbeatSec is rounded up and never below 1 because the schema requires a
// whole second; a test that runs the heartbeat at 10ms still gets an honest
// "expect a ping at least once a second".
func (h *hub) marshalWelcome() []byte {
	sec := int(h.opts.Heartbeat / time.Second)
	if h.opts.Heartbeat%time.Second != 0 {
		sec++
	}
	if sec < 1 {
		sec = 1
	}
	return mustMarshal(outWelcome{
		Type: frameWelcome,
		V:    protocolVersion,
		Server: outServerInfo{
			Version:   h.opts.ServerVersion,
			PiVersion: h.opts.PiVersion,
			Features:  h.opts.Features,
			Limits:    h.opts.Limits,
		},
		HeartbeatSec: sec,
	})
}

// marshalPong answers a client ping.
func marshalPong() []byte {
	return mustMarshal(struct {
		Type string `json:"type"`
	}{Type: framePong})
}

// marshalResponse builds a successful response. Nil data is omitted, which is
// what the dialog path returns.
func marshalResponse(id string, data json.RawMessage) []byte {
	return mustMarshal(outResponse{Type: frameResponse, ID: id, OK: true, Data: data})
}

// marshalErrorResponse builds a failed response.
func marshalErrorResponse(id, code, message string) []byte {
	return mustMarshal(outResponse{Type: frameResponse, ID: id, Error: &outError{Code: code, Message: message}})
}

// marshalEvent encodes one event frame. It refuses a payload that is not valid
// JSON instead of writing a frame no client could parse, so the caller can
// substitute a server.error.
func marshalEvent(ev Event) ([]byte, error) {
	if len(ev.Payload) > 0 && !json.Valid(ev.Payload) {
		return nil, fmt.Errorf("ws: event %q carries a payload that is not valid JSON", ev.Type)
	}
	return json.Marshal(ev)
}

// mustMarshal encodes a frame the hub built itself. nil means "nothing to send",
// which enqueue drops: the alternative would be a panic inside a publisher's
// goroutine over a payload that came from the wire.
func mustMarshal(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}

// mustMarshalEvent is mustMarshal for events.
func mustMarshalEvent(ev Event) []byte {
	data, err := marshalEvent(ev)
	if err != nil {
		return nil
	}
	return data
}

// replayBeginPayload is the payload of server.replay.begin. direction is "seq"
// for a ring replay and "entry" for a durable one, so a client can tell which
// cursor was honoured.
func replayBeginPayload(direction string) json.RawMessage {
	return mustMarshal(struct {
		Direction string `json:"direction"`
	}{Direction: direction})
}

// replayEndPayload is the payload of server.replay.end. truncated matters only
// for a ring replay and is omitted when false.
func replayEndPayload(count int, complete, truncated bool) json.RawMessage {
	return mustMarshal(struct {
		Count     int  `json:"count"`
		Complete  bool `json:"complete"`
		Truncated bool `json:"truncated,omitempty"`
	}{Count: count, Complete: complete, Truncated: truncated})
}
