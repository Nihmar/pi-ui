package cli

import (
	"net/http"

	"github.com/Nihmar/pi-ui/server/internal/api"
	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// restToWS adapts the REST authenticator to the WebSocket Authorizer seam, so both
// transports make the same decision about the same request: one credential, one scope,
// one loopback rule.
//
// The adapter exists because the two packages cannot import each other (api imports ws
// for the hub seam), and because the device-aware decision lives on the REST
// authenticator: the optional AuthenticateDevice method is used when present.
type restToWS struct {
	auth api.Authenticator
}

// Authorize implements ws.Authorizer.
func (a restToWS) Authorize(r *http.Request) (ws.Scope, string, error) {
	type deviceAuthenticator interface {
		AuthenticateDevice(r *http.Request) (api.Scope, string, error)
	}
	if extended, ok := a.auth.(deviceAuthenticator); ok {
		scope, deviceID, err := extended.AuthenticateDevice(r)
		if err != nil {
			return "", "", err
		}
		return ws.Scope(scope), deviceID, nil
	}
	scope, err := a.auth.Authenticate(r)
	if err != nil {
		return "", "", err
	}
	return ws.Scope(scope), "", nil
}
