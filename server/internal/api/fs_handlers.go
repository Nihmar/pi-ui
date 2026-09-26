package api

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/fs"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// The filesystem surface (docs/api-v1.md, "Files"): the read side is viewer, every
// mutation is operator, and each one lands in the audit trail.
//
// The confinement itself lives in internal/fs: a handler here only reads the
// arguments, calls the service and maps the failure. That is what keeps one rule —
// no path leaves the configured workspaces — true for every endpoint that will be
// added later.

// workspaces answers GET /api/v1/workspaces: the roots a client may browse.
func (a *api) workspaces(w http.ResponseWriter, _ *http.Request) {
	roots := []fs.Root{}
	if a.files != nil {
		roots = a.files.Roots()
	}
	writeJSON(w, http.StatusOK, struct {
		Roots []fs.Root `json:"roots"`
	}{Roots: roots})
}

// listFiles answers GET /api/v1/fs/list?path=.
func (a *api) listFiles(w http.ResponseWriter, r *http.Request) {
	service, ok := a.filesOrError(w)
	if !ok {
		return
	}
	entries, err := service.List(r.URL.Query().Get("path"))
	if err != nil {
		a.writeFileError(w, r, "", err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Entries []fs.Entry `json:"entries"`
	}{Entries: entries})
}

// statFile answers GET /api/v1/fs/stat?path=.
func (a *api) statFile(w http.ResponseWriter, r *http.Request) {
	service, ok := a.filesOrError(w)
	if !ok {
		return
	}
	entry, err := service.Stat(r.URL.Query().Get("path"))
	if err != nil {
		a.writeFileError(w, r, "", err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

// readFile answers GET /api/v1/files/read?path=&maxBytes=: text when the content is
// valid UTF-8, base64 when it is not, so a client never receives mangled bytes.
func (a *api) readFile(w http.ResponseWriter, r *http.Request) {
	service, ok := a.filesOrError(w)
	if !ok {
		return
	}
	maxBytes, _ := strconv.ParseInt(r.URL.Query().Get("maxBytes"), 10, 64)
	data, entry, err := service.Read(r.URL.Query().Get("path"), maxBytes)
	if err != nil {
		a.writeFileError(w, r, "", err)
		return
	}
	body := struct {
		Entry  fs.Entry `json:"entry"`
		Text   string   `json:"text,omitempty"`
		Base64 string   `json:"base64,omitempty"`
	}{Entry: entry}
	if utf8.Valid(data) {
		body.Text = string(data)
	} else {
		body.Base64 = base64.StdEncoding.EncodeToString(data)
	}
	writeJSON(w, http.StatusOK, body)
}

// writeFileBody is the PUT /api/v1/files/write payload. `text` and `base64` are
// alternatives: a binary file round-trips through the second one.
type writeFileBody struct {
	Path           string  `json:"path"`
	Text           *string `json:"text,omitempty"`
	Base64         string  `json:"base64,omitempty"`
	ExpectedSha256 string  `json:"expectedSha256,omitempty"`
}

// writeFile answers PUT /api/v1/files/write.
func (a *api) writeFile(w http.ResponseWriter, r *http.Request) {
	service, ok := a.filesOrError(w)
	if !ok {
		return
	}
	var body writeFileBody
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, codeOr(err, sessions.CodeBadRequest), err.Error())
		return
	}
	data, err := body.content()
	if err != nil {
		writeError(w, http.StatusBadRequest, sessions.CodeBadRequest, err.Error())
		return
	}
	entry, err := service.Write(body.Path, data, body.ExpectedSha256)
	if err != nil {
		a.writeFileError(w, r, audit.ActionFileWrite, err)
		return
	}
	event := a.auditEvent(r, audit.ActionFileWrite, audit.OutcomeOK)
	event.Target = entry.Path
	a.record(event)
	writeJSON(w, http.StatusOK, entry)
}

// deleteFileBody is the DELETE /api/v1/files/delete payload.
type deleteFileBody struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive,omitempty"`
}

// deleteFile answers DELETE /api/v1/files/delete.
func (a *api) deleteFile(w http.ResponseWriter, r *http.Request) {
	service, ok := a.filesOrError(w)
	if !ok {
		return
	}
	var body deleteFileBody
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, codeOr(err, sessions.CodeBadRequest), err.Error())
		return
	}
	if err := service.Remove(body.Path, body.Recursive); err != nil {
		a.writeFileError(w, r, audit.ActionFileDelete, err)
		return
	}
	event := a.auditEvent(r, audit.ActionFileDelete, audit.OutcomeOK)
	event.Target = body.Path
	a.record(event)
	writeJSON(w, http.StatusOK, map[string]string{"removed": body.Path})
}

// content reads the bytes of one write body.
func (b writeFileBody) content() ([]byte, error) {
	switch {
	case b.Text != nil:
		return []byte(*b.Text), nil
	case b.Base64 != "":
		data, err := base64.StdEncoding.DecodeString(b.Base64)
		if err != nil {
			return nil, sessions.Codedf(sessions.CodeBadRequest, "base64 is not decodable: %v", err)
		}
		return data, nil
	default:
		return nil, sessions.Codedf(sessions.CodeBadRequest, "a write needs text or base64")
	}
}

// filesOrError reports the missing-capability case instead of panicking: a server
// built without roots has no filesystem surface.
func (a *api) filesOrError(w http.ResponseWriter) (*fs.Service, bool) {
	if a.files == nil {
		writeError(w, http.StatusNotImplemented, sessions.CodeUnsupported,
			"no workspace is configured on this server")
		return nil, false
	}
	return a.files, true
}

// writeFileError maps a service failure onto the wire, and records the escape
// attempts a reader of the audit trail cares about.
func (a *api) writeFileError(w http.ResponseWriter, r *http.Request, action audit.Action, err error) {
	code := codeOr(err, sessions.CodeBadRequest)
	if code == codePathEscape {
		event := a.auditEvent(r, audit.ActionPathEscapeBlocked, audit.OutcomeDenied)
		event.Target = r.URL.Query().Get("path")
		event.Details = map[string]any{"method": r.Method, "action": string(action)}
		a.record(event)
	}
	writeError(w, statusFor(code), code, err.Error())
}

// codePathEscape is the code internal/fs raises for a path outside every workspace.
const codePathEscape = "path_escape"
