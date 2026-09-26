package api

import (
	"net/http"

	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/mcp"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// The MCP surface (docs/api-v1.md, "MCP"): admin only, because the configuration names
// programs the children will run and endpoints they will talk to.
//
// The document is stored, not spoken: pi itself never sees it, the bridge inside the
// child reads it through PI_UI_BRIDGE_CONFIG and connects to the servers. What matters
// here is that a stored secret never comes back out (values are redacted, the sentinel
// keeps them) and that a change lands in the trail as mcp.update.

// mcpView is what a client receives: the file, the entries with every secret redacted,
// and which servers the bridge would start.
type mcpView struct {
	Path    string                `json:"path,omitempty"`
	Version int                   `json:"version,omitempty"`
	Servers map[string]mcp.Server `json:"servers"`
	Enabled []string              `json:"enabled"`
}

// getMcp answers GET /api/v1/mcp.
func (a *api) getMcp(w http.ResponseWriter, _ *http.Request) {
	service, ok := a.mcpOrError(w)
	if !ok {
		return
	}
	document, err := service.Read()
	if err != nil {
		code := codeOr(err, sessions.CodeInternal)
		writeError(w, statusFor(code), code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, viewOf(service, document))
}

// putMcpBody is the PUT /api/v1/mcp payload: the same shape a reader gets back.
type putMcpBody struct {
	Version int                   `json:"version,omitempty"`
	Servers map[string]mcp.Server `json:"servers"`
}

// putMcp answers PUT /api/v1/mcp.
func (a *api) putMcp(w http.ResponseWriter, r *http.Request) {
	service, ok := a.mcpOrError(w)
	if !ok {
		return
	}
	var body putMcpBody
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, codeOr(err, sessions.CodeBadRequest), err.Error())
		return
	}
	document, err := service.Write(mcp.Document{Version: body.Version, Servers: body.Servers})
	if err != nil {
		code := codeOr(err, sessions.CodeBadRequest)
		event := a.auditEvent(r, mcp.AuditAction, audit.OutcomeDenied)
		event.Details = map[string]any{"reason": err.Error()}
		a.record(event)
		writeError(w, statusFor(code), code, err.Error())
		return
	}
	event := a.auditEvent(r, mcp.AuditAction, audit.OutcomeOK)
	event.Target = service.Path()
	event.Details = map[string]any{"servers": len(document.Servers)}
	a.record(event)
	writeJSON(w, http.StatusOK, viewOf(service, document))
}

// viewOf renders a document for a client.
func viewOf(service *mcp.Service, document mcp.Document) mcpView {
	enabled := document.EnabledNames()
	if enabled == nil {
		enabled = []string{}
	}
	servers := document.Servers
	if servers == nil {
		servers = map[string]mcp.Server{}
	}
	return mcpView{
		Path:    service.Path(),
		Version: document.Version,
		Servers: servers,
		Enabled: enabled,
	}
}

// mcpOrError reports a server without an MCP configuration file.
func (a *api) mcpOrError(w http.ResponseWriter) (*mcp.Service, bool) {
	if a.mcp == nil || !a.mcp.Configured() {
		writeError(w, http.StatusNotImplemented, sessions.CodeUnsupported,
			"no MCP configuration file is configured on this server")
		return nil, false
	}
	return a.mcp, true
}
