package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/Nihmar/pi-ui/server/internal/protocol/gen"
)

// binPath is the binary under test, built once for the whole package: the tests drive
// the real process over pipes, exactly like the server's RpcBridge will.
var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fake-pi-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake-pi tests: temp dir:", err)
		os.Exit(1)
	}
	binPath = filepath.Join(dir, "fake-pi")
	build := exec.Command("go", "build", "-trimpath", "-o", binPath, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "fake-pi tests: build:", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type runResult struct {
	stdout  string
	stderr  string
	code    int
	elapsed time.Duration
}

// runPipe runs the binary and closes its stdin after input, which is pi's shutdown.
func runPipe(t *testing.T, args []string, input string) runResult {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Stdin = strings.NewReader(input)
	return runCommand(t, cmd)
}

// runOpenStdin runs the binary with a stdin that never reaches EOF, so the run can only
// end through a fault timer or a signal.
func runOpenStdin(t *testing.T, args []string) runResult {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer reader.Close()
	defer writer.Close()

	cmd := exec.Command(binPath, args...)
	cmd.Stdin = reader
	return runCommand(t, cmd)
}

func runCommand(t *testing.T, cmd *exec.Cmd) runResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run %s: %v", binPath, err)
		}
		code = exitErr.ExitCode()
	}
	return runResult{stdout: stdout.String(), stderr: stderr.String(), code: code, elapsed: elapsed}
}

// recordMap is one decoded protocol record with its fields still raw, so tests can look
// at exactly what went over the wire.
type recordMap map[string]json.RawMessage

// records splits stdout into decoded records; a record that does not decode is a framing
// bug, not a test failure to ignore.
func records(t *testing.T, out string) []recordMap {
	t.Helper()
	trimmed := strings.TrimSuffix(out, "\n")
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")
	decoded := make([]recordMap, 0, len(lines))
	for i, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		var record recordMap
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("record %d does not decode as a JSON object: %v\nline: %q", i, err, line)
		}
		decoded = append(decoded, record)
	}
	return decoded
}

func (r recordMap) str(t *testing.T, key string) string {
	t.Helper()
	raw, ok := r[key]
	if !ok {
		t.Fatalf("record has no field %q: %v", key, r)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("field %q is not a string: %v", key, err)
	}
	return value
}

func (r recordMap) boolean(t *testing.T, key string) bool {
	t.Helper()
	raw, ok := r[key]
	if !ok {
		t.Fatalf("record has no field %q: %v", key, r)
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("field %q is not a boolean: %v", key, err)
	}
	return value
}

func (r recordMap) number(t *testing.T, key string) float64 {
	t.Helper()
	raw, ok := r[key]
	if !ok {
		t.Fatalf("record has no field %q: %v", key, r)
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("field %q is not a number: %v", key, err)
	}
	return value
}

// object decodes a nested object field into the same raw-field representation.
func (r recordMap) object(t *testing.T, key string) recordMap {
	t.Helper()
	raw, ok := r[key]
	if !ok {
		t.Fatalf("record has no field %q: %v", key, r)
	}
	var nested recordMap
	if err := json.Unmarshal(raw, &nested); err != nil {
		t.Fatalf("field %q is not an object: %v", key, err)
	}
	return nested
}

