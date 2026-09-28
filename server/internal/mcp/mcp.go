// Package mcp owns the MCP server configuration the bridge extension reads.
//
// The server never speaks the Model Context Protocol itself: it stores and validates the
// document that says which MCP servers a pi child should connect to, and the bridge
// inside the child is what talks to them. What this package does own is the two rules
// that make that safe: the shape of a server entry is validated before it is written, and
// a stored secret never leaves the process — `env` and `headers` values are returned
// redacted, and the sentinel a client sends back means "keep what you have".
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// Redacted is what a client sees in place of a stored secret, and what it sends back to
// mean "do not change it".
const Redacted = "***"

// maxServers bounds the document: a config listing hundreds of MCP servers is a mistake,
// not a configuration.
const maxServers = 64

// Server is one MCP server as the bridge should start (or connect to) it.
//
// Exactly one of Command (a stdio server) or URL (a remote one) is required. Unknown
// fields a newer writer put in the file are kept in Extra and written back untouched, so
// this server can be older than the bridge without truncating its configuration: the
// marshalling below puts them on the wire next to the known ones, which is what lets a
// client that only reads and writes the document hand them back.
type Server struct {
	// Command starts a stdio MCP server.
	Command string `json:"command,omitempty"`
	// Args are its arguments.
	Args []string `json:"args,omitempty"`
	// Env is the environment for a stdio server. Values are secrets: they are redacted
	// on read.
	Env map[string]string `json:"env,omitempty"`
	// URL is a remote MCP endpoint.
	URL string `json:"url,omitempty"`
	// Headers goes with URL. Values are secrets too.
	Headers map[string]string `json:"headers,omitempty"`
	// Enabled is false for a server kept in the file but not started. Absent means true.
	Enabled *bool `json:"enabled,omitempty"`
	// Extra is every other field the file carried, preserved verbatim.
	Extra map[string]json.RawMessage `json:"-"`
}

// MarshalJSON renders one entry with the unknown fields the file carried, so a reader can
// send the entry back without losing anything.
func (s Server) MarshalJSON() ([]byte, error) {
	fields := make(map[string]json.RawMessage, len(s.Extra)+6)
	for key, value := range s.Extra {
		fields[key] = value
	}
	if s.Command != "" {
		if err := setField(fields, "command", s.Command); err != nil {
			return nil, err
		}
	}
	if len(s.Args) > 0 {
		if err := setField(fields, "args", s.Args); err != nil {
			return nil, err
		}
	}
	if len(s.Env) > 0 {
		if err := setField(fields, "env", s.Env); err != nil {
			return nil, err
		}
	}
	if s.URL != "" {
		if err := setField(fields, "url", s.URL); err != nil {
			return nil, err
		}
	}
	if len(s.Headers) > 0 {
		if err := setField(fields, "headers", s.Headers); err != nil {
			return nil, err
		}
	}
	if s.Enabled != nil {
		if err := setField(fields, "enabled", *s.Enabled); err != nil {
			return nil, err
		}
	}
	return json.Marshal(fields)
}

// UnmarshalJSON reads one entry, keeping the fields this version does not know in `Extra`.
// A known field with the wrong shape is left at its zero value rather than failing the
// whole document: `validate` reports what a client has to fix.
func (s *Server) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*s = Server{Extra: map[string]json.RawMessage{}}
	for key, value := range fields {
		switch key {
		case "command":
			_ = json.Unmarshal(value, &s.Command)
		case "args":
			_ = json.Unmarshal(value, &s.Args)
		case "env":
			_ = json.Unmarshal(value, &s.Env)
		case "url":
			_ = json.Unmarshal(value, &s.URL)
		case "headers":
			_ = json.Unmarshal(value, &s.Headers)
		case "enabled":
			var enabled bool
			if err := json.Unmarshal(value, &enabled); err == nil {
				s.Enabled = &enabled
			}
		default:
			s.Extra[key] = value
		}
	}
	return nil
}

// setField encodes one value into a raw-message map.
func setField(fields map[string]json.RawMessage, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("mcp: encoding %s: %w", key, err)
	}
	fields[key] = encoded
	return nil
}

// Disabled reports whether the entry asks the bridge to skip this server.
func (s Server) Disabled() bool { return s.Enabled != nil && !*s.Enabled }

// Document is the whole configuration file.
//
// It marshals to the file's own shape: the known fields plus whatever `Extra` kept, in
// both directions. That is what makes the promise hold — a reader receives the fields a
// newer writer stored, so the write that follows cannot drop them.
type Document struct {
	// Servers is the catalogue, by name.
	Servers map[string]Server `json:"servers"`
	// Version is an optional document version a newer bridge might use.
	Version int `json:"version,omitempty"`
	// Extra is every other top-level field, preserved verbatim.
	Extra map[string]json.RawMessage `json:"-"`
}

