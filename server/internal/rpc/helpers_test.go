package rpc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

// Test timeouts. They only bound a hung test; every wait below finishes as soon as the
// child produces the record or the state the assertion is about.
const (
	recordTimeout = 10 * time.Second
	exitTimeout   = 10 * time.Second
	closeTimeout  = 10 * time.Second
)

// fakePi builds the harness once per test process and returns the argv of one run with a
// production-shaped pi command line, which is what the bridge sees in production too.
func fakePi(t *testing.T, script fakeharness.Script, extra ...string) []string {
	t.Helper()
	fakeharness.Build(t)
	argv := append([]string{"--mode", "rpc", "--no-session"}, extra...)
	return fakeharness.Command(fakeharness.WriteScript(t, script), argv...)
}

// startBridge starts one bridge and closes it when the test ends, so a failing assertion
// never leaves a child behind.
func startBridge(t *testing.T, opts Options, argv []string, stderr func(line []byte)) Bridge {
	t.Helper()
	bridge := New(Spec{Command: argv, Dir: t.TempDir(), Stderr: stderr}, opts)
	ctx, cancel := context.WithTimeout(context.Background(), recordTimeout)
	defer cancel()
	if err := bridge.Start(ctx); err != nil {
		t.Fatalf("Start(%v): %v", argv, err)
	}
	t.Cleanup(func() { _ = bridge.Close() })
	return bridge
}

// nextRecord returns the next record, failing the test when Records closes first.
func nextRecord(t *testing.T, records <-chan Record) Record {
	t.Helper()
	select {
	case record, ok := <-records:
		if !ok {
			t.Fatal("Records closed before the expected record")
		}
		return record
	case <-time.After(recordTimeout):
		t.Fatal("timed out waiting for a record")
		return Record{}
	}
}

// recordsOfType reads records until one has the wanted type, skipping events that arrive
// before it.
func recordsOfType(t *testing.T, records <-chan Record, want string) Record {
	t.Helper()
	deadline := time.After(recordTimeout)
	for {
		select {
		case record, ok := <-records:
			if !ok {
				t.Fatalf("Records closed before a %q record arrived", want)
			}
			if record.Type == want {
				return record
			}
		case <-deadline:
			t.Fatalf("timed out waiting for a %q record", want)
		}
	}
}

// drainRecords collects records until Records closes and proves that it closes.
func drainRecords(t *testing.T, records <-chan Record) []Record {
	t.Helper()
	collected := []Record{}
	deadline := time.After(recordTimeout)
	for {
		select {
		case record, ok := <-records:
			if !ok {
				if _, stillOpen := <-records; stillOpen {
					t.Error("Records yielded a record after closing")
				}
				return collected
			}
			collected = append(collected, record)
		case <-deadline:
			t.Fatalf("Records did not close; %d records so far", len(collected))
		}
	}
}

// fields decodes a record into its top-level fields.
func fields(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("record %s is not a JSON object: %v", raw, err)
	}
	return decoded
}

// fieldString reads one string field of a record.
func fieldString(t *testing.T, raw json.RawMessage, name string) string {
	t.Helper()
	value, ok := fields(t, raw)[name]
	if !ok {
		t.Fatalf("record %s has no %q field", raw, name)
	}
	var decoded string
	if err := json.Unmarshal(value, &decoded); err != nil {
		t.Fatalf("record %s field %q is not a string: %v", raw, name, err)
	}
	return decoded
}

// processExists reports whether the pid is still visible to the kernel, which is how the
// lifecycle tests prove a child was reaped instead of left behind as a zombie.
func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid)))
	return err == nil
}
