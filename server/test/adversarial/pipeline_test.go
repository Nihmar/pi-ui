package adversarial_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

// TestPipeline_SeparatorsAndCRLFEndToEnd drives U+2028/U+2029 and CRLF through
// the whole path — child stdout → rpc framing → sessions → hub → WebSocket — and
// checks the client decodes the exact code points the child wrote. The child is
// fake-pi with --separators --crlf, so the records are the harness's own.
func TestPipeline_SeparatorsAndCRLFEndToEnd(t *testing.T) {
	stack := newStack(t, nil)
	info := stack.startSession(t, t.TempDir(), "--separators", "--crlf", "--emit", "3")

	client := stack.mustDial(nil)
	welcome := client.hello()
	if string(welcome["v"]) != "1" {
		t.Fatalf("welcome v = %s, want 1", welcome["v"])
	}
	client.send(`{"type":"subscribe","sessionId":"` + info.ID + `"}`)
	client.send(`{"type":"command","id":"c1","sessionId":"` + info.ID + `","op":"session.prompt","payload":{"message":"hi"}}`)

	sep, par := "\u2028", "\u2029"
	update := client.mustNext("pi.message_update with separators", hasType("pi.message_update"))
	payload := payloadOf(t, update)
	delta := fieldString(fieldObject(t, payload, "assistantMessageEvent"), "delta")
	if !strings.HasSuffix(delta, sep+par) {
		t.Fatalf("delta does not end in the literal U+2028/U+2029 the child wrote: %q", delta)
	}

	settled := client.mustNext("pi.agent_settled", hasType("pi.agent_settled"))
	if got := fieldString(payloadOf(t, settled), "piuiSeparators"); got != sep+par {
		t.Fatalf("piuiSeparators = %q, want %q", got, sep+par)
	}
}

// TestPipeline_BigRecordReachesAClient sends an 8 MiB record through the whole
// pipeline and asserts the client decodes it byte-exact: the frame spans many
// reads on both sides and the hub's queue carries it whole.
func TestPipeline_BigRecordReachesAClient(t *testing.T) {
	stack := newStack(t, func(o *stackOptions) { o.piArgs = []string{"--big", strconv.Itoa(bigRecordBytes)} })
	info := stack.startSession(t, t.TempDir())

	client := stack.mustDial(nil)
	client.hello()
	client.send(`{"type":"subscribe","sessionId":"` + info.ID + `"}`)
	client.send(`{"type":"command","id":"c1","sessionId":"` + info.ID + `","op":"session.prompt","payload":{"message":"hi"}}`)

	update := client.mustNext("the 8 MiB pi.message_update", hasType("pi.message_update"))
	payload := payloadOf(t, update)
	delta := fieldString(fieldObject(t, payload, "assistantMessageEvent"), "delta")
	if len(delta) != bigRecordBytes {
		t.Fatalf("delta length = %d, want %d", len(delta), bigRecordBytes)
	}
	if strings.Trim(delta, "x") != "" {
		t.Fatalf("delta is not the exact run of x the child wrote")
	}

	// The stream keeps flowing after the big frame: agent_end and the settlement
	// event follow it.
	client.mustNext("pi.agent_settled after the big record", hasType("pi.agent_settled"))
}

// TestPipeline_UnknownEventTypeIsForwardedVerbatim pins the lenient pass-through
// rule: an event type this server has never heard of reaches the client with its
// payload intact instead of being dropped or rejected.
func TestPipeline_UnknownEventTypeIsForwardedVerbatim(t *testing.T) {
	script := fakeharness.Script{
		Commands: map[string]fakeharness.CommandScript{
			"prompt": {Events: []fakeharness.Step{
				{Record: []byte(`{"type":"future_event","x":1}`)},
			}},
		},
	}
	stack := newStack(t, func(o *stackOptions) { o.script = &script })
	info := stack.startSession(t, t.TempDir())

	client := stack.mustDial(nil)

	client.hello()
	client.send(`{"type":"subscribe","sessionId":"` + info.ID + `"}`)
	client.send(`{"type":"command","id":"c1","sessionId":"` + info.ID + `","op":"session.prompt","payload":{"message":"hi"}}`)

	event := client.mustNext("pi.future_event", hasType("pi.future_event"))
	if got := string(payloadOf(t, event)["x"]); got != "1" {
		t.Fatalf("future_event payload x = %s, want 1", got)
	}
}

