package adversarial_test

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/rpc"
)

// childRun is everything one scripted child produced: the records the bridge
// published, the diagnostic lines its stderr hook saw, and the wait result.
type childRun struct {
	records []rpc.Record
	stderr  []string
	waitErr error
}

// runChild spawns `sh <script>` through the real rpc.Bridge, drains every record
// and waits for the child to be reaped. It is the framing test's view of a child
// process: raw bytes on stdout, diagnostics on stderr, nothing else.
func runChild(t *testing.T, script string) childRun {
	t.Helper()

	shell := shellPath(t)
	path := writeFile(t, "child.sh", script)

	var mu sync.Mutex
	var stderrLines []string
	bridge := rpc.New(
		rpc.Spec{
			Command: []string{shell, path},
			Stderr: func(line []byte) {
				mu.Lock()
				stderrLines = append(stderrLines, string(line))
				mu.Unlock()
			},
		},
		rpc.Options{KillGrace: time.Second},
	)

	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	if err := bridge.Start(ctx); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() { _ = bridge.Close() })

	var records []rpc.Record
	for record := range bridge.Records() {
		records = append(records, record)
	}
	waitErr := bridge.Wait()

	mu.Lock()
	lines := append([]string(nil), stderrLines...)
	mu.Unlock()
	return childRun{records: records, stderr: lines, waitErr: waitErr}
}

// TestFraming_SeparatorsCRLFSplitAndInvalidPassThrough is the C7 byte-level
// contract against a real pipe: U+2028/U+2029 inside a JSON string survive
// untouched, a CRLF terminator loses only the carriage return, a record split
// across two writes is reassembled, invalid JSON is passed through instead of
// rejected, and a last record without a terminator is still delivered.
func TestFraming_SeparatorsCRLFSplitAndInvalidPassThrough(t *testing.T) {
	sep, par := "\u2028", "\u2029"
	run := runChild(t, `#!/bin/sh
printf '{"type":"sep","v":"a`+sep+`b`+par+`c"}\n'
printf '{"type":"crlf","n":1}\r\n'
printf '{"type":"split'
sleep 0.05
printf '","half":"end"}\n'
printf 'this is not json\n'
printf '{"type":"tail"}'
`)

	if run.waitErr != nil {
		t.Fatalf("child wait: %v", run.waitErr)
	}
	want := []struct {
		raw  string
		typ  string
		name string
	}{
		{raw: `{"type":"sep","v":"a` + sep + `b` + par + `c"}`, typ: "sep", name: "U+2028/U+2029 record"},
		{raw: `{"type":"crlf","n":1}`, typ: "crlf", name: "CRLF record without its CR"},
		{raw: `{"type":"split","half":"end"}`, typ: "split", name: "record split across two writes"},
		{raw: "this is not json", typ: "", name: "invalid JSON passed through"},
		{raw: `{"type":"tail"}`, typ: "tail", name: "final record without a terminator"},
	}
	if len(run.records) != len(want) {
		t.Fatalf("records = %d, want %d (%+v)", len(run.records), len(want), run.records)
	}
	for i, expected := range want {
		got := run.records[i]
		if string(got.Raw) != expected.raw {
			t.Errorf("%s: raw = %q, want %q", expected.name, got.Raw, expected.raw)
		}
		if got.Type != expected.typ {
			t.Errorf("%s: type = %q, want %q", expected.name, got.Type, expected.typ)
		}
	}
	if strings.Contains(string(run.records[0].Raw), `\u2028`) || strings.Contains(string(run.records[0].Raw), `\u2029`) {
		t.Errorf("separators were escaped on the wire: %q", run.records[0].Raw)
	}
}

// TestFraming_LargeRecordSurvivesSplitReads pushes an 8 MiB record through a real
// pipe, so the record spans hundreds of 64 KiB reads and the reader's buffer has
// to grow without truncating or duplicating a byte.
func TestFraming_LargeRecordSurvivesSplitReads(t *testing.T) {
	run := runChild(t, `#!/bin/sh
printf '{"type":"big","pad":"'
head -c `+strconv.Itoa(bigRecordBytes)+` /dev/zero | tr '\0' x
printf '","end":true}\n'
`)

	if run.waitErr != nil {
		t.Fatalf("child wait: %v", run.waitErr)
	}
	if len(run.records) != 1 {
		t.Fatalf("records = %d, want exactly 1", len(run.records))
	}
	record := run.records[0]
	if record.Type != "big" {
		t.Fatalf("type = %q, want big", record.Type)
	}
	const prefix = `{"type":"big","pad":"`
	const suffix = `","end":true}`
	wantLen := len(prefix) + bigRecordBytes + len(suffix)
	if len(record.Raw) != wantLen {
		t.Fatalf("record length = %d, want %d", len(record.Raw), wantLen)
	}
	if !strings.HasPrefix(string(record.Raw), prefix) || !strings.HasSuffix(string(record.Raw), suffix) {
		t.Fatalf("record boundary is corrupted: %q…%q", record.Raw[:64], record.Raw[len(record.Raw)-32:])
	}
	pad := record.Raw[len(prefix) : len(prefix)+bigRecordBytes]
	if pad[0] != 'x' || pad[len(pad)-1] != 'x' || strings.IndexByte(string(pad), 0) >= 0 {
		t.Fatalf("payload is not the exact 8 MiB of x the child wrote")
	}
}

// TestFraming_StderrStaysOutOfTheRecordStream interleaves diagnostic lines with
// protocol records and asserts the two streams never mix: the hook sees every
// line, no record contains one, and the record sequence is exactly what stdout
// wrote.
func TestFraming_StderrStaysOutOfTheRecordStream(t *testing.T) {
	run := runChild(t, `#!/bin/sh
i=1
while [ $i -le 50 ]; do
  echo "diagnostic $i" >&2
  printf '{"type":"record","n":%d}\n' "$i"
  i=$((i+1))
done
echo "final diagnostic" >&2
`)

	if run.waitErr != nil {
		t.Fatalf("child wait: %v", run.waitErr)
	}
	if len(run.records) != 50 {
		t.Fatalf("records = %d, want 50", len(run.records))
	}
	for i, record := range run.records {
		want := `{"type":"record","n":` + strconv.Itoa(i+1) + `}`
		if string(record.Raw) != want {
			t.Fatalf("record %d = %q, want %q", i, record.Raw, want)
		}
		if strings.Contains(string(record.Raw), "diagnostic") {
			t.Fatalf("stderr leaked into record %d: %q", i, record.Raw)
		}
	}
	if len(run.stderr) != 51 {
		t.Fatalf("stderr lines = %d, want 51 (%v)", len(run.stderr), run.stderr)
	}
	if run.stderr[0] != "diagnostic 1" || run.stderr[50] != "final diagnostic" {
		t.Fatalf("stderr lines out of order or mangled: %q … %q", run.stderr[0], run.stderr[50])
	}
}
