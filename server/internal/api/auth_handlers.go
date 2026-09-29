package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/auth"
	"github.com/Nihmar/pi-ui/server/internal/protocol/gen"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// timestampLayout is the wire format of every timestamp this surface writes: RFC3339
// UTC with milliseconds (schemas/core.json SrvTimestamp).
const timestampLayout = "2006-01-02T15:04:05.000Z"

// pair exchanges a pairing code or the admin password for a device token. It is the
// only endpoint besides /health that takes no credential: the credential is the
// request body. A legacy client that still sends a `secret` field is accepted and the
// field is ignored (objects are lenient).
func (a *api) pair(w http.ResponseWriter, r *http.Request) {
	service := a.authService
	if service == nil {
		writeError(w, http.StatusNotImplemented, sessions.CodeUnsupported,
			"device pairing is not configured on this server")
		return
	}

	raw, err := readBody(r)
	if err != nil {
		code := codeOr(err, sessions.CodeBadRequest)
		writeError(w, statusFor(code), code, err.Error())
		return
	}
	if err := validateServerDef(raw, a.pairSchema, "SrvPairRequest"); err != nil {
		writeError(w, http.StatusBadRequest, sessions.CodeBadRequest, err.Error())
		return
	}
	var body gen.SrvPairRequest
	if err := json.Unmarshal(raw, &body); err != nil {
		writeError(w, http.StatusBadRequest, sessions.CodeBadRequest, "the pairing body is not readable")
		return
	}

	result, err := service.Pair(auth.PairRequest{
		DeviceName: body.DeviceName,
		Platform:   deref(body.Platform),
		Code:       deref(body.Code),
		Password:   deref(body.Password),
	}, clientIP(r))
	if err != nil {
		denied := a.auditEvent(r, audit.ActionAuthDenied, audit.OutcomeDenied)
		denied.Details = map[string]any{"method": r.Method, "path": "/api/v1/auth/pair", "reason": authCode(err)}
		a.record(denied)
		writeAuthError(w, err)
		return
	}
	paired := audit.Event{
		Action:        audit.ActionAuthPair,
		Outcome:       audit.OutcomeOK,
		ActorDeviceID: result.Device.ID,
		ActorName:     result.Device.Name,
		ActorScope:    string(result.Device.Scope),
		Target:        result.Device.ID,
		RemoteAddr:    clientIP(r),
		Details:       map[string]any{"platform": result.Device.Platform},
	}
	a.record(paired)
	writeJSON(w, http.StatusCreated, pairResponse(result, a.info))
}

// refresh rotates the token of the authenticated device. The scope is viewer at
// minimum: even a read-only device may renew its own credential.
func (a *api) refresh(w http.ResponseWriter, r *http.Request) {
	service := a.authService
	if service == nil {
		writeError(w, http.StatusNotImplemented, sessions.CodeUnsupported,
			"device pairing is not configured on this server")
		return
	}
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, sessions.CodeUnauthorized,
			"a device token is required to refresh it")
		return
	}
	deviceID := deviceFrom(r.Context())
	if ok, retry := a.refreshRateLimit.Allow(rateKey(deviceID, r)); !ok {
		a.writeRateLimited(w, r, scopeFrom(r.Context()), deviceID, retry, "refresh")
		return
	}
	result, err := service.Refresh(token)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, refreshResponse(result))
}

// listDevices reports the paired devices to an admin. The caller is marked with
// current:true so the app can label itself without seeing ids it cannot compare.
func (a *api) listDevices(w http.ResponseWriter, r *http.Request) {
	service := a.authService
	if service == nil {
		writeError(w, http.StatusNotImplemented, sessions.CodeUnsupported,
			"device pairing is not configured on this server")
		return
	}
	current := deviceFrom(r.Context())
	devices := service.Devices()
	out := gen.SrvDevicesResponse{Devices: make([]gen.SrvDevice, 0, len(devices))}
	for _, device := range devices {
		out.Devices = append(out.Devices, deviceDTO(device, device.ID == current))
	}
	writeJSON(w, http.StatusOK, out)
}

// revokeDevice revokes one device immediately. The WebSocket layer closes that
// device's connections through the service's revoke hook.
func (a *api) revokeDevice(w http.ResponseWriter, r *http.Request) {
	service := a.authService
	if service == nil {
		writeError(w, http.StatusNotImplemented, sessions.CodeUnsupported,
			"device pairing is not configured on this server")
		return
	}
	deviceID := r.PathValue("id")
	if err := service.Revoke(deviceID); err != nil {
		writeAuthError(w, err)
		return
	}
	revoked := a.auditEvent(r, audit.ActionDeviceRevoke, audit.OutcomeOK)
	revoked.Target = deviceID
	a.record(revoked)
	// 204: the device list is what a client re-reads; there is nothing to return.
	w.WriteHeader(http.StatusNoContent)
}

