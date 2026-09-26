package gen

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"testing"
)

// ensureTestSchema registers one test-only schema once per process. Names are
// prefixed "test_" so they cannot collide with the generated <schema>_gen.go
// files, and re-registering is a no-op so `go test -count=N` (same process)
// stays green.
func ensureTestSchema(t *testing.T, name, doc string) {
	t.Helper()
	if data, ok := SchemaJSON(name); ok {
		if string(data) != doc {
			t.Fatalf("test schema %q already registered with different content: %s", name, data)
		}
		return
	}
	registerSchema(name, []byte(doc))
}

func TestSchemaJSONUnknown(t *testing.T) {
	if data, ok := SchemaJSON("definitely-not-a-schema"); ok || data != nil {
		t.Fatalf("SchemaJSON returned (%q, %v), want (nil, false)", data, ok)
	}
}

func TestSchemasIsSortedAndRegistered(t *testing.T) {
	names := Schemas()
	if !sort.StringsAreSorted(names) {
		t.Fatalf("Schemas() = %v, want lexicographic order", names)
	}
	for _, name := range names {
		data, ok := SchemaJSON(name)
		if !ok {
			t.Fatalf("Schemas() lists %q but SchemaJSON does not know it", name)
		}
		if !json.Valid(data) {
			t.Fatalf("SchemaJSON(%q) returned invalid JSON", name)
		}
	}
}

func TestSchemaJSONRoundTrip(t *testing.T) {
	const (
		name = "test_round_trip"
		doc  = `{"$schema":"https://json-schema.org/draft/2020-12/schema"}`
	)
	ensureTestSchema(t, name, doc)

	data, ok := SchemaJSON(name)
	if !ok {
		t.Fatalf("SchemaJSON(%q) not found after registration", name)
	}
	if string(data) != doc {
		t.Fatalf("SchemaJSON(%q) = %s, want %s", name, data, doc)
	}
	if !slices.Contains(Schemas(), name) {
		t.Fatalf("Schemas() = %v, want it to contain %q", Schemas(), name)
	}

	// The registry must not hand out its own storage: mutating the returned
	// bytes may not corrupt later reads.
	data[0] = 'x'
	again, _ := SchemaJSON(name)
	if string(again) != doc {
		t.Fatalf("SchemaJSON(%q) returned aliased storage: %s", name, again)
	}
}

func TestRegisterSchemaPanics(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		want   string
	}{
		{name: "", schema: `{}`, want: "empty schema name"},
		{name: "test_invalid", schema: `{`, want: "not valid JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+tt.want, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("registerSchema(%q, %q) did not panic", tt.name, tt.schema)
				}
				if msg, ok := r.(string); !ok || !strings.Contains(msg, tt.want) {
					t.Fatalf("panic = %v, want a message containing %q", r, tt.want)
				}
			}()
			registerSchema(tt.name, []byte(tt.schema))
		})
	}
}

func TestRegisterSchemaDuplicatePanics(t *testing.T) {
	const (
		name = "test_duplicate"
		doc  = `{"type":"object"}`
	)
	ensureTestSchema(t, name, doc)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("registering %q twice did not panic", name)
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, "registered twice") {
			t.Fatalf("panic = %v, want a message containing %q", r, "registered twice")
		}
	}()
	registerSchema(name, []byte(doc))
}