// TestPipeline_InvalidJSONRecordIsSurfacedNotSwallowed is the adversarial case
// for a child that writes a line which is not JSON at all (the rpc layer passes
// it through with an empty type, per its contract).
//
// FINDING: the record does not reach the client as a pass-through event. sessions
// publishes it as pi.unknown with the raw bytes as payload, and the hub's
// marshalEvent refuses a payload that is not valid JSON: the subscriber gets
// server.error{code:"internal"} ("event payload could not be encoded") and the
// raw bytes are lost on the WS leg. This test pins the observed fallback — the
// hub must not write a frame no client could parse, and one dirty line must not
// poison the stream — and docs/verification-report.md records the deviation from
// "invalid JSON is tolerated as pass-through". rpc-level pass-through is asserted
// by TestFraming_SeparatorsCRLFSplitAndInvalidPassThrough.
func TestPipeline_InvalidJSONRecordIsSurfacedNotSwallowed(t *testing.T) {
	fakePi := fakeharness.Build(t)
	script := fakeharness.Script{}
	scriptPath := fakeharness.WriteScript(t, script)
	shell := shellPath(t)
	dirty := writeFile(t, "dirty-child.sh", "#!/bin/sh\nprintf 'this is not json\\n'\nexec "+
		shellQuote(fakePi)+" --script "+shellQuote(scriptPath)+" --emit 2 \"$@\"\n")

	stack := newStack(t, func(o *stackOptions) {
		o.script = &script
		o.piCommand = []string{shell, dirty}
	})
	info, resp, data := stack.createSession(t.TempDir(), "")
	wantStatus(t, resp, data, 201)

	client := stack.mustDial(nil)
	client.hello()
	client.send(`{"type":"subscribe","sessionId":"` + info.ID + `","since":{"seq":0}}`)
	client.mustNext("server.replay.begin", hasType("server.replay.begin"))

	var sawInternalError, sawUnknown bool
	deadline := time.Now().Add(waitTimeout)
	replayDone := false
	for !replayDone && time.Now().Before(deadline) {
		frame, err := client.next(time.Until(deadline))
		if err != nil {
			t.Fatalf("reading the replay: %v (frames read: %v)", err, client.frames)
		}
		switch frameType(frame) {
		case "server.error":
			if errorCodeOf(frame) == "internal" {
				sawInternalError = true
			}
		case "pi.unknown":
			sawUnknown = true
		case "server.replay.end":
			replayDone = true
		}
	}
	if !replayDone {
		t.Fatalf("the replay never ended (frames read: %v)", client.frames)
	}
	if !sawInternalError {
		t.Errorf("no server.error{internal} reached the client: the unencodable record was dropped silently (frames: %v)", client.frames)
	}
	if sawUnknown {
		t.Errorf("a pi.unknown frame arrived: the raw invalid record was encodable after all (frames: %v)", client.frames)
	}

	// One dirty line must not poison the connection or the session: a prompt still
	// round-trips and its events still arrive.
	client.send(`{"type":"command","id":"c1","sessionId":"` + info.ID + `","op":"session.prompt","payload":{"message":"hi"}}`)
	response := client.mustNext("the prompt response", hasResponse("c1"))
	if !fieldBool(t, response, "ok") {
		t.Fatalf("prompt response = %v, want ok:true", response)
	}
	client.mustNext("pi.agent_settled", hasType("pi.agent_settled"))
}
