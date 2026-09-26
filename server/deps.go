//go:build tools

// This file pins the spike's runtime dependencies before the packages that
// import them land: internal/ws, internal/api and internal/rpc arrive in their
// own commits (docs/spike-interfaces.md §4). Pinning the revisions here keeps
// `go mod tidy` stable and every workstream on the same version; the
// corresponding lines move to the importing packages once those exist.
package tools

import (
	// WebSocket transport for /ws/v1.
	_ "github.com/coder/websocket"

	// JSON Schema (draft 2020-12) validation of inbound REST/WS payloads.
	_ "github.com/santhosh-tekuri/jsonschema/v6"
)