func writeScript(t *testing.T, script Script) string {
	t.Helper()
	data, err := json.Marshal(script)
	if err != nil {
		t.Fatalf("marshal script: %v", err)
	}
	path := filepath.Join(t.TempDir(), "script.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

func expectTypes(t *testing.T, decoded []recordMap, want ...string) {
	t.Helper()
	if len(decoded) != len(want) {
		t.Fatalf("got %d records, want %d: %v", len(decoded), len(want), decoded)
	}
	for i, record := range decoded {
		if got := record.str(t, "type"); got != want[i] {
			t.Fatalf("record %d has type %q, want %q", i, got, want[i])
		}
	}
}
func TestScriptReplayOrderAndAutoFill(t *testing.T) {
	script := Script{
		SessionID: "s-1",
		Startup: []Step{{Record: json.RawMessage(
			`{"type":"extension_ui_request","id":"u1","method":"notify","message":"ready"}`)}},
		Commands: map[string]CommandScript{
			"prompt": {
				DelayMs:  30,
				Response: json.RawMessage(`{"success":true}`),
				Events: []Step{
					{DelayMs: 20, Record: json.RawMessage(
						`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hi"}}`)},
					{Record: json.RawMessage(`{"type":"agent_end"}`)},
				},
			},
			"bash": {Response: json.RawMessage(`{"success":false,"command":"bash","error":"boom"}`)},
		},
	}
	input := "{\"id\":\"p1\",\"type\":\"prompt\",\"message\":\"hello\"}\n" +
		"{\"id\":\"b1\",\"type\":\"bash\",\"command\":\"echo hi\"}\n"

	result := runPipe(t, []string{"--script", writeScript(t, script)}, input)
	if result.code != 0 {
		t.Fatalf("exit code %d, want 0 (stderr: %s)", result.code, result.stderr)
	}

	decoded := records(t, result.stdout)
	expectTypes(t, decoded, "extension_ui_request", "response", "message_update", "agent_end", "response")

	startup := decoded[0]
	if got := startup.str(t, "method"); got != "notify" {
		t.Errorf("startup method = %q, want notify", got)
	}
	if got := startup.str(t, "message"); got != "ready" {
		t.Errorf("startup message = %q, want ready", got)
	}

	prompt := decoded[1]
	if got := prompt.str(t, "id"); got != "p1" {
		t.Errorf("auto-filled id = %q, want p1", got)
	}
	if got := prompt.str(t, "command"); got != "prompt" {
		t.Errorf("auto-filled command = %q, want prompt", got)
	}
	if !prompt.boolean(t, "success") {
		t.Error("prompt response is not successful")
	}
	if got := decoded[2].object(t, "assistantMessageEvent").str(t, "delta"); got != "hi" {
		t.Errorf("delta = %q, want hi", got)
	}

	bash := decoded[4]
	if got := bash.str(t, "command"); got != "bash" {
		t.Errorf("scripted command = %q, want bash", got)
	}
	if bash.boolean(t, "success") {
		t.Error("scripted failing response reports success")
	}
	if got := bash.str(t, "error"); got != "boom" {
		t.Errorf("scripted error = %q, want boom", got)
	}
	if got := bash.str(t, "id"); got != "b1" {
		t.Errorf("auto-filled bash id = %q, want b1", got)
	}

	if want := 50 * time.Millisecond; result.elapsed < want {
		t.Errorf("script delays were not honoured: %v elapsed, want >= %v", result.elapsed, want)
	}
}

func TestScriptDefaultEntry(t *testing.T) {
	script := Script{
		Default: &CommandScript{
			Response: json.RawMessage(`{"success":true,"data":{"ok":true}}`),
			Events:   []Step{{Record: json.RawMessage(`{"type":"thinking_level_changed","level":"off"}`)}},
		},
	}
	result := runPipe(t, []string{"--script", writeScript(t, script)}, "{\"id\":\"x1\",\"type\":\"set_thinking_level\"}\n")
	decoded := records(t, result.stdout)
	expectTypes(t, decoded, "response", "thinking_level_changed")

	if got := decoded[0].str(t, "command"); got != "set_thinking_level" {
		t.Errorf("command = %q, want set_thinking_level", got)
	}
	if got := decoded[0].str(t, "id"); got != "x1" {
		t.Errorf("id = %q, want x1", got)
	}
	if !bytes.Contains(decoded[0]["data"], []byte(`{"ok":true}`)) {
		t.Errorf("default data was not replayed verbatim: %s", decoded[0]["data"])
	}
}

// TestScriptExitCodeEndsTheRunAfterTheAnswer pins the deterministic fault: the answer is on
// stdout before the process exits, so a test can arm a crash after readiness by sending a
// command instead of racing a wall-clock timer.
func TestScriptExitCodeEndsTheRunAfterTheAnswer(t *testing.T) {
	code := 9
	script := Script{Commands: map[string]CommandScript{
		"abort": {ExitCode: &code},
	}}
	result := runPipe(t, []string{"--script", writeScript(t, script)}, "{\"id\":\"a1\",\"type\":\"abort\"}\n")

	decoded := records(t, result.stdout)
	expectTypes(t, decoded, "response")
	if got := decoded[0].str(t, "id"); got != "a1" {
		t.Errorf("response id = %q, want a1", got)
	}
	if !decoded[0].boolean(t, "success") {
		t.Error("the answer before the exit was a failure")
	}
	if result.code != code {
		t.Fatalf("exit code %d, want %d", result.code, code)
	}
}

// TestScriptRejectsAnImpossibleExitCode keeps a nonsense status out of a run: the script
// fails to load instead of exiting with a value the OS truncates.
func TestScriptRejectsAnImpossibleExitCode(t *testing.T) {
	code := 300
	script := Script{Commands: map[string]CommandScript{
		"abort": {ExitCode: &code},
	}}
	result := runPipe(t, []string{"--script", writeScript(t, script)}, "")
	if result.code != exitUsage {
		t.Fatalf("exit code %d, want %d", result.code, exitUsage)
	}
	if !strings.Contains(result.stderr, "exitCode") {
		t.Errorf("stderr = %q, want the offending field", result.stderr)
	}
}

func TestScriptedErrorForcesFailure(t *testing.T) {
	script := Script{Commands: map[string]CommandScript{
		"set_model": {Response: json.RawMessage(`{"success":true}`), Error: "Model not found: invalid/model"},
	}}
	result := runPipe(t, []string{"--script", writeScript(t, script)}, "{\"id\":\"m1\",\"type\":\"set_model\"}\n")
	decoded := records(t, result.stdout)
	expectTypes(t, decoded, "response")

	if decoded[0].boolean(t, "success") {
		t.Error("scripted error still reports success")
	}
	if got := decoded[0].str(t, "error"); got != "Model not found: invalid/model" {
		t.Errorf("error = %q", got)
	}
	if _, ok := decoded[0]["data"]; ok {
		t.Errorf("failing response carries data: %v", decoded[0])
	}
}

func TestBuiltinGetState(t *testing.T) {
	script := Script{SessionID: "s-42", SessionFile: "/tmp/s-42.jsonl"}
	result := runPipe(t, []string{"--script", writeScript(t, script)}, "{\"id\":\"s1\",\"type\":\"get_state\"}\n")
	decoded := records(t, result.stdout)
	expectTypes(t, decoded, "response")

	state := decoded[0].object(t, "data")
	want := map[string]string{
		"sessionId":     "s-42",
		"sessionFile":   "/tmp/s-42.jsonl",
		"thinkingLevel": "off",
		"steeringMode":  "one-at-a-time",
		"followUpMode":  "one-at-a-time",
	}
	for key, value := range want {
		if got := state.str(t, key); got != value {
			t.Errorf("state.%s = %q, want %q", key, got, value)
		}
	}
	for _, key := range []string{"isStreaming", "isCompacting"} {
		if state.boolean(t, key) {
			t.Errorf("state.%s is true, want false", key)
		}
	}
	if !state.boolean(t, "autoCompactionEnabled") {
		t.Error("state.autoCompactionEnabled is false, want true")
	}
	for _, key := range []string{"messageCount", "pendingMessageCount"} {
		if got := state.number(t, key); got != 0 {
			t.Errorf("state.%s = %v, want 0", key, got)
		}
	}
}

func TestBuiltinGetStateDefaults(t *testing.T) {
	result := runPipe(t, nil, "{\"id\":\"s1\",\"type\":\"get_state\"}\n")
	decoded := records(t, result.stdout)
	expectTypes(t, decoded, "response")

	state := decoded[0].object(t, "data")
	if got := state.str(t, "sessionId"); got != defaultSessionID {
		t.Errorf("sessionId = %q, want %q", got, defaultSessionID)
	}
	if got := state.str(t, "sessionFile"); got != defaultSessionFile {
		t.Errorf("sessionFile = %q, want %q", got, defaultSessionFile)
	}
}

func TestBuiltinGetEntriesPaging(t *testing.T) {
	script := Script{Entries: []json.RawMessage{
		json.RawMessage(`{"type":"message","id":"e1","message":{"role":"user"}}`),
		json.RawMessage(`{"type":"message","id":"e2","message":{"role":"assistant"}}`),
		json.RawMessage(`{"type":"message","id":"e3","message":{"role":"user"}}`),
	}}
	path := writeScript(t, script)

	t.Run("all entries", func(t *testing.T) {
		result := runPipe(t, []string{"--script", path}, "{\"id\":\"g1\",\"type\":\"get_entries\"}\n")
		data := records(t, result.stdout)[0].object(t, "data")

		entries := decodeEntries(t, data)
		if len(entries) != 3 {
			t.Fatalf("got %d entries, want 3", len(entries))
		}
		if got := entries[0].str(t, "id"); got != "e1" {
			t.Errorf("first entry id = %q, want e1", got)
		}
		if got := data.str(t, "leafId"); got != "e3" {
			t.Errorf("leafId = %q, want e3", got)
		}
	})

	t.Run("since cursor", func(t *testing.T) {
		result := runPipe(t, []string{"--script", path}, "{\"id\":\"g2\",\"type\":\"get_entries\",\"since\":\"e1\"}\n")
		data := records(t, result.stdout)[0].object(t, "data")

		entries := decodeEntries(t, data)
		if len(entries) != 2 {
			t.Fatalf("got %d entries after e1, want 2", len(entries))
		}
		if got := entries[0].str(t, "id"); got != "e2" {
			t.Errorf("first paged entry = %q, want e2", got)
		}
		if got := data.str(t, "leafId"); got != "e3" {
			t.Errorf("paging moved the leaf: got %q, want e3", got)
		}
	})

	t.Run("unknown cursor", func(t *testing.T) {
		result := runPipe(t, []string{"--script", path}, "{\"id\":\"g3\",\"type\":\"get_entries\",\"since\":\"nope\"}\n")
		decoded := records(t, result.stdout)
		if decoded[0].boolean(t, "success") {
			t.Error("unknown cursor reports success")
		}
		if got := decoded[0].str(t, "error"); !strings.Contains(got, "nope") {
			t.Errorf("error %q does not name the cursor", got)
		}
	})

	t.Run("empty session", func(t *testing.T) {
		result := runPipe(t, nil, "{\"id\":\"g4\",\"type\":\"get_entries\"}\n")
		data := records(t, result.stdout)[0].object(t, "data")
		if got := string(data["entries"]); got != "[]" {
			t.Errorf("entries = %s, want []", got)
		}
		if got := string(data["leafId"]); got != "null" {
			t.Errorf("leafId = %s, want null", got)
		}
	})
}

func decodeEntries(t *testing.T, data recordMap) []recordMap {
	t.Helper()
	raw, ok := data["entries"]
	if !ok {
		t.Fatalf("get_entries data has no entries: %v", data)
	}
	var entries []recordMap
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("entries do not decode: %v", err)
	}
	return entries
}

func TestBuiltinGetCommandsAndUnknownCommand(t *testing.T) {
	input := "{\"id\":\"c1\",\"type\":\"get_commands\"}\n{\"id\":\"o1\",\"type\":\"set_auto_retry\"}\n"
	result := runPipe(t, nil, input)
	decoded := records(t, result.stdout)
	expectTypes(t, decoded, "response", "response")

	if got := string(decoded[0].object(t, "data")["commands"]); got != "[]" {
		t.Errorf("commands = %s, want []", got)
	}
	if !decoded[1].boolean(t, "success") {
		t.Error("unknown command did not succeed")
	}
	if _, ok := decoded[1]["data"]; ok {
		t.Errorf("unknown command carries data: %v", decoded[1])
	}
	if got := decoded[1].str(t, "command"); got != "set_auto_retry" {
		t.Errorf("command = %q, want set_auto_retry", got)
	}
}

func TestSyntheticPromptStream(t *testing.T) {
	// 50 records/s is a 20 ms interval: three waits plus slack is unmistakable.
	result := runPipe(t, []string{"--emit", "4", "--rate", "50"}, "{\"id\":\"p1\",\"type\":\"prompt\"}\n")
	decoded := records(t, result.stdout)
	expectTypes(t, decoded, "response", "message_update", "message_update", "message_update", "message_update", "agent_end", "agent_settled")

	for i := 0; i < 4; i++ {
		update := decoded[i+1]
		event := update.object(t, "assistantMessageEvent")
		if got := event.str(t, "type"); got != "text_delta" {
			t.Errorf("update %d event type = %q, want text_delta", i, got)
		}
		if want := fmt.Sprintf("chunk-%d", i); event.str(t, "delta") != want {
			t.Errorf("update %d delta = %q, want %q", i, event.str(t, "delta"), want)
		}
		spike := update.object(t, "spike")
		if got := spike.number(t, "i"); got != float64(i) {
			t.Errorf("update %d spike.i = %v, want %d", i, got, i)
		}
		if spike.number(t, "ns") <= 0 {
			t.Errorf("update %d has no nanosecond timestamp", i)
		}
	}
	if want := 45 * time.Millisecond; result.elapsed < want {
		t.Errorf("--rate did not pace the stream: %v elapsed, want >= %v", result.elapsed, want)
	}
}

// TestSyntheticPromptStreamCatchesUp is the counterpart of TestSyntheticPromptStream:
// --rate must hold even when the interval is finer than the host's timer granularity
// (250µs here; common hosts fire a 200µs ticker at ~1ms, which is why the emitter paces
// on a deadline schedule). A ticker or a per-record sleep would deliver this stream at
// ~1000 records/s, i.e. in ~2s instead of the ~0.5s the schedule takes.
func TestSyntheticPromptStreamCatchesUp(t *testing.T) {
	const (
		events = 2000
		rate   = 4000 // 250µs per record
	)
	result := runPipe(t, []string{"--emit", fmt.Sprint(events), "--rate", fmt.Sprint(rate)}, "{\"id\":\"p1\",\"type\":\"prompt\"}\n")
	if got := len(records(t, result.stdout)); got != events+3 {
		t.Errorf("record count = %d, want %d (response, updates, agent_end, agent_settled)", got, events+3)
	}
	if want := 1 * time.Second; result.elapsed > want {
		t.Errorf("--rate did not catch up: %v elapsed for %d records at %d/s, want <= %v", result.elapsed, events, rate, want)
	}
}

// TestNewPacerRejectsUnusableRates pins every rate that must not become a schedule: zero
// and negative (unpaced by contract), NaN and the infinities (valid float64 values whose
// time.Duration conversion is implementation-defined) and a rate so high that the
// interval rounds to zero.
func TestNewPacerRejectsUnusableRates(t *testing.T) {
	for _, rate := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1), 1e12} {
		if p := newPacer(rate); p != nil {
			t.Errorf("newPacer(%v) = %+v, want nil (unpaced)", rate, p)
		}
	}

	p := newPacer(2000)
	if p == nil || p.interval != 500*time.Microsecond {
		t.Fatalf("newPacer(2000) = %+v, want a 500µs interval", p)
	}
	// A deadline that has already passed (and the first record) must not block.
	p.start = time.Now().Add(-time.Second)
	p.wait(0)
	p.wait(1)
}

