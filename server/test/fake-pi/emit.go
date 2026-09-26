package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// separatorPoints is embedded literally (never as \u escapes) inside a JSON string by
// --separators: a client that splits records on anything but 0x0A must break on it.
const separatorPoints = "\u2028\u2029"

// separatorField is the extra top-level field used on records that carry no delta.
const separatorField = "piuiSeparators"

// emitter writes protocol records to the child's stdout and owns the record terminator.
type emitter struct {
	mu         sync.Mutex
	out        io.Writer
	terminator string
	separators bool
}

func newEmitter(w io.Writer, cfg config) *emitter {
	terminator := "\n"
	if cfg.crlf {
		terminator = "\r\n"
	}
	return &emitter{out: w, terminator: terminator, separators: cfg.separators}
}

// writeRecord writes one record plus its terminator in a single Write, so a concurrent
// fault can never truncate a record.
func (e *emitter) writeRecord(record json.RawMessage) error {
	if e.separators {
		record = withSeparators(record)
	}
	line := make([]byte, 0, len(record)+len(e.terminator))
	line = append(line, record...)
	line = append(line, e.terminator...)

	e.mu.Lock()
	defer e.mu.Unlock()
	_, err := e.out.Write(line)
	return err
}

// exit ends the process once the record in flight is complete; fault timers use it.
func (e *emitter) exit(code int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	os.Exit(code)
}

// writeParseError mirrors pi's answer to a malformed record: a response without an id.
func (e *emitter) writeParseError(err error) error {
	record, marshalErr := json.Marshal(responseRecord{
		Type:    recordResponse,
		Command: recordParse,
		Success: false,
		Error:   "Failed to parse command: " + err.Error(),
	})
	if marshalErr != nil {
		return marshalErr
	}
	return e.writeRecord(record)
}

// emitStartup replays script.startup (with its delays) before stdin is read.
func (e *emitter) emitStartup(steps []Step) error {
	for _, step := range steps {
		sleepMs(step.DelayMs)
		if err := e.writeRecord(step.Record); err != nil {
			return err
		}
	}
	return nil
}

// emitSyntheticRun streams the traffic that follows a prompt: an optional --big record,
// --emit paced message_update deltas, then agent_end and agent_settled.
func (e *emitter) emitSyntheticRun(cfg config) error {
	if cfg.big == 0 && cfg.emit == 0 {
		return nil
	}
	if cfg.big > 0 {
		record, err := deltaRecord(strings.Repeat("x", cfg.big), nil)
		if err != nil {
			return err
		}
		if err := e.writeRecord(record); err != nil {
			return err
		}
	}
	if cfg.emit > 0 {
		ticker := newRateTicker(cfg.rate)
		if ticker != nil {
			defer ticker.Stop()
		}
		for i := 0; i < cfg.emit; i++ {
			if i > 0 && ticker != nil {
				<-ticker.C
			}
			record, err := deltaRecord("chunk-"+strconv.Itoa(i), &spikeMarker{I: i, NS: time.Now().UnixNano()})
			if err != nil {
				return err
			}
			if err := e.writeRecord(record); err != nil {
				return err
			}
		}
	}
	if err := e.writeRecord(json.RawMessage(`{"type":"agent_end","messages":[],"willRetry":false}`)); err != nil {
		return err
	}
	return e.writeRecord(json.RawMessage(`{"type":"agent_settled"}`))
}

// newRateTicker bounds the synthetic stream to --rate records per second; a nil ticker
// means "as fast as the writer allows". A ticker (not a per-record sleep) keeps the
// sustained rate on target instead of accumulating sleep overhead.
func newRateTicker(rate float64) *time.Ticker {
	if rate <= 0 {
		return nil
	}
	interval := time.Duration(float64(time.Second) / rate)
	if interval <= 0 {
		return nil
	}
	return time.NewTicker(interval)
}

