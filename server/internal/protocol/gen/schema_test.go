package gen

import (
	"bytes"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// draft2020Version is how jsonschema/v6 numbers the 2020-12 draft; the
// jsonschema.Draft2020 value does not expose its version number.
const draft2020Version = 2020

// schemaMeta is the part of a schema document this test reasons about.
type schemaMeta struct {
	ID    string `json:"$id"`
	Title string `json:"title"`
}

// decodeJSON decodes one JSON document the way the validator expects it,
// keeping number precision.
func decodeJSON(t *testing.T, what string, data []byte) any {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode %s: %v", what, err)
	}
	return doc
}

// metaOf reads the $id and title of one embedded schema and checks that it is
// valid JSON at all.
func metaOf(t *testing.T, name string) schemaMeta {
	t.Helper()
	data, ok := SchemaJSON(name)
	if !ok {
		t.Fatalf("SchemaJSON(%q) not found; schemas: %v", name, Schemas())
	}
	if !json.Valid(data) {
		t.Fatalf("SchemaJSON(%q) is not valid JSON", name)
	}
	var meta schemaMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("read $id/title of %q: %v", name, err)
	}
	return meta
}

// generatedNames returns the schemas produced by scripts/gen.sh. Names starting
// with "test_" are registered by the registry tests (register_test.go) and are
// not part of the wire surface.
func generatedNames() []string {
	names := make([]string, 0, len(Schemas()))
	for _, name := range Schemas() {
		if !strings.HasPrefix(name, "test_") {
			names = append(names, name)
		}
	}
	return names
}

// compiler registers every embedded schema under its own $id so refs resolve,
// and returns the compiler.
func compiler(t *testing.T) *jsonschema.Compiler {
	t.Helper()
	c := jsonschema.NewCompiler()
	for _, name := range generatedNames() {
		meta := metaOf(t, name)
		if meta.ID == "" {
			t.Fatalf("schema %q has no $id, so refs cannot resolve", name)
		}
		data, _ := SchemaJSON(name)
		if err := c.AddResource(meta.ID, decodeJSON(t, name, data)); err != nil {
			t.Fatalf("AddResource(%s): %v", meta.ID, err)
		}
	}
	return c
}

// compile compiles one location: a schema $id, or "$id#/$defs/<def>".
func compile(t *testing.T, c *jsonschema.Compiler, loc string) *jsonschema.Schema {
	t.Helper()
	sch, err := c.Compile(loc)
	if err != nil {
		t.Fatalf("Compile(%s): %v", loc, err)
	}
	return sch
}

// defLocation builds the compile location of one $defs entry of a schema.
func defLocation(t *testing.T, name, def string) string {
	t.Helper()
	return metaOf(t, name).ID + "#/$defs/" + def
}

// TestSchemasCompile is the drift guard CI relies on: every schema embedded in
// internal/protocol/gen compiles as draft 2020-12.
func TestSchemasCompile(t *testing.T) {
	c := compiler(t)
	names := generatedNames()
	if len(names) == 0 {
		t.Fatal("Schemas() is empty: scripts/gen.sh did not run or produced nothing")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			sch := compile(t, c, metaOf(t, name).ID)
			if sch.DraftVersion != draft2020Version {
				t.Errorf("draft = %d, want %d (draft 2020-12)", sch.DraftVersion, draft2020Version)
			}
			if sch.Location == "" {
				t.Error("compiled schema has no location")
			}
		})
	}
}

// TestCoreAndPiSchemaJSON pins the two schemas of the spike and their identity.
func TestCoreAndPiSchemaJSON(t *testing.T) {
	want := map[string]struct{ id, title string }{
		"core": {id: "https://github.com/Nihmar/pi-ui/schemas/core.json", title: "pi-ui core vocabulary"},
		"pi":   {id: "https://github.com/Nihmar/pi-ui/schemas/pi.json", title: "pi child protocol (pi --mode rpc)"},
	}
	for name, w := range want {
		meta := metaOf(t, name)
		if meta.ID != w.id {
			t.Errorf("schema %q $id = %q, want %q", name, meta.ID, w.id)
		}
		if meta.Title != w.title {
			t.Errorf("schema %q title = %q, want %q", name, meta.Title, w.title)
		}
	}
}

// TestSchemasSortedAndNamed guards the registry contract: the names reported by
// Schemas() are the sorted schemas/<name>.json basenames that SchemaJSON takes.
func TestSchemasSortedAndNamed(t *testing.T) {
	names := Schemas()
	if !sort.StringsAreSorted(names) {
		t.Errorf("Schemas() = %v, want lexicographic order", names)
	}
	for _, want := range []string{"core", "pi"} {
		if !slices.Contains(names, want) {
			t.Errorf("Schemas() = %v, want it to contain %q", names, want)
		}
	}
	for _, name := range generatedNames() {
		if meta := metaOf(t, name); !strings.HasSuffix(meta.ID, "/"+name+".json") {
			t.Errorf("schema %q is registered under $id %q, want the name to match the file", name, meta.ID)
		}
	}
}