func TestBigRecord(t *testing.T) {
	const size = 8192
	result := runPipe(t, []string{"--big", "8192"}, "{\"id\":\"p1\",\"type\":\"prompt\"}\n")
	decoded := records(t, result.stdout)
	expectTypes(t, decoded, "response", "message_update", "agent_end", "agent_settled")

	delta := decoded[1].object(t, "assistantMessageEvent").str(t, "delta")
	if len(delta) != size {
		t.Errorf("delta is %d bytes, want %d", len(delta), size)
	}
	if _, ok := decoded[1]["spike"]; ok {
		t.Error("the bulk record carries a latency spike field")
	}
}

func TestCRLFFraming(t *testing.T) {
	result := runPipe(t, []string{"--crlf", "--emit", "1"}, "{\"id\":\"p1\",\"type\":\"prompt\"}\n")
	if !strings.HasSuffix(result.stdout, "\r\n") {
		t.Fatalf("output does not end with CRLF: %q", result.stdout)
	}
	lines := strings.Split(result.stdout, "\n")
	if lines[len(lines)-1] != "" {
		t.Fatalf("output does not end with a terminator: %q", result.stdout)
	}
	for i, line := range lines[:len(lines)-1] {
		if !strings.HasSuffix(line, "\r") {
			t.Fatalf("record %d is not CRLF terminated: %q", i, line)
		}
	}
	expectTypes(t, records(t, result.stdout), "response", "message_update", "agent_end", "agent_settled")
}

