package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Nihmar/pi-ui/server/internal/audit"
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
	if draining, message := a.drain.drainingNow(); draining {
		// A client that wonders why a create came back 503 reads it here. The map is
		// copied first: mutating the one the caller passed would make one request's
		// answer depend on another's.
		limits := make(map[string]any, len(info.Limits)+2)
		for key, value := range info.Limits {
			limits[key] = value
		}
		limits["draining"] = true
		limits["drainingSince"] = message
		info.Limits = limits
	}
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
// cwd, 409 for the session limit, 413 for a body above the cap, 502 for a child that never
// became ready.
func (a *api) createSession(w http.ResponseWriter, r *http.Request) {
	var body createSessionBody
	if err := decodeBody(r, &body); err != nil {
		code := codeOr(err, sessions.CodeBadRequest)
		writeError(w, statusFor(code), code, err.Error())
		return
	}
	if err := a.drainRefusal(); err != nil {
		writeError(w, http.StatusServiceUnavailable, sessions.CodeUnavailable, err.Error())
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
	created := a.auditEvent(r, audit.ActionSessionCreate, audit.OutcomeOK)
	created.SessionID = info.ID
	created.Target = info.ID
	a.record(created)
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

// stopSession asks one session to shut down gracefully and answers 202 with the state the
// session is in when the response is written: Stop waits for the terminal status, so the
// body already reports exited or crashed, and a client that prefers the event still gets
// it on the stream. 202 and not 200 on purpose: the session is a resource the client asked
// to take down, not one this response created.
func (a *api) stopSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := a.supervisor.Stop(r.Context(), id); err != nil {
		code := sessions.CodeOf(err)
		writeError(w, statusFor(code), code, err.Error())
		return
	}
	stopped := a.auditEvent(r, audit.ActionSessionStop, audit.OutcomeOK)
	stopped.SessionID = id
	stopped.Target = id
	a.record(stopped)
	if info, ok := a.supervisor.Get(id); ok {
		writeJSON(w, http.StatusAccepted, info)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"id": id, "status": string(sessions.StatusStopping)})
}

// decodeBody reads a JSON body, tolerating unknown members and rejecting anything that is
// not an object this server can read. A body above maxBodyBytes is reported as too_large,
// which the router answers 413: the caller can retry with less data, while a shape error
// is a permanent bad_request.
func decodeBody(r *http.Request, target any) error {
	raw, err := readBody(r)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("the request body is not a JSON object: %v", err)
	}
	return nil
}

// readBody reads a bounded request body into memory, so a handler that must validate the
// raw payload before decoding it (the auth surface validates against schemas/server.json)
// can do both without reading the body twice. The error carries too_large for a body above
// the cap, which the router answers 413.
func readBody(r *http.Request) ([]byte, error) {
	body := http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	raw, err := io.ReadAll(body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, sessions.Codedf(sessions.CodeTooLarge, "the request body exceeds %d bytes", tooLarge.Limit)
		}
		return nil, fmt.Errorf("the request body could not be read: %v", err)
	}
	return raw, nil
}