// compilePairSchema compiles schemas/server.json#/$defs/SrvPairRequest once, at router
// construction. A failure is a build-time defect of the repository (a missing gen.sh run,
// or a schema whose refs do not resolve), so it panics with the reason instead of
// degrading into a server that accepts unvalidated pairing bodies.
func compilePairSchema() *jsonschema.Schema {
	data, ok := gen.SchemaJSON("server")
	if !ok {
		panic("api: schemas/server.json is not embedded; run server/scripts/gen.sh")
	}
	var meta struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(data, &meta); err != nil || meta.ID == "" {
		panic("api: schemas/server.json has no usable $id; refs cannot resolve")
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		panic(fmt.Sprintf("api: decode schemas/server.json: %v", err))
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(meta.ID, doc); err != nil {
		panic(fmt.Sprintf("api: register schemas/server.json: %v", err))
	}
	schema, err := compiler.Compile(meta.ID + "#/$defs/SrvPairRequest")
	if err != nil {
		panic(fmt.Sprintf("api: compile schemas/server.json#/$defs/SrvPairRequest: %v", err))
	}
	return schema
}

// validateServerDef validates raw against one definition of schemas/server.json.
func validateServerDef(raw []byte, schema *jsonschema.Schema, def string) error {
	if !json.Valid(raw) {
		return fmt.Errorf("the request body is not a JSON object")
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("the request body is not readable: %v", err)
	}
	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("the request body does not match schemas/server.json#/$defs/%s", def)
	}
	return nil
}

// writeAuthError maps an auth failure onto the wire. The message never says which
// part of a credential failed: the client gets one refusal per failure family, and
// the specific reason stays in the store and the logs.
func writeAuthError(w http.ResponseWriter, err error) {
	var limited *auth.RateLimitError
	if errors.As(err, &limited) {
		seconds := int(limited.RetryAfter.Seconds())
		if limited.RetryAfter%time.Second != 0 {
			seconds++
		}
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
	}
	code := codeOr(err, sessions.CodeUnauthorized)
	writeError(w, statusFor(code), code, authMessage(err))
}

// authCode names the failure family for the trail. It is the same set the client sees,
// because the trail is admin-only and the reason a pairing failed is exactly what an
// operator wants to read there.
func authCode(err error) string {
	code := codeOr(err, sessions.CodeUnauthorized)
	return code
}

// authMessage is the client-facing message of an auth failure.
func authMessage(err error) string {
	switch {
	case errors.Is(err, auth.ErrUnauthorized), errors.Is(err, auth.ErrNoPassword):
		return "the credentials are not valid"
	case errors.Is(err, auth.ErrRateLimited):
		return "too many attempts; retry later"
	case errors.Is(err, auth.ErrDeviceLimit):
		return "the device limit is reached; revoke a device first"
	case errors.Is(err, auth.ErrNotFound):
		return "no such device"
	case errors.Is(err, auth.ErrWeakPassword):
		return "the password is too short"
	default:
		return err.Error()
	}
}

// pairResponse renders a pairing result. The token appears here and nowhere else.
func pairResponse(result auth.PairResult, info ServerInfo) gen.SrvPairResponse {
	return gen.SrvPairResponse{
		DeviceId:  gen.SrvDeviceId(result.Device.ID),
		Token:     result.Token,
		Scope:     gen.SrvScope(result.Device.Scope),
		ExpiresAt: gen.SrvTimestamp(wireTime(result.ExpiresAt)),
		Server:    serverIdentity(info),
	}
}

// refreshResponse renders a rotation.
func refreshResponse(result auth.PairResult) gen.SrvRefreshResponse {
	return gen.SrvRefreshResponse{
		Token:     result.Token,
		ExpiresAt: gen.SrvTimestamp(wireTime(result.ExpiresAt)),
	}
}

// deviceDTO renders one device for the admin list. Hashes never leave the store.
func deviceDTO(device auth.Device, current bool) gen.SrvDevice {
	dto := gen.SrvDevice{
		DeviceId:  gen.SrvDeviceId(device.ID),
		Name:      device.Name,
		Scope:     gen.SrvScope(device.Scope),
		CreatedAt: gen.SrvTimestamp(wireTime(device.CreatedAt)),
		ExpiresAt: gen.SrvTimestamp(wireTime(device.ExpiresAt)),
		Current:   boolPointer(current),
	}
	if device.Platform != "" {
		dto.Platform = stringPointer(device.Platform)
	}
	if !device.LastSeenAt.IsZero() {
		seen := gen.SrvTimestamp(wireTime(device.LastSeenAt))
		dto.LastSeenAt = &seen
	}
	return dto
}

// serverIdentity renders what a pairing client learns about this server.
func serverIdentity(info ServerInfo) gen.SrvServerIdentity {
	features := info.Features
	if features == nil {
		features = []string{}
	}
	limits := gen.SrvServerIdentityLimits(info.Limits)
	if limits == nil {
		limits = gen.SrvServerIdentityLimits{}
	}
	identity := gen.SrvServerIdentity{
		Version:   info.Version,
		PiVersion: info.PiVersion,
		Protocol:  gen.SrvProtocolVersion(Protocol),
		Features:  features,
		Limits:    limits,
	}
	if info.TLS != nil && info.TLS.FingerprintSHA256 != "" {
		// A pairing client gets the fingerprint here, so it can confirm the certificate
		// before it starts trusting it (the mockup's screen 4).
		notAfter := gen.SrvTimestamp(info.TLS.NotAfter)
		subject := info.TLS.Subject
		identity.Tls = &gen.SrvTlsInfo{
			FingerprintSha256: info.TLS.FingerprintSHA256,
			NotAfter:          &notAfter,
			Subject:           &subject,
		}
	}
	return identity
}

// wireTime formats a timestamp the way every DTO expects it.
func wireTime(t time.Time) string {
	return t.UTC().Format(timestampLayout)
}

// clientIP is the peer address of a request without its port: the bucket key of the
// pairing limiter. It reads the transport, never a forwarded header, so a client
// cannot choose its own bucket.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func boolPointer(value bool) *bool { return &value }

func stringPointer(value string) *string { return &value }
