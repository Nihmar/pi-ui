package api

import (
	"net/http"

	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
	"github.com/Nihmar/pi-ui/server/internal/tasks"
)

// The task surface (docs/api-v1.md, "Tasks"): a background command is an operator
// capability, reading what runs is a viewer one, and starting or stopping one lands in
// the trail as task.start / task.stop.

// listTasks answers GET /api/v1/tasks.
func (a *api) listTasks(w http.ResponseWriter, _ *http.Request) {
	service, ok := a.tasksOrError(w)
	if !ok {
		return
	}
	list := service.List()
	if list == nil {
		list = []tasks.Task{}
	}
	writeJSON(w, http.StatusOK, struct {
		Tasks []tasks.Task `json:"tasks"`
	}{Tasks: list})
}

// getTask answers GET /api/v1/tasks/{id}: the report plus the kept output.
func (a *api) getTask(w http.ResponseWriter, r *http.Request) {
	service, ok := a.tasksOrError(w)
	if !ok {
		return
	}
	id := r.PathValue("id")
	task, found := service.Get(id)
	if !found {
		writeError(w, http.StatusNotFound, sessions.CodeNotFound, "no task "+id)
		return
	}
	output, _ := service.Output(id)
	writeJSON(w, http.StatusOK, struct {
		Task   tasks.Task `json:"task"`
		Output string     `json:"output"`
	}{Task: task, Output: output})
}

// startTaskBody is the POST /api/v1/tasks payload.
type startTaskBody struct {
	Name    string   `json:"name,omitempty"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Dir     string   `json:"dir"`
}

// startTask answers POST /api/v1/tasks.
func (a *api) startTask(w http.ResponseWriter, r *http.Request) {
	service, ok := a.tasksOrError(w)
	if !ok {
		return
	}
	var body startTaskBody
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, codeOr(err, sessions.CodeBadRequest), err.Error())
		return
	}
	task, err := service.Start(r.Context(), tasks.Spec{
		Name:    body.Name,
		Command: body.Command,
		Args:    body.Args,
		Dir:     body.Dir,
	}, a.actorOf(r))
	if err != nil {
		a.writeTaskError(w, r, sessions.CodeOf(err), err)
		return
	}
	event := a.auditEvent(r, audit.ActionTaskStart, audit.OutcomeOK)
	event.Target = task.ID
	event.Details = map[string]any{"command": task.Command, "dir": task.Dir}
	a.record(event)
	writeJSON(w, http.StatusCreated, task)
}

// stopTask answers POST /api/v1/tasks/{id}/stop.
func (a *api) stopTask(w http.ResponseWriter, r *http.Request) {
	service, ok := a.tasksOrError(w)
	if !ok {
		return
	}
	id := r.PathValue("id")
	task, err := service.Stop(id)
	if err != nil {
		a.writeTaskError(w, r, codeOr(err, sessions.CodeBadRequest), err)
		return
	}
	event := a.auditEvent(r, audit.ActionTaskStop, audit.OutcomeOK)
	event.Target = id
	a.record(event)
	writeJSON(w, http.StatusOK, task)
}

// writeTaskError maps a failure and keeps a refused start on the trail: an admin
// wants to read that somebody tried to run something the workspaces do not allow.
func (a *api) writeTaskError(w http.ResponseWriter, r *http.Request, code string, err error) {
	if code == codePathEscape {
		event := a.auditEvent(r, audit.ActionPathEscapeBlocked, audit.OutcomeDenied)
		event.Details = map[string]any{"method": r.Method, "action": "task.start"}
		a.record(event)
	}
	writeError(w, statusFor(code), code, err.Error())
}

// tasksOrError reports a server without a workspace.
func (a *api) tasksOrError(w http.ResponseWriter) (*tasks.Service, bool) {
	if a.tasks == nil {
		writeError(w, http.StatusNotImplemented, sessions.CodeUnsupported,
			"no workspace is configured on this server")
		return nil, false
	}
	return a.tasks, true
}