// MarshalJSON renders the document as the file has it: the unknown top-level fields first,
// then the known ones.
func (d Document) MarshalJSON() ([]byte, error) {
	fields := make(map[string]json.RawMessage, len(d.Extra)+2)
	for key, value := range d.Extra {
		fields[key] = value
	}
	servers := d.Servers
	if servers == nil {
		servers = map[string]Server{}
	}
	if err := setField(fields, "servers", servers); err != nil {
		return nil, err
	}
	if d.Version != 0 {
		if err := setField(fields, "version", d.Version); err != nil {
			return nil, err
		}
	}
	return json.Marshal(fields)
}

// UnmarshalJSON reads a document, keeping every field this version does not know in
// `Extra`. A known field with the wrong shape is left at its zero value instead of
// failing the whole document: the shape of one entry is `validate`'s business.
func (d *Document) UnmarshalJSON(data []byte) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return err
	}
	*d = Document{Servers: map[string]Server{}, Extra: map[string]json.RawMessage{}}
	for key, value := range top {
		switch key {
		case "version":
			_ = json.Unmarshal(value, &d.Version)
		case "servers":
			if err := json.Unmarshal(value, &d.Servers); err != nil {
				return fmt.Errorf("%w: %v", errServersShape, err)
			}
		default:
			d.Extra[key] = value
		}
	}
	if d.Servers == nil {
		d.Servers = map[string]Server{}
	}
	if d.Extra == nil {
		d.Extra = map[string]json.RawMessage{}
	}
	return nil
}

// Service reads and writes one MCP configuration file.
type Service struct {
	path string
}

// New builds the service over one path. An empty path is not an error: the server then
// answers 501, which is what a deployment without MCP should say.
func New(path string) *Service { return &Service{path: path} }

// Path is the file this service owns.
func (s *Service) Path() string { return s.path }

// Configured reports whether a path was given.
func (s *Service) Configured() bool { return s.path != "" }

// Read returns the document with every secret redacted.
func (s *Service) Read() (Document, error) {
	if !s.Configured() {
		return Document{}, sessions.Codedf(sessions.CodeUnsupported,
			"no MCP configuration file is configured on this server")
	}
	raw, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// A server without an MCP file is a server with no MCP servers, not an error:
		// the first PUT creates it.
		return Document{Servers: map[string]Server{}}, nil
	case err != nil:
		return Document{}, sessions.Codedf(sessions.CodeInternal,
			"the MCP configuration cannot be read: %v", err)
	}
	document, err := parse(raw)
	if err != nil {
		return Document{}, err
	}
	return redact(document), nil
}

// Write validates a document, keeps the secrets the client did not resend and stores the
// file atomically. It returns what a reader now sees (redacted).
func (s *Service) Write(incoming Document) (Document, error) {
	if !s.Configured() {
		return Document{}, sessions.Codedf(sessions.CodeUnsupported,
			"no MCP configuration file is configured on this server")
	}
	if len(incoming.Servers) > maxServers {
		return Document{}, sessions.Codedf(sessions.CodeBadRequest,
			"at most %d MCP servers are allowed", maxServers)
	}
	stored, err := s.readRaw()
	if err != nil {
		return Document{}, err
	}
	merged := Document{Version: incoming.Version, Extra: incoming.Extra, Servers: map[string]Server{}}
	for name, server := range incoming.Servers {
		if strings.TrimSpace(name) == "" {
			return Document{}, sessions.Codedf(sessions.CodeBadRequest, "an MCP server needs a name")
		}
		if err := validate(name, server); err != nil {
			return Document{}, err
		}
		if previous, ok := stored.Servers[name]; ok {
			server.Env = keepSecrets(name, "env", server.Env, previous.Env)
			server.Headers = keepSecrets(name, "headers", server.Headers, previous.Headers)
		} else {
			for key, value := range server.Env {
				if value == Redacted {
					return Document{}, sessions.Codedf(sessions.CodeBadRequest,
						"%s.env.%s is the redaction sentinel but nothing is stored to keep: send the real value", name, key)
				}
			}
			for key, value := range server.Headers {
				if value == Redacted {
					return Document{}, sessions.Codedf(sessions.CodeBadRequest,
						"%s.headers.%s is the redaction sentinel but nothing is stored to keep: send the real value", name, key)
				}
			}
		}
		merged.Servers[name] = server
	}
	if merged.Servers == nil {
		merged.Servers = map[string]Server{}
	}
	if err := s.write(merged); err != nil {
		return Document{}, err
	}
	return redact(merged), nil
}

