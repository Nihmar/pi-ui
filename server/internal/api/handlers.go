package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// maxBodyBytes bounds a REST request body: the spike accepts small JSON documents only, and
// a bound is what keeps a stuck client from holding a connection open with an endless body.
const maxBodyBytes = 1 << 20

// health answers the liveness probe: no credential, no state, no cwd or names (§5.4).
func (a *api) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// server reports the build and the capabilities a client negotiates against.
func (a *api) server(w http.ResponseWriter, _ *http.Request) {
	info := a.info
	info.Protocol = Protocol
	if info.Features == nil {
		info.Features = []string{}
	}
	if info.Limits == nil {
		info.Limits = map[string]any{}
	}
	writeJSON(w, http.StatusOK, info)
}

// listSessions returns every session, live or finished, in creation order.
func (a *api) listSessions(w http.ResponseWriter, _ *http.Request) {
	infos := a.supervisor.List()
	if infos == nil {
		infos = []sessions.Info{}
	}
	writeJSON(w, http.StatusOK, struct {
		Sessions []sessions.Info `json:"sessions"`
	}{Sessions: infos})
}

// createSessionBody is the POST /api/v1/sessions payload. Unknown members are tolerated: a
// newer client may send fields this server ignores.
type createSessionBody struct {
	CWD  string `json:"cwd"`
	Name string `json:"name,omitempty"`
}

// createSession spawns one session (201) or explains why it could not: 400 for an unusable
// cwd, 409 for the session limit, 502 for a child that never became ready.
func (a *api) createSession(w http.ResponseWriter, r *http.Request) {
	var body createSessionBody
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, sessions.CodeBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.CWD) == "" {
		writeError(w, http.StatusBadRequest, sessions.CodeBadRequest, "cwd is required")
		return
	}

	info, err := a.supervisor.Start(r.Context(), sessions.Spec{CWD: body.CWD, Name: body.Name})
	if err != nil {
		code := sessions.CodeOf(err)
		writeError(w, statusFor(code), code, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, info)
}

// getSession returns one session projection.
func (a *api) getSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	info, ok := a.supervisor.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, sessions.CodeSessionNotFound, "no session "+id)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// stopSession asks one session to shut down gracefully and answers 202: the child is still
// exiting when the response is written, and the exit arrives as a server.exited event.
func (a *api) stopSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.supervisor.Stop(r.Context(), id); err != nil {
		code := sessions.CodeOf(err)
		writeError(w, statusFor(code), code, err.Error())
		return
	}
	// Report the state the session is in while the 202 is written; a client that wants the
	// final one listens on the event stream.
	if info, ok := a.supervisor.Get(id); ok {
		writeJSON(w, http.StatusAccepted, info)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"id": id, "status": string(sessions.StatusStopping)})
}

// decodeBody reads a JSON body, tolerating unknown members and rejecting anything that is
// not an object this server can read.
func decodeBody(r *http.Request, target any) error {
	body := http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	if err := json.NewDecoder(body).Decode(target); err != nil {
		return fmt.Errorf("the request body is not a JSON object: %v", err)
	}
	return nil
}