func TestSeparatorsStayLiteral(t *testing.T) {
	result := runPipe(t, []string{"--separators", "--emit", "2"}, "{\"id\":\"p1\",\"type\":\"prompt\"}\n")
	out := result.stdout

	if !strings.Contains(out, "\u2028\u2029") {
		t.Fatal("no literal U+2028/U+2029 code point in the output")
	}
	if strings.Contains(out, `\u2028`) || strings.Contains(out, `\u2029`) {
		t.Fatal("separators were escaped, so a client could not tell them from text")
	}
	// The code points must not have added record boundaries: one LF per record.
	decoded := records(t, out)
	expectTypes(t, decoded, "response", "message_update", "message_update", "agent_end", "agent_settled")

	if got := decoded[1].object(t, "assistantMessageEvent").str(t, "delta"); got != "chunk-0\u2028\u2029" {
		t.Errorf("streamed delta = %q, want the code points appended", got)
	}
	if got := decoded[3].str(t, separatorField); got != "\u2028\u2029" {
		t.Errorf("%s = %q, want the code points on a record without a delta", separatorField, got)
	}
}

func TestStderrLinesDoNotReachStdout(t *testing.T) {
	result := runPipe(t, []string{"--stderr", "3"}, "{\"id\":\"s1\",\"type\":\"get_state\"}\n")
	if got := strings.Count(strings.TrimSuffix(result.stderr, "\n"), "\n") + 1; got != 3 {
		t.Errorf("stderr has %d lines, want 3: %q", got, result.stderr)
	}
	if strings.Contains(result.stdout, "stderr line") {
		t.Errorf("a diagnostic leaked into the protocol stream: %q", result.stdout)
	}
	expectTypes(t, records(t, result.stdout), "response")
}

