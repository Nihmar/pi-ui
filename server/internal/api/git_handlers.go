package api

import (
	"net/http"
	"strconv"

	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/git"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
	"github.com/Nihmar/pi-ui/server/internal/settings"
)

// The git surface (docs/api-v1.md, "Git"): reads are viewer, the two mutations are
// operator and land in the audit trail as git.write. The confinement and the parsing
// live in internal/git; a handler here decodes, calls and maps.

// gitStatus answers GET /api/v1/git/status?dir=.
func (a *api) gitStatus(w http.ResponseWriter, r *http.Request) {
	service, ok := a.gitOrError(w)
	if !ok {
		return
	}
	status, err := service.Status(r.Context(), r.URL.Query().Get("dir"))
	if err != nil {
		writeError(w, statusFor(codeOr(err, "bad_request")), codeOr(err, "bad_request"), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// gitLog answers GET /api/v1/git/log?dir=&limit=.
func (a *api) gitLog(w http.ResponseWriter, r *http.Request) {
	service, ok := a.gitOrError(w)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	commits, err := service.Log(r.Context(), r.URL.Query().Get("dir"), limit)
	if err != nil {
		writeError(w, statusFor(codeOr(err, "bad_request")), codeOr(err, "bad_request"), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Commits []git.Commit `json:"commits"`
	}{Commits: commits})
}

// gitDiff answers GET /api/v1/git/diff?dir=&path=&staged=.
func (a *api) gitDiff(w http.ResponseWriter, r *http.Request) {
	service, ok := a.gitOrError(w)
	if !ok {
		return
	}
	query := r.URL.Query()
	staged, _ := strconv.ParseBool(query.Get("staged"))
	diff, err := service.Diff(r.Context(), query.Get("dir"), query.Get("path"), staged)
	if err != nil {
		writeError(w, statusFor(codeOr(err, "bad_request")), codeOr(err, "bad_request"), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"diff": diff, "staged": staged})
}

// gitStageBody is the POST /api/v1/git/stage payload.
type gitStageBody struct {
	Dir   string   `json:"dir"`
	Paths []string `json:"paths"`
}

// gitStage answers POST /api/v1/git/stage.
func (a *api) gitStage(w http.ResponseWriter, r *http.Request) {
	service, ok := a.gitOrError(w)
	if !ok {
		return
	}
	if !a.featureEnabled(r, settings.KeyGitWrite) {
		writeError(w, http.StatusForbidden, sessions.CodeFeatureDisabled,
			"git.write is off on this server; an admin can enable it in the settings")
		return
	}
	var body gitStageBody
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, codeOr(err, "bad_request"), err.Error())
		return
	}
	if err := service.Stage(r.Context(), body.Dir, body.Paths); err != nil {
		a.writeGitError(w, r, body.Dir, err)
		return
	}
	event := a.auditEvent(r, audit.ActionGitWrite, audit.OutcomeOK)
	event.Target = body.Dir
	event.Details = map[string]any{"op": "stage", "paths": len(body.Paths)}
	a.record(event)
	writeJSON(w, http.StatusOK, map[string]any{"staged": body.Paths})
}

// gitCommitBody is the POST /api/v1/git/commit payload.
type gitCommitBody struct {
	Dir     string `json:"dir"`
	Message string `json:"message"`
}

// gitCommit answers POST /api/v1/git/commit.
func (a *api) gitCommit(w http.ResponseWriter, r *http.Request) {
	service, ok := a.gitOrError(w)
	if !ok {
		return
	}
	if !a.featureEnabled(r, settings.KeyGitWrite) {
		writeError(w, http.StatusForbidden, sessions.CodeFeatureDisabled,
			"git.write is off on this server; an admin can enable it in the settings")
		return
	}
	var body gitCommitBody
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, codeOr(err, "bad_request"), err.Error())
		return
	}
	commit, err := service.Commit(r.Context(), body.Dir, body.Message)
	if err != nil {
		a.writeGitError(w, r, body.Dir, err)
		return
	}
	event := a.auditEvent(r, audit.ActionGitWrite, audit.OutcomeOK)
	event.Target = body.Dir
	event.Details = map[string]any{"op": "commit", "commit": commit.Short}
	a.record(event)
	writeJSON(w, http.StatusOK, commit)
}

// gitOrError reports a server built without a workspace instead of panicking.
func (a *api) gitOrError(w http.ResponseWriter) (*git.Service, bool) {
	if a.git == nil {
		writeError(w, http.StatusNotImplemented, "unsupported",
			"no workspace is configured on this server")
		return nil, false
	}
	return a.git, true
}

// writeGitError maps a failure and records the escapes a reviewer cares about.
func (a *api) writeGitError(w http.ResponseWriter, r *http.Request, dir string, err error) {
	code := codeOr(err, "bad_request")
	if code == codePathEscape {
		event := a.auditEvent(r, audit.ActionPathEscapeBlocked, audit.OutcomeDenied)
		event.Target = dir
		event.Details = map[string]any{"method": r.Method, "action": "git.write"}
		a.record(event)
	}
	writeError(w, statusFor(code), code, err.Error())
}