// TestSchemasAreSelfContained fails when a schema refs another schema file.
// scripts/gen.sh generates one Go file per schema, so a cross-file $ref would be
// invisible to the generator and produce code that does not compile.
func TestSchemasAreSelfContained(t *testing.T) {
	for _, name := range generatedNames() {
		data, _ := SchemaJSON(name)
		for _, ref := range collectRefs(decodeJSON(t, name, data)) {
			if !strings.HasPrefix(ref, "#") {
				t.Errorf("schema %q has the cross-file $ref %q; schemas must be self-contained", name, ref)
			}
		}
	}
}

// collectRefs walks a decoded schema and returns every "$ref" value, wherever
// it sits.
func collectRefs(v any) []string {
	var refs []string
	switch node := v.(type) {
	case map[string]any:
		for key, value := range node {
			if key == "$ref" {
				if ref, ok := value.(string); ok {
					refs = append(refs, ref)
					continue
				}
			}
			refs = append(refs, collectRefs(value)...)
		}
	case []any:
		for _, item := range node {
			refs = append(refs, collectRefs(item)...)
		}
	}
	return refs
}

// validateAgainst validates one JSON document against "$id#/$defs/<def>".
func validateAgainst(t *testing.T, c *jsonschema.Compiler, name, def, doc string) error {
	t.Helper()
	instance, err := jsonschema.UnmarshalJSON(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("decode instance for %s.%s: %v", name, def, err)
	}
	return compile(t, c, defLocation(t, name, def)).Validate(instance)
}