func TestStallFlagDelaysEveryAnswer(t *testing.T) {
	result := runPipe(t, []string{"--stall-ms", "150"}, "{\"id\":\"s1\",\"type\":\"get_state\"}\n")
	if result.elapsed < 140*time.Millisecond {
		t.Errorf("--stall-ms 150 answered after %v", result.elapsed)
	}
	expectTypes(t, records(t, result.stdout), "response")
}

func TestScriptFaultsStallMs(t *testing.T) {
	script := Script{Faults: Faults{StallMs: 150}}
	result := runPipe(t, []string{"--script", writeScript(t, script)}, "{\"id\":\"s1\",\"type\":\"get_state\"}\n")
	if result.elapsed < 140*time.Millisecond {
		t.Errorf("faults.stallMs 150 answered after %v", result.elapsed)
	}
	expectTypes(t, records(t, result.stdout), "response")
}

func TestExitAfter(t *testing.T) {
	result := runOpenStdin(t, []string{"--exit-after", "150"})
	if result.code != exitOK {
		t.Fatalf("exit code %d, want %d (stderr: %s)", result.code, exitOK, result.stderr)
	}
	if result.elapsed < 140*time.Millisecond {
		t.Errorf("--exit-after 150 exited after %v", result.elapsed)
	}
	if result.elapsed > 5*time.Second {
		t.Errorf("--exit-after 150 took %v", result.elapsed)
	}
}