// EnabledNames lists the servers the bridge should start, in a stable order.
func (d Document) EnabledNames() []string {
	names := make([]string, 0, len(d.Servers))
	for name, server := range d.Servers {
		if !server.Disabled() {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// readRaw reads the stored document without redacting it, for the merge.
func (s *Service) readRaw() (Document, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Document{Servers: map[string]Server{}}, nil
	}
	if err != nil {
		return Document{}, sessions.Codedf(sessions.CodeInternal,
			"the MCP configuration cannot be read: %v", err)
	}
	return parse(raw)
}

// keepSecrets replaces the sentinel with the stored value, so a client that reads a
// redacted document can write it back without losing anything.
func keepSecrets(name, field string, incoming, stored map[string]string) map[string]string {
	if incoming == nil {
		return nil
	}
	kept := make(map[string]string, len(incoming))
	for key, value := range incoming {
		if value == Redacted {
			if previous, ok := stored[key]; ok {
				kept[key] = previous
				continue
			}
		}
		kept[key] = value
	}
	return kept
}

// validate checks one server entry.
func validate(name string, server Server) error {
	hasCommand := strings.TrimSpace(server.Command) != ""
	hasURL := strings.TrimSpace(server.URL) != ""
	switch {
	case hasCommand && hasURL:
		return sessions.Codedf(sessions.CodeBadRequest,
			"%s has both command and url: a stdio server and a remote one are different entries", name)
	case !hasCommand && !hasURL:
		return sessions.Codedf(sessions.CodeBadRequest,
			"%s needs a command (stdio) or a url (remote)", name)
	}
	if hasURL {
		if !strings.HasPrefix(server.URL, "http://") && !strings.HasPrefix(server.URL, "https://") {
			return sessions.Codedf(sessions.CodeBadRequest,
				"%s.url must be http or https", name)
		}
	}
	for key, value := range server.Env {
		if strings.TrimSpace(key) == "" {
			return sessions.Codedf(sessions.CodeBadRequest, "%s has an empty env name", name)
		}
		if strings.ContainsRune(value, '\x00') {
			return sessions.Codedf(sessions.CodeBadRequest, "%s.env.%s contains a NUL byte", name, key)
		}
	}
	for key, value := range server.Headers {
		if strings.TrimSpace(key) == "" {
			return sessions.Codedf(sessions.CodeBadRequest, "%s has an empty header name", name)
		}
		if strings.ContainsRune(value, '\x00') {
			return sessions.Codedf(sessions.CodeBadRequest, "%s.headers.%s contains a NUL byte", name, key)
		}
	}
	return nil
}

// redact returns a copy with every secret replaced by the sentinel.
func redact(document Document) Document {
	out := Document{Version: document.Version, Extra: document.Extra, Servers: map[string]Server{}}
	for name, server := range document.Servers {
		copied := server
		copied.Env = redactValues(server.Env)
		copied.Headers = redactValues(server.Headers)
		copied.Args = append([]string(nil), server.Args...)
		out.Servers[name] = copied
	}
	return out
}

// redactValues hides every value of a secret map.
func redactValues(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	hidden := make(map[string]string, len(values))
	for key := range values {
		hidden[key] = Redacted
	}
	return hidden
}

// errServersShape reports a `servers` member that is not an object. It exists so `parse`
// keeps the sentence a client reads: the shape of one member, not "the file is broken".
var errServersShape = errors.New("the MCP servers are not an object")

// parse reads a document, keeping the fields this version does not know about.
func parse(raw []byte) (Document, error) {
	var document Document
	if err := json.Unmarshal(raw, &document); err != nil {
		if errors.Is(err, errServersShape) {
			return Document{}, sessions.Codedf(sessions.CodeBadRequest, "%v", err)
		}
		return Document{}, sessions.Codedf(sessions.CodeBadRequest,
			"the MCP configuration is not a JSON object: %v", err)
	}
	if document.Extra == nil {
		document.Extra = map[string]json.RawMessage{}
	}
	return document, nil
}

// write stores the document atomically: a temporary file in the same directory, renamed
// over the target, so a bridge reading it never sees half a JSON object. It is 0600
// because it holds secrets.
func (s *Service) write(document Document) error {
	encoded, err := encode(document)
	if err != nil {
		return sessions.Codedf(sessions.CodeInternal, "the MCP configuration cannot be encoded: %v", err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return sessions.Codedf(sessions.CodeInternal, "the MCP configuration directory: %v", err)
	}
	tmp, err := os.CreateTemp(dir, ".mcp-*.tmp")
	if err != nil {
		return sessions.Codedf(sessions.CodeInternal, "the MCP configuration cannot be written: %v", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return sessions.Codedf(sessions.CodeInternal, "the MCP configuration: %v", err)
	}
	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		return sessions.Codedf(sessions.CodeInternal, "the MCP configuration: %v", err)
	}
	if err := tmp.Close(); err != nil {
		return sessions.Codedf(sessions.CodeInternal, "the MCP configuration: %v", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return sessions.Codedf(sessions.CodeInternal, "the MCP configuration cannot be stored: %v", err)
	}
	return nil
}

// encode renders the document, putting the unknown fields back where they were.
func encode(document Document) ([]byte, error) {
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// AuditAction is the trail entry one change writes.
const AuditAction = audit.ActionMcpUpdate