// assistantMessageEvent is the delta-only event pi puts in a message_update.
type assistantMessageEvent struct {
	Type  string `json:"type"`
	Delta string `json:"delta"`
}

// spikeMarker is the latency probe the spike tests read out of synthetic records.
type spikeMarker struct {
	I  int   `json:"i"`
	NS int64 `json:"ns"`
}

type messageUpdateRecord struct {
	Type                  string                `json:"type"`
	AssistantMessageEvent assistantMessageEvent `json:"assistantMessageEvent"`
	Spike                 *spikeMarker          `json:"spike,omitempty"`
}

func deltaRecord(delta string, spike *spikeMarker) (json.RawMessage, error) {
	return json.Marshal(messageUpdateRecord{
		Type:                  recordMessageUpdate,
		AssistantMessageEvent: assistantMessageEvent{Type: "text_delta", Delta: delta},
		Spike:                 spike,
	})
}

func sleepMs(ms int) {
	if ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}

// withSeparators returns a copy of record carrying the literal U+2028/U+2029 code
// points inside a JSON string value. The delta of a streaming record is preferred
// (that is where pi streams arbitrary text); a record without a delta gets an extra
// top-level string field instead of a rewritten payload, so no pi field is corrupted.
// The code points are inserted after marshalling, because encoding/json would escape
// them as \u2028/\u2029 and a client could then not tell the two apart.
func withSeparators(record json.RawMessage) json.RawMessage {
	if _, end, ok := findStringValue(record, "delta"); ok {
		patched := make([]byte, 0, len(record)+len(separatorPoints))
		patched = append(patched, record[:end-1]...)
		patched = append(patched, separatorPoints...)
		patched = append(patched, record[end-1:]...)
		return patched
	}
	body := bytes.TrimSpace(record)
	if len(body) < 2 || body[0] != '{' || body[len(body)-1] != '}' {
		return record
	}
	field := `"` + separatorField + `":"` + separatorPoints + `"`
	if len(bytes.TrimSpace(body[1:len(body)-1])) == 0 {
		return []byte("{" + field + "}")
	}
	patched := make([]byte, 0, len(body)+len(field)+2)
	patched = append(patched, body[:len(body)-1]...)
	patched = append(patched, ',')
	patched = append(patched, field...)
	patched = append(patched, '}')
	return patched
}

// findStringValue returns the [start,end) byte range of the string value that follows
// the first `"key":` occurrence, quotes included. Keys and values inside nested
// objects are searched too; a key mentioned inside another string is skipped, because
// strings are scanned as units.
func findStringValue(record []byte, key string) (int, int, bool) {
	for i := 0; i+1 < len(record); {
		if record[i] != '"' {
			i++
			continue
		}
		itemEnd, ok := scanString(record, i)
		if !ok {
			return 0, 0, false
		}
		item := record[i:itemEnd]
		next := skipSpace(record, itemEnd)
		if next < len(record) && record[next] == ':' && len(item) >= 2 && string(item[1:len(item)-1]) == key {
			valueStart := skipSpace(record, next+1)
			if valueStart >= len(record) || record[valueStart] != '"' {
				return 0, 0, false
			}
			valueEnd, ok := scanString(record, valueStart)
			if !ok {
				return 0, 0, false
			}
			return valueStart, valueEnd, true
		}
		i = itemEnd
	}
	return 0, 0, false
}

// scanString returns the index just past the closing quote of the JSON string that
// starts at start, honouring backslash escapes.
func scanString(record []byte, start int) (int, bool) {
	for i := start + 1; i < len(record); i++ {
		switch record[i] {
		case '\\':
			i++
		case '"':
			return i + 1, true
		}
	}
	return 0, false
}

func skipSpace(record []byte, from int) int {
	for i := from; i < len(record); i++ {
		switch record[i] {
		case ' ', '\t', '\n', '\r':
		default:
			return i
		}
	}
	return len(record)
}