func TestCrashAfter(t *testing.T) {
	script := Script{Startup: []Step{{Record: json.RawMessage(`{"type":"agent_start"}`)}}}
	result := runOpenStdin(t, []string{"--script", writeScript(t, script), "--crash-after", "150"})
	if result.code != exitCrash {
		t.Fatalf("exit code %d, want %d", result.code, exitCrash)
	}
	// Only the startup record: a crash emits nothing further.
	expectTypes(t, records(t, result.stdout), "agent_start")
}

func TestIgnoreStdinRunsUntilExitAfter(t *testing.T) {
	t.Run("flag", func(t *testing.T) {
		script := Script{Startup: []Step{{Record: json.RawMessage(`{"type":"agent_start"}`)}}}
		result := runOpenStdin(t, []string{"--script", writeScript(t, script), "--ignore-stdin", "--exit-after", "150"})
		if result.code != exitOK {
			t.Fatalf("exit code %d, want %d", result.code, exitOK)
		}
		if result.elapsed < 140*time.Millisecond {
			t.Errorf("--ignore-stdin exited on stdin EOF after %v", result.elapsed)
		}
		expectTypes(t, records(t, result.stdout), "agent_start")
	})

	t.Run("script faults", func(t *testing.T) {
		ms := 150
		script := Script{
			Startup: []Step{{Record: json.RawMessage(`{"type":"agent_start"}`)}},
			Faults:  Faults{ExitAfterMs: &ms},
		}
		result := runOpenStdin(t, []string{"--script", writeScript(t, script), "--ignore-stdin"})
		if result.code != exitOK {
			t.Fatalf("exit code %d, want %d", result.code, exitOK)
		}
		if result.elapsed < 140*time.Millisecond {
			t.Errorf("faults.exitAfterMs 150 exited after %v", result.elapsed)
		}
		expectTypes(t, records(t, result.stdout), "agent_start")
	})
}

