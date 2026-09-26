package ws

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/Nihmar/pi-ui/server/internal/protocol/gen"
)

// schemaCompiler compiles one embedded schema from schemas/ and returns the
// compiler with its $id, so a test can validate against any $defs entry.
func schemaCompiler(t *testing.T, name string) (*jsonschema.Compiler, string) {
	t.Helper()

	data, ok := gen.SchemaJSON(name)
	if !ok {
		t.Fatalf("gen.SchemaJSON(%q) not found; run server/scripts/gen.sh", name)
	}
	var meta struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("read $id of %q: %v", name, err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode %q: %v", name, err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(meta.ID, doc); err != nil {
		t.Fatalf("AddResource(%s): %v", meta.ID, err)
	}
	return compiler, meta.ID
}

// mustValidate compiles "$id#/$defs/<def>" and fails the test when raw does not
// match it.
func mustValidate(t *testing.T, compiler *jsonschema.Compiler, base, def string, raw []byte) {
	t.Helper()

	schema, err := compiler.Compile(base + "#/$defs/" + def)
	if err != nil {
		t.Fatalf("Compile(%s#/$defs/%s): %v", base, def, err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode instance for %s: %v", def, err)
	}
	if err := schema.Validate(instance); err != nil {
		t.Fatalf("%s does not match %s#/$defs/%s: %v\n%s", def, base, def, err, raw)
	}
}

// enumOf reads one enum from a schema $defs entry.
func enumOf(t *testing.T, name, def string) []string {
	t.Helper()

	data, _ := gen.SchemaJSON(name)
	var doc struct {
		Defs map[string]struct {
			Enum []string `json:"enum"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode %q: %v", name, err)
	}
	entry, ok := doc.Defs[def]
	if !ok {
		t.Fatalf("%s has no $defs/%s", name, def)
	}
	return entry.Enum
}

// TestErrorCodeTaxonomyMatchesCore is the drift guard for the one shape ws.json
// re-declares in full: the codes a client branches on must be the same list in both
// files, or a client would reject a frame the server considers valid.
func TestErrorCodeTaxonomyMatchesCore(t *testing.T) {
	core := enumOf(t, "core", "ErrorCode")
	wsEnum := enumOf(t, "ws", "WsErrorCode")

	if len(core) == 0 || len(wsEnum) == 0 {
		t.Fatalf("empty enums: core=%v ws=%v", core, wsEnum)
	}
	inCore := make(map[string]bool, len(core))
	for _, code := range core {
		inCore[code] = true
	}
	inWS := make(map[string]bool, len(wsEnum))
	for _, code := range wsEnum {
		inWS[code] = true
	}
	for code := range inCore {
		if !inWS[code] {
			t.Errorf("error code %q is in core.json but missing from ws.json", code)
		}
	}
	for code := range inWS {
		if !inCore[code] {
			t.Errorf("error code %q is in ws.json but missing from core.json", code)
		}
	}
}

// TestOutboundFramesMatchTheSchema validates the frames the hub writes. The hub is
// the last gate before the wire, so the shapes it builds have to be the shapes the
// schema documents — the inbound direction is already covered by validateFrame.
func TestOutboundFramesMatchTheSchema(t *testing.T) {
	compiler, base := schemaCompiler(t, "ws")
	h := New(withDefaults(Options{
		ServerVersion: "0.0.1-test",
		PiVersion:     "0.87.1",
		Features:      []string{"bridge"},
		Limits:        map[string]any{"maxSessions": 8},
	})).(*hub)
	defer h.Close()

	mustValidate(t, compiler, base, "WsWelcome", h.marshalWelcome())
	mustValidate(t, compiler, base, "WsPong", marshalPong())
	mustValidate(t, compiler, base, "WsResponse", marshalResponse("c1", json.RawMessage(`{"accepted":true}`)))
	mustValidate(t, compiler, base, "WsResponse", marshalResponse("u1", nil))
	mustValidate(t, compiler, base, "WsResponse", marshalErrorResponse("c1", codeBadRequest, "nope"))
	mustValidate(t, compiler, base, "WsRequest", validRequestFrame("s_0123456789abcdef"))

	event := Event{
		Type:      "pi.entry_appended",
		SessionID: "s_0123456789abcdef",
		Seq:       h.nextSeq(),
		EntryID:   "e-1",
		TS:        h.timestamp(),
		Payload:   json.RawMessage(`{"entry":{"id":"e-1"}}`),
	}
	raw, err := marshalEvent(event)
	if err != nil {
		t.Fatalf("marshalEvent: %v", err)
	}
	mustValidate(t, compiler, base, "WsEvent", raw)

	failure := h.errorEvent(codeSlowConsumer, "subscriber is not keeping up")
	raw, err = marshalEvent(failure)
	if err != nil {
		t.Fatalf("marshalEvent(error): %v", err)
	}
	mustValidate(t, compiler, base, "WsEvent", raw)

	// A server-wide event has no sessionId, and the schema has to allow that.
	serverWide := h.localEvent(EventHeartbeat, "", mustMarshal(map[string]any{"uptimeSec": 1}))
	raw, err = marshalEvent(serverWide)
	if err != nil {
		t.Fatalf("marshalEvent(heartbeat): %v", err)
	}
	mustValidate(t, compiler, base, "WsEvent", raw)
}

// TestReplayFramesMatchTheSchema: the subscriber-local replay frames are events too,
// and a client parses them with the same reader.
func TestReplayFramesMatchTheSchema(t *testing.T) {
	compiler, base := schemaCompiler(t, "ws")
	h := New(withDefaults(Options{})).(*hub)
	defer h.Close()

	const session = "s_0123456789abcdef"
	mustValidate(t, compiler, base, "WsEvent", mustMarshalEvent(h.localEvent(EventReplayBegin, session, replayBeginPayload("seq"))))
	mustValidate(t, compiler, base, "WsEvent", mustMarshalEvent(h.localEvent(EventReplayBegin, session, replayBeginPayload("entry"))))
	mustValidate(t, compiler, base, "WsEvent", mustMarshalEvent(h.localEvent(EventReplayEnd, session, replayEndPayload(3, true, true))))
	mustValidate(t, compiler, base, "WsEvent", mustMarshalEvent(h.localEvent(EventReplayEnd, session, replayEndPayload(0, false, false))))

	// A replay end without truncation must not carry the key at all: a client reads
	// its presence as "events were lost".
	frame := mustMarshalEvent(h.localEvent(EventReplayEnd, session, replayEndPayload(0, true, false)))
	if bytes.Contains(frame, []byte("truncated")) {
		t.Fatalf("replay.end without truncation mentions it: %s", frame)
	}
}

// TestInboundClientFramesMatchTheSchema pins the six client frames of §6, so a schema
// edit that breaks one of them fails here with a readable message.
func TestInboundClientFramesMatchTheSchema(t *testing.T) {
	const session = "s_0123456789abcdef"
	h := New(withDefaults(Options{})).(*hub)
	defer h.Close()

	valid := []string{
		`{"type":"hello","v":1,"client":{"name":"pi-ui-test","version":"0.0.1"}}`,
		`{"type":"subscribe","sessionId":"` + session + `"}`,
		`{"type":"subscribe","sessionId":"` + session + `","since":{"seq":0},"replay":true}`,
		`{"type":"subscribe","sessionId":"` + session + `","since":{"entryId":"e-1"}}`,
		`{"type":"unsubscribe","sessionId":"` + session + `"}`,
		`{"type":"command","id":"c1","sessionId":"` + session + `","op":"session.prompt","payload":{"message":"hi"}}`,
		`{"type":"command","id":"c1","sessionId":"` + session + `","op":"session.command.raw"}`,
		`{"type":"ui_response","sessionId":"` + session + `","id":"u1","value":"Allow"}`,
		`{"type":"ui_response","sessionId":"` + session + `","id":"u1","confirmed":true}`,
		`{"type":"ui_response","sessionId":"` + session + `","id":"u1","cancelled":true}`,
		`{"type":"ping"}`,
	}
	for _, raw := range valid {
		if !h.validateFrame([]byte(raw)) {
			t.Errorf("valid client frame rejected: %s", raw)
		}
	}

	invalid := []string{
		`{"type":"hello","v":2,"client":{"name":"c","version":"1"}}`,
		`{"type":"subscribe"}`,
		`{"type":"subscribe","sessionId":"` + session + `","since":{}}`,
		`{"type":"command","id":"c1","sessionId":"` + session + `","op":"Not Dotted"}`,
		`{"type":"command","sessionId":"` + session + `","op":"session.abort"}`,
		`{"type":"ui_response","sessionId":"` + session + `","id":"u1"}`,
		`{"type":"pong"}`,
		`{"type":"event","seq":1,"ts":"2025-01-02T03:04:05.678Z"}`,
	}
	for _, raw := range invalid {
		if h.validateFrame([]byte(raw)) {
			t.Errorf("invalid client frame accepted: %s", raw)
		}
	}
}

// TestOptionsDefaultsAreApplied pins the documented defaults, including the two that
// reach the client through the welcome frame.
func TestOptionsDefaultsAreApplied(t *testing.T) {
	resolved := withDefaults(Options{})

	if resolved.ReplayEvents != defaultReplayEvents {
		t.Errorf("ReplayEvents = %d, want %d", resolved.ReplayEvents, defaultReplayEvents)
	}
	if resolved.ReplayWindow != defaultReplayWindow {
		t.Errorf("ReplayWindow = %v, want %v", resolved.ReplayWindow, defaultReplayWindow)
	}
	if resolved.Heartbeat != defaultHeartbeat {
		t.Errorf("Heartbeat = %v, want %v", resolved.Heartbeat, defaultHeartbeat)
	}
	if resolved.WriteTimeout != defaultWriteTimeout {
		t.Errorf("WriteTimeout = %v, want %v", resolved.WriteTimeout, defaultWriteTimeout)
	}
	if resolved.SendBuffer != defaultSendBuffer {
		t.Errorf("SendBuffer = %d, want %d", resolved.SendBuffer, defaultSendBuffer)
	}
	if resolved.Features == nil || resolved.Limits == nil {
		t.Fatal("Features and Limits must be non-nil: the welcome schema requires an array and an object")
	}
}

// TestOptionsCollectionsAreCopied: a caller that keeps a reference to its slices must
// not be able to change a running hub.
func TestOptionsCollectionsAreCopied(t *testing.T) {
	hosts := []string{"pi-ui.local"}
	features := []string{"bridge"}
	limits := map[string]any{"maxSessions": 8}

	resolved := withDefaults(Options{AllowHosts: hosts, Features: features, Limits: limits})
	hosts[0] = "evil.example"
	features[0] = "broken"
	limits["maxSessions"] = 1

	if resolved.AllowHosts[0] != "pi-ui.local" {
		t.Error("AllowHosts was not copied")
	}
	if resolved.Features[0] != "bridge" {
		t.Error("Features was not copied")
	}
	if resolved.Limits["maxSessions"] != 8 {
		t.Error("Limits was not copied")
	}
}
