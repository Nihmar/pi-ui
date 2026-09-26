package api

import (
	"net/http"

	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
	"github.com/Nihmar/pi-ui/server/internal/tasks"
	"github.com/Nihmar/pi-ui/server/internal/updates"
)

// The updates surface (docs/api-v1.md, "Updates"): checking is a read for an admin, and
// applying is *running the command the operator configured* — which is why it goes
// through the task runner and why a deployment that manages its own updates answers
// managed_mode instead of guessing how to replace itself.

// getUpdates answers GET /api/v1/updates: what this deployment runs and what is out
// there.
func (a *api) getUpdates(w http.ResponseWriter, r *http.Request) {
	service := a.updates
	if service == nil {
		writeError(w, http.StatusNotImplemented, sessions.CodeUnsupported,
			"this server does not report updates")
		return
	}
	reports := service.Check(r.Context())
	writeJSON(w, http.StatusOK, struct {
		Components []updates.Report `json:"components"`
		// Managed is true when applying is not this server's job (no update command
		// configured): a client hides the button instead of offering a 409.
		Managed bool `json:"managed"`
	}{Components: reports, Managed: a.updateCommand == ""})
}

// applyUpdatesBody is the POST /api/v1/updates/apply payload.
type applyUpdatesBody struct {
	// Components names what to update; empty means everything the command covers.
	Components []string `json:"components,omitempty"`
}

// applyUpdates answers POST /api/v1/updates/apply.
//
// It starts the configured update command as a background task and returns the task, so
// a client watches the update the same way it watches a build: with the task's output.
// The server never replaces its own binary: that is the operator's script.
func (a *api) applyUpdates(w http.ResponseWriter, r *http.Request) {
	if a.updateCommand == "" {
		event := a.auditEvent(r, audit.ActionUpdatesApply, audit.OutcomeDenied)
		event.Details = map[string]any{"reason": "managed mode"}
		a.record(event)
		writeError(w, http.StatusConflict, sessions.CodeManagedMode,
			"this deployment manages its own updates; set --update-command to let the server run one")
		return
	}
	service, ok := a.tasksOrError(w)
	if !ok {
		return
	}
	var body applyUpdatesBody
	if r.ContentLength > 0 {
		if err := decodeBody(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, codeOr(err, sessions.CodeBadRequest), err.Error())
			return
		}
	}
	roots := a.rootPaths()
	if len(roots) == 0 {
		writeError(w, http.StatusNotImplemented, sessions.CodeUnsupported,
			"no workspace is configured on this server")
		return
	}
	task, err := service.Start(r.Context(), tasks.Spec{
		Name:    "update",
		Command: a.updateCommand,
		Args:    a.updateCommandArgs(body.Components),
		Dir:     roots[0],
	}, a.actorOf(r))
	if err != nil {
		code := codeOr(err, sessions.CodeBadRequest)
		writeError(w, statusFor(code), code, err.Error())
		return
	}
	event := a.auditEvent(r, audit.ActionUpdatesApply, audit.OutcomeOK)
	event.Target = task.ID
	event.Details = map[string]any{"components": body.Components, "command": a.updateCommand}
	a.record(event)
	writeJSON(w, http.StatusAccepted, task)
}

// updateCommandArgs turns the requested components into command arguments: a deployment
// that names a component gets them, one that does not gets none.
func (a *api) updateCommandArgs(components []string) []string {
	if len(components) == 0 {
		return nil
	}
	args := make([]string, 0, len(components))
	return append(args, components...)
}

// rootPaths lists the configured roots: an update command runs inside the first one,
// because it is the only directory this server is allowed to work in.
func (a *api) rootPaths() []string {
	if a.files == nil {
		return nil
	}
	roots := a.files.Roots()
	paths := make([]string, 0, len(roots))
	for _, root := range roots {
		paths = append(paths, root.Path)
	}
	return paths
}
