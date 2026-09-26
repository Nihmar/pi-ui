package sessions

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"
	"unicode/utf8"

	"github.com/Nihmar/pi-ui/server/internal/rpc"
)

// TestRecordPayloadKeepsTheBytesOfAnUnparseableLine pins the last piece of the
// verbatim-bytes claim: a child line that is not valid JSON travels as pi.unknown, and a
// line that is not valid UTF-8 must still be recoverable exactly. A JSON string alone
// cannot carry it — encoding/json replaces every invalid byte with U+FFFD — so the payload
// also carries rawBase64.
func TestRecordPayloadKeepsTheBytesOfAnUnparseableLine(t *testing.T) {
	valid := recordPayload(rpc.Record{Raw: json.RawMessage(`{"type":"message_update"}`)})
	if string(valid) != `{"type":"message_update"}` {
		t.Fatalf("valid record payload = %s, want it byte for byte", valid)
	}

	// A malformed line: the bytes of an encoding the child got wrong. \xff\xfe is not
	// valid UTF-8.
	raw := []byte{'{', '"', 0xff, 0xfe, '"', '}'}
	payload := recordPayload(rpc.Record{Raw: raw})

	var decoded struct {
		Raw       string `json:"raw"`
		RawBase64 string `json:"rawBase64"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("payload %s is not a JSON object: %v", payload, err)
	}
	if decoded.RawBase64 == "" {
		t.Fatalf("payload %s has no rawBase64 for a line that is not valid UTF-8", payload)
	}
	back, err := base64.StdEncoding.DecodeString(decoded.RawBase64)
	if err != nil {
		t.Fatalf("rawBase64 is not base64: %v", err)
	}
	if !bytes.Equal(back, raw) {
		t.Fatalf("rawBase64 decoded to %q, want the child's bytes %q", back, raw)
	}
	if !utf8.ValidString(decoded.Raw) {
		t.Fatalf("raw = %q is not valid UTF-8, want the readable approximation", decoded.Raw)
	}
}