// TestDefinitionConstraints exercises the parts of the wire surface the server
// really validates, in both directions: valid payloads pass, and the constraints
// that matter (session id shape, protocol version, required pi fields, error
// codes) reject bad input.
func TestDefinitionConstraints(t *testing.T) {
	c := compiler(t)
	tests := []struct {
		name    string
		def     string
		doc     string
		wantErr bool
	}{
		{name: "core", def: "ServerSessionId", doc: `"s_0123456789abcdef"`},
		{name: "core", def: "ServerSessionId", doc: `"s_0123456789ABCDEF"`, wantErr: true},
		{name: "core", def: "ServerSessionId", doc: `"pi-session-1"`, wantErr: true},
		{name: "core", def: "ProtocolVersion", doc: `1`},
		{name: "core", def: "ProtocolVersion", doc: `2`, wantErr: true},
		{name: "core", def: "Scope", doc: `"operator"`},
		{name: "core", def: "Scope", doc: `"root"`, wantErr: true},
		{name: "core", def: "Timestamp", doc: `"2025-01-02T03:04:05.678Z"`},
		{name: "core", def: "Timestamp", doc: `"2025-01-02 03:04:05"`, wantErr: true},
		{name: "core", def: "ApiError", doc: `{"code":"session_not_found","message":"no such session","details":{"sessionId":"s_x"},"traceId":"t1"}`},
		{name: "core", def: "ApiError", doc: `{"code":"nope","message":"x"}`, wantErr: true},
		{name: "core", def: "ApiError", doc: `{"code":"internal"}`, wantErr: true},
		{name: "core", def: "ServerInfo", doc: `{"version":"0.0.1-spike","piVersion":"0.87.1","protocol":1,"features":["terminal"],"limits":{"maxSessions":8}}`},
		{name: "core", def: "ServerInfo", doc: `{"version":"0.0.1-spike","piVersion":"0.87.1","protocol":1,"features":[]}`, wantErr: true},

		// pi events pass through verbatim: unknown types and unknown fields are fine.
		{name: "pi", def: "RpcEventEnvelope", doc: `{"type":"something_new","id":"18","payload":{"unknown":true}}`},
		{name: "pi", def: "RpcEventEnvelope", doc: `{"id":"18"}`, wantErr: true},
		{name: "pi", def: "RpcResponseEnvelope", doc: `{"type":"response","id":"c1","command":"get_state","success":true,"data":{"isStreaming":false}}`},
		{name: "pi", def: "RpcResponseEnvelope", doc: `{"type":"response","success":false,"error":{"message":"boom"}}`},
		{name: "pi", def: "RpcResponseEnvelope", doc: `{"type":"event","success":true}`, wantErr: true},
		{name: "pi", def: "ExtensionUiRequest", doc: `{"type":"extension_ui_request","id":"u1","method":"confirm","title":"Bash","message":"run rm -rf?","timeout":60000}`},
		{name: "pi", def: "ExtensionUiRequest", doc: `{"type":"extension_ui_request","id":"u1","method":"setWidget","widgetKey":"k","widgetLines":["a","b"],"widgetPlacement":"below"}`},
		{name: "pi", def: "ExtensionUiRequest", doc: `{"type":"extension_ui_request","id":"u1","method":"drag"}`, wantErr: true},
		{name: "pi", def: "ExtensionUiResponse", doc: `{"type":"extension_ui_response","id":"u1","confirmed":false}`},
		{name: "pi", def: "ExtensionUiResponse", doc: `{"type":"extension_ui_response","id":"u1","value":"Allow"}`},
		{name: "pi", def: "RpcSessionState", doc: `{"sessionId":"pi-1","sessionFile":"/tmp/s.jsonl","sessionName":"spike","thinkingLevel":"medium","isStreaming":false,"isCompacting":false,"steeringMode":"immediate","followUpMode":"queue","autoCompactionEnabled":true,"messageCount":3,"pendingMessageCount":0,"model":{"id":"claude","provider":"anthropic"}}`},
		{name: "pi", def: "RpcSessionState", doc: `{"sessionId":"pi-1","thinkingLevel":"medium","isStreaming":false,"isCompacting":false,"followUpMode":"queue","autoCompactionEnabled":true,"messageCount":3,"pendingMessageCount":0}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+tt.def, func(t *testing.T) {
			err := validateAgainst(t, c, tt.name, tt.def, tt.doc)
			switch {
			case tt.wantErr && err == nil:
				t.Errorf("validated %s, want a validation error", tt.doc)
			case !tt.wantErr && err != nil:
				t.Errorf("validated %s: %v", tt.doc, err)
			}
		})
	}
}

// TestGeneratedTypesRoundTrip checks that the generated Go types match the
// schema: the constants exist with the right values, a ServerInfo survives a
// round trip and the required lists still reject a partial pi record.
func TestGeneratedTypesRoundTrip(t *testing.T) {
	if ProtocolVersion(1) != 1 || ProtocolVersion(0) == 1 {
		t.Error("generated ProtocolVersion does not match the schema const")
	}
	for got, want := range map[string]string{
		string(ScopeViewer):                           "viewer",
		string(ScopeOperator):                         "operator",
		string(ScopeAdmin):                            "admin",
		string(ErrorCodeSessionNotFound):              "session_not_found",
		string(ErrorCodeReplayCursorInvalid):          "replay_cursor_invalid",
		string(ExtensionUiRequestMethodSetWidget):     "setWidget",
		string(ExtensionUiRequestMethodSetEditorText): "set_editor_text",
	} {
		if got != want {
			t.Errorf("generated constant = %q, want %q", got, want)
		}
	}

	info := ServerInfo{
		Version:   "0.0.1-spike",
		PiVersion: "0.87.1",
		Protocol:  1,
		Features:  []string{"terminal", "git"},
		Limits:    ServerInfoLimits{"maxSessions": 8},
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal ServerInfo: %v", err)
	}
	if !strings.Contains(string(encoded), `"protocol":1`) {
		t.Errorf("marshalled ServerInfo = %s, want protocol 1", encoded)
	}
	var decoded ServerInfo
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal ServerInfo: %v", err)
	}
	if decoded.PiVersion != info.PiVersion || len(decoded.Features) != 2 {
		t.Errorf("ServerInfo did not round trip: %+v", decoded)
	}

	var req ExtensionUiRequest
	const notify = `{"type":"extension_ui_request","id":"u1","method":"notify","message":"ready","notifyType":"info"}`
	if err := json.Unmarshal([]byte(notify), &req); err != nil {
		t.Fatalf("unmarshal ExtensionUiRequest: %v", err)
	}
	if req.Method != ExtensionUiRequestMethodNotify || req.Message == nil || *req.Message != "ready" {
		t.Errorf("ExtensionUiRequest = %+v, want the notify dialog", req)
	}

	// The generated type enforces the required list, which is why callers that
	// must tolerate a partial record keep the raw JSON.
	var state RpcSessionState
	const partial = `{"sessionId":"pi-1","thinkingLevel":"medium","isStreaming":false}`
	if err := json.Unmarshal([]byte(partial), &state); err == nil {
		t.Errorf("unmarshal of a partial session state succeeded (%+v), want the required fields enforced", state)
	}
}

// TestSchemaJSONIsIndependentOfRegistryStorage pins that embedded schemas are
// handed out as copies: a caller mutating them must not corrupt the registry.
func TestSchemaJSONIsIndependentOfRegistryStorage(t *testing.T) {
	data, ok := SchemaJSON("core")
	if !ok {
		t.Fatal(`SchemaJSON("core") not found`)
	}
	original := string(data)
	data[0] = 'x'
	again, _ := SchemaJSON("core")
	if string(again) != original {
		t.Error("SchemaJSON returned aliased storage")
	}
}
