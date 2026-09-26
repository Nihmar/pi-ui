package rpc

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

// U+2028 and U+2029 as literal UTF-8 bytes: Node's readline treats them as record
// boundaries, which would split pi's records mid-string.
const (
	lineSeparator      = "\u2028"
	paragraphSeparator = "\u2029"
)

func TestFramingKeepsUnicodeSeparators(t *testing.T) {
	insideString := []byte(`{"type":"message_update","delta":"a` + lineSeparator + `b` + paragraphSeparator + `c"}`)
	outsideString := []byte(`{"a":1}` + lineSeparator + paragraphSeparator + `{"b":2}`)
	terminated := append(append([]byte(nil), outsideString...), lineFeed)

	for _, tc := range []struct {
		name  string
		input []byte
	}{
		{"separators inside a JSON string", append(append([]byte(nil), insideString...), lineFeed)},
		{"separators as bare bytes", terminated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records := readRecords(t, newRecordReader(bytes.NewReader(tc.input)))
			want := bytes.TrimSuffix(tc.input, []byte{lineFeed})
			if len(records) != 1 {
				t.Fatalf("got %d records, want 1: %q", len(records), records)
			}
			if !bytes.Equal(records[0], want) {
				t.Fatalf("record = %q, want %q", records[0], want)
			}
		})
	}
}

// TestFramingStripsCRLFAndSkipsEmptyLines covers a child that terminates records with
// CRLF and a child that emits blank lines between records.
func TestFramingStripsCRLFAndSkipsEmptyLines(t *testing.T) {
	input := "\r\n\n{\"a\":1}\r\n" + "\n" + "{\"b\":2}\r\n\r\n"
	records := readRecords(t, newRecordReader(strings.NewReader(input)))
	assertRecords(t, records, `{"a":1}`, `{"b":2}`)
}

// TestFramingHandlesLargeRecords feeds an 8 MiB record in chunks: the buffer has to grow
// past the read granularity, and the record must arrive byte-for-byte.
func TestFramingHandlesLargeRecords(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 8<<20)
	input := append([]byte(`{"type":"message_update","delta":"`), payload...)
	input = append(input, []byte("\"}\n")...)

	records := readRecords(t, newRecordReader(&chunkReader{src: input, chunk: 4 << 10}))
	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	if !bytes.Equal(records[0], input[:len(input)-1]) {
		t.Fatalf("8 MiB record is %d bytes, want %d", len(records[0]), len(input)-1)
	}
}

// TestFramingSurvivesManySmallWrites reads one byte at a time, the worst case a pipe can
// produce when a child writes a record in several syscalls.
func TestFramingSurvivesManySmallWrites(t *testing.T) {
	first := `{"type":"extension_ui_request","id":"u1","method":"confirm","message":"ok?"}`
	second := `{"type":"message_update","delta":"hello"}`
	src := iotest.OneByteReader(strings.NewReader(first + "\n" + second + "\n"))
	records := readRecords(t, newRecordReader(src))
	assertRecords(t, records, first, second)
}

// TestFramingDeliversFinalRecordWithoutTerminator covers a child that exits after writing
// its last record without a trailing line feed.
func TestFramingDeliversFinalRecordWithoutTerminator(t *testing.T) {
	records := readRecords(t, newRecordReader(strings.NewReader("{\"a\":1}\n{\"b\":2}")))
	assertRecords(t, records, `{"a":1}`, `{"b":2}`)
}

// TestFramingPreservesInvalidJSONBytes is the lenient pass-through rule: a record that is
// not valid JSON still reaches the caller unchanged, so the supervisor can report it.
func TestFramingPreservesInvalidJSONBytes(t *testing.T) {
	invalid := `{not json at all, "unterminated`
	records := readRecords(t, newRecordReader(strings.NewReader(invalid+"\n")))
	assertRecords(t, records, invalid)
}

// TestFramingReportsStreamError proves a failing read is reported after the records it
// already delivered, instead of being swallowed as a clean end of stream.
func TestFramingReportsStreamError(t *testing.T) {
	boom := errors.New("boom")
	reader := newRecordReader(&failingReader{src: []byte("{\"a\":1}"), err: boom})

	record, err := reader.Next()
	if err != nil {
		t.Fatalf("first Next: %v", err)
	}
	if string(record) != `{"a":1}` {
		t.Fatalf("record = %q, want %q", record, `{"a":1}`)
	}
	if _, err := reader.Next(); !errors.Is(err, boom) {
		t.Fatalf("second Next = %v, want %v", err, boom)
	}
}

// chunkReader hands out at most chunk bytes per Read, which is how a pipe behaves when a
// record is larger than the pipe buffer.
type chunkReader struct {
	src   []byte
	chunk int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.src) == 0 {
		return 0, io.EOF
	}
	n := min(r.chunk, len(p), len(r.src))
	copy(p, r.src[:n])
	r.src = r.src[n:]
	return n, nil
}

// failingReader returns its bytes and then a non-EOF error, like a pipe that breaks
// instead of closing cleanly.
type failingReader struct {
	src    []byte
	err    error
	failed bool
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.failed {
		return 0, r.err
	}
	r.failed = true
	return copy(p, r.src), nil
}

func readRecords(t *testing.T, reader *recordReader) [][]byte {
	t.Helper()
	var records [][]byte
	for {
		record, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return records
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		records = append(records, record)
	}
}

func assertRecords(t *testing.T, got [][]byte, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d records, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if string(got[i]) != want[i] {
			t.Errorf("record %d = %q, want %q", i, got[i], want[i])
		}
	}
}
