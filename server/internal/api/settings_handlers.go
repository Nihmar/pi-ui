package api

import (
	"encoding/json"
	"net/http"

	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
	"github.com/Nihmar/pi-ui/server/internal/settings"
)

// The settings surface (docs/api-v1.md, "Settings"): reading is viewer, changing is
// admin, and every change is audited as settings.update with the keys it touched.
//
// The catalogue lives in internal/settings: a key this server does not know is a
// bad_request here, never a value stored under a name nothing reads.

// settingsBody is what a client receives: the effective values, the defaults and the
// catalogue that explains them.
type settingsBody struct {
	Values   map[string]json.RawMessage `json:"values"`
	Defaults map[string]json.RawMessage `json:"defaults"`
	Known    []settingsEntry            `json:"known"`
}

// settingsEntry is one catalogue row.
type settingsEntry struct {
	Key         string          `json:"key"`
	Kind        string          `json:"kind"`
	Default     json.RawMessage `json:"default"`
	Description string          `json:"description"`
}

// getSettings answers GET /api/v1/settings.
func (a *api) getSettings(w http.ResponseWriter, _ *http.Request) {
	service, ok := a.settingsOrError(w)
	if !ok {
		return
	}
	snapshot, err := service.Values()
	if err != nil {
		code := codeOr(err, sessions.CodeInternal)
		writeError(w, statusFor(code), code, err.Error())
		return
	}
	body := settingsBody{Values: snapshot.Values, Defaults: snapshot.Defaults}
	for _, definition := range snapshot.Known {
		body.Known = append(body.Known, settingsEntry{
			Key:         definition.Key,
			Kind:        string(definition.Kind),
			Default:     definition.Default,
			Description: definition.Description,
		})
	}
	writeJSON(w, http.StatusOK, body)
}

// patchSettings answers PATCH /api/v1/settings with a partial map of keys.
func (a *api) patchSettings(w http.ResponseWriter, r *http.Request) {
	service, ok := a.settingsOrError(w)
	if !ok {
		return
	}
	var changes map[string]json.RawMessage
	if err := decodeBody(r, &changes); err != nil {
		writeError(w, http.StatusBadRequest, codeOr(err, sessions.CodeBadRequest), err.Error())
		return
	}
	applied, err := service.SetMany(changes, a.actorOf(r))
	if err != nil {
		code := codeOr(err, sessions.CodeBadRequest)
		a.writeSettingsError(w, r, code, err)
		return
	}
	a.recordSettings(r, applied)
	writeJSON(w, http.StatusOK, map[string]any{
		"applied": applied,
		"values":  a.settingsValues(service),
	})
}

// deleteSetting answers DELETE /api/v1/settings/{key}: back to the default.
func (a *api) deleteSetting(w http.ResponseWriter, r *http.Request) {
	service, ok := a.settingsOrError(w)
	if !ok {
		return
	}
	key := r.PathValue("key")
	if err := service.Reset(key, a.actorOf(r)); err != nil {
		code := codeOr(err, sessions.CodeBadRequest)
		a.writeSettingsError(w, r, code, err)
		return
	}
	a.recordSettings(r, map[string]json.RawMessage{})
	writeJSON(w, http.StatusOK, map[string]any{
		"reset":  key,
		"values": a.settingsValues(service),
	})
}

// settingsValues re-reads the effective settings after a change.
func (a *api) settingsValues(service *settings.Service) map[string]json.RawMessage {
	snapshot, err := service.Values()
	if err != nil {
		return map[string]json.RawMessage{}
	}
	return snapshot.Values
}

// recordSettings writes the trail entry of one change.
func (a *api) recordSettings(r *http.Request, applied map[string]json.RawMessage) {
	keys := make([]string, 0, len(applied))
	for key := range applied {
		keys = append(keys, key)
	}
	event := a.auditEvent(r, settings.AuditAction, audit.OutcomeOK)
	event.Details = map[string]any{"keys": keys}
	a.record(event)
}

// writeSettingsError maps a failure and keeps a refusal on the trail: a settings
// change that did not happen is part of what an admin wants to read back.
func (a *api) writeSettingsError(w http.ResponseWriter, r *http.Request, code string, err error) {
	event := a.auditEvent(r, settings.AuditAction, audit.OutcomeError)
	if code == sessions.CodeBadRequest {
		event.Outcome = audit.OutcomeDenied
	}
	event.Details = map[string]any{"reason": err.Error()}
	a.record(event)
	writeError(w, statusFor(code), code, err.Error())
}

// settingsOrError reports a server without a state directory.
func (a *api) settingsOrError(w http.ResponseWriter) (*settings.Service, bool) {
	if a.settings == nil {
		writeError(w, http.StatusNotImplemented, sessions.CodeUnsupported,
			"this server has no state directory, so it has no settings")
		return nil, false
	}
	return a.settings, true
}

// featureEnabled reports whether an administrative setting allows one operation.
//
// A server without a settings service (the static-token mode) is not gated: there is
// no admin surface to turn anything on with, and the scope alone decides.
func (a *api) featureEnabled(r *http.Request, key string) bool {
	if a.settings == nil {
		return true
	}
	return a.settings.Bool(r.Context(), key)
}

// actorOf names the device behind a request, for the settings event payload and the
// audit trail: it re-authenticates, because the handler only received a scope.
func (a *api) actorOf(r *http.Request) string {
	_, deviceID, err := authenticate(a.auth, r)
	if err != nil || deviceID == "" {
		return "loopback"
	}
	return deviceID
}