func TestExitOnStdinEOF(t *testing.T) {
	t.Run("without startup records", func(t *testing.T) {
		result := runPipe(t, nil, "")
		if result.code != exitOK {
			t.Fatalf("exit code %d, want %d", result.code, exitOK)
		}
		if result.stdout != "" {
			t.Errorf("stdout = %q, want empty", result.stdout)
		}
	})

	t.Run("startup records are emitted first", func(t *testing.T) {
		script := Script{Startup: []Step{{Record: json.RawMessage(`{"type":"agent_start"}`)}}}
		result := runPipe(t, []string{"--script", writeScript(t, script)}, "")
		if result.code != exitOK {
			t.Fatalf("exit code %d, want %d", result.code, exitOK)
		}
		expectTypes(t, records(t, result.stdout), "agent_start")
	})
}
func TestToleratesPiArgv(t *testing.T) {
	script := Script{SessionID: "argv-session"}
	args := []string{
		"--mode", "rpc", "--no-session", "--offline",
		"--name", "build-agent", "-e", "/tmp/extension.ts",
		"--session-dir=/tmp/sessions", "rpc",
		"--script", writeScript(t, script),
		"--emit", "1",
	}
	input := "{\"id\":\"s1\",\"type\":\"get_state\"}\n{\"id\":\"p1\",\"type\":\"prompt\",\"message\":\"hi\"}\n"
	result := runPipe(t, args, input)
	decoded := records(t, result.stdout)
	expectTypes(t, decoded, "response", "response", "message_update", "agent_end", "agent_settled")

	if got := decoded[0].object(t, "data").str(t, "sessionId"); got != "argv-session" {
		t.Errorf("sessionId = %q, want argv-session", got)
	}
}

func TestExtensionUIResponseIsIgnored(t *testing.T) {
	input := "{\"type\":\"extension_ui_response\",\"id\":\"u1\",\"confirmed\":true}\n" +
		"{\"id\":\"s1\",\"type\":\"get_state\"}\n"
	result := runPipe(t, nil, input)
	expectTypes(t, records(t, result.stdout), "response")
}

func TestMalformedRecordGetsParseResponse(t *testing.T) {
	input := "not json\n{\"id\":\"s1\",\"type\":\"get_state\"}\n{\"id\":\"n2\"}\n"
	result := runPipe(t, nil, input)
	decoded := records(t, result.stdout)
	expectTypes(t, decoded, "response", "response", "response")

	if got := decoded[0].str(t, "command"); got != recordParse {
		t.Errorf("first answer is for %q, want %s", got, recordParse)
	}
	if decoded[0].boolean(t, "success") {
		t.Error("malformed record was answered successfully")
	}
	if got := decoded[0].str(t, "error"); !strings.Contains(got, "parse") {
		t.Errorf("parse error = %q", got)
	}
	if got := decoded[1].str(t, "command"); got != commandGetState {
		t.Errorf("second answer is for %q, want %s", got, commandGetState)
	}
	if got := decoded[2].str(t, "command"); got != recordParse {
		t.Errorf("typeless record answered for %q, want %s", got, recordParse)
	}
}

func TestInvalidInvocation(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"non-numeric", []string{"--emit", "many"}, "--emit"},
		{"negative", []string{"--rate", "-1"}, "--rate"},
		{"missing value", []string{"--script"}, "--script"},
		{"missing script", []string{"--script", "/tmp/does-not-exist-fake-pi.json"}, "read script"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := runPipe(t, tc.args, "")
			if result.code != exitUsage {
				t.Fatalf("exit code %d, want %d", result.code, exitUsage)
			}
			if !strings.Contains(result.stderr, tc.want) {
				t.Errorf("stderr %q does not mention %q", result.stderr, tc.want)
			}
			if result.stdout != "" {
				t.Errorf("stdout = %q, want empty", result.stdout)
			}
		})
	}
}

func TestInvalidScriptIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"commands":{"prompt":{"events":[{"record":"not an object"}]}}}`), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	result := runPipe(t, []string{"--script", path}, "")
	if result.code != exitUsage {
		t.Fatalf("exit code %d, want %d", result.code, exitUsage)
	}
	if !strings.Contains(result.stderr, "record is not a JSON object") {
		t.Errorf("stderr = %q, want the offending record", result.stderr)
	}
}

var fixtureDate = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)

// TestFixturesAreWellFormed guards what the other workstreams read: every fixture starts
// with exactly one `#` header naming the command, the pi version, the capture date and
// whether it is a real capture, followed by LF-terminated JSON records only.
func TestFixturesAreWellFormed(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "fixtures", "*.jsonl"))
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no fixtures found in ../fixtures")
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			if len(lines) < 2 {
				t.Fatalf("%s has no records below the header", path)
			}

			header := lines[0]
			if !strings.HasPrefix(header, "# ") {
				t.Fatalf("header = %q, want a single `# ` comment line", header)
			}
			if !strings.Contains(header, "| pi ") {
				t.Errorf("header does not name the pi version: %q", header)
			}
			if !fixtureDate.MatchString(header) {
				t.Errorf("header does not name the capture date: %q", header)
			}
			kind := false
			for _, want := range []string{"real capture", "hand-authored", "derived"} {
				if strings.Contains(header, want) {
					kind = true
				}
			}
			if !kind {
				t.Errorf("header does not say whether the fixture is real or derived: %q", header)
			}

			validate := fixtureSchemas(t)
			for i, line := range lines[1:] {
				var record map[string]json.RawMessage
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatalf("line %d is not a JSON record: %v\nline: %q", i+2, err, line)
				}
				if _, ok := record["type"]; !ok {
					t.Errorf("line %d has no record type: %q", i+2, line)
				}
				validate(t, []byte(line))
			}
		})
	}
}

// TestCaptureScriptIsExecutable keeps the regeneration script runnable and bash-clean:
// fixtures are only trustworthy when the exact command that produced them is available.
func TestCaptureScriptIsExecutable(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "capture-fixtures.sh")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("%s is not executable", path)
	}
	syntax := exec.Command("bash", "-n", path)
	if output, err := syntax.CombinedOutput(); err != nil {
		t.Errorf("bash -n %s: %v\n%s", path, err, output)
	}
}

// fixtureSchemas compiles schemas/pi.json once and returns a validator for one fixture
// record. Every record is checked against the contract the generated types come from, so a
// fixture cannot drift from the wire shapes while still being valid JSON.
func fixtureSchemas(t *testing.T) func(t *testing.T, line []byte) {
	t.Helper()

	data, ok := gen.SchemaJSON("pi")
	if !ok {
		t.Fatal("schemas/pi.json is not embedded; run server/scripts/gen.sh")
	}
	var meta struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(data, &meta); err != nil || meta.ID == "" {
		t.Fatalf("schemas/pi.json has no usable $id (%v)", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode schemas/pi.json: %v", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(meta.ID, doc); err != nil {
		t.Fatalf("register schemas/pi.json: %v", err)
	}

	compiled := map[string]*jsonschema.Schema{}
	compile := func(def string) *jsonschema.Schema {
		if schema, ok := compiled[def]; ok {
			return schema
		}
		schema, err := compiler.Compile(meta.ID + def)
		if err != nil {
			t.Fatalf("compile %s%s: %v", meta.ID, def, err)
		}
		compiled[def] = schema
		return schema
	}

	return func(t *testing.T, line []byte) {
		t.Helper()
		var envelope struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(line, &envelope)
		def := "#/$defs/RpcEventEnvelope"
		switch envelope.Type {
		case "response":
			def = "#/$defs/RpcResponseEnvelope"
		case "extension_ui_request":
			def = "#/$defs/ExtensionUiRequest"
		}
		instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(line))
		if err != nil {
			t.Fatalf("decode record: %v", err)
		}
		if err := compile(def).Validate(instance); err != nil {
			t.Errorf("record does not match %s: %v\nline: %s", def, err, line)
		}
	}
}

// TestFixturesDoNotEmbedTheCaptureHost pins the sanitization the capture script promises:
// the home directory of whoever ran the capture must not reach a fixture. The exact path
// belongs to one machine; the record shape is what a fixture is for.
func TestFixturesDoNotEmbedTheCaptureHost(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skipf("no home directory to check against: %v", err)
	}
	paths, err := filepath.Glob(filepath.Join("..", "fixtures", "*.jsonl"))
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if bytes.Contains(data, []byte(home)) {
			t.Errorf("%s embeds the capture host's home directory %q", path, home)
		}
	}
}
