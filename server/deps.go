//go:build tools

// This file keeps the spike's runtime dependencies pinned so every
// workstream stays on the same revision: internal/ws, internal/api and
// internal/rpc now exist and import coder/websocket and
// santhosh-tekuri/jsonschema/v6 directly (docs/spike-interfaces.md §4), so the
// blank imports below are no longer the only reference to them — they are kept
// as an explicit, single-place version anchor that keeps `go mod tidy` stable.
package tools

import (
	// WebSocket transport for /ws/v1.
	_ "github.com/coder/websocket"

	// JSON Schema (draft 2020-12) validation of inbound REST/WS payloads.
	_ "github.com/santhosh-tekuri/jsonschema/v6"
)
