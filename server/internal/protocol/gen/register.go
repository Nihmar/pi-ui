package gen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// schemas holds the raw JSON document of every registered schema, keyed by
// schema name (the schemas/<name>.json file name without the extension).
//
// It is written exclusively by the init() functions of the generated
// <schema>_schema_gen.go files, before any caller can observe it, and is only
// read afterwards; no locking is needed.
var schemas = map[string]json.RawMessage{}

// registerSchema records the raw JSON document of one schema under name. It is
// called from init() in generated files and must not be called anywhere else.
//
// Problems here are generator bugs, not runtime conditions, so they panic
// during program start-up: an empty or duplicate name, or a document that is
// not valid JSON, would otherwise surface much later as a validation failure
// or a silent pass-through.
func registerSchema(name string, data []byte) {
	if name == "" {
		panic("protocol/gen: registerSchema called with an empty schema name")
	}
	if _, dup := schemas[name]; dup {
		panic(fmt.Sprintf("protocol/gen: schema %q registered twice", name))
	}
	if !json.Valid(data) {
		panic(fmt.Sprintf("protocol/gen: schema %q is not valid JSON", name))
	}
	schemas[name] = json.RawMessage(bytes.Clone(data))
}

// SchemaJSON returns the raw JSON document of the schema registered under
// name, exactly as committed in schemas/<name>.json. The returned bytes belong
// to the caller. ok is false when no schema with that name has been generated
// yet.
func SchemaJSON(name string) ([]byte, bool) {
	data, ok := schemas[name]
	if !ok {
		return nil, false
	}
	return bytes.Clone(data), true
}

// Schemas returns the names of all registered schemas in lexicographic order,
// for diagnostics, /api/v1/server features and start-up logs.
func Schemas() []string {
	names := make([]string, 0, len(schemas))
	for name := range schemas {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
