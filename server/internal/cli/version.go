package cli

// Build stamps reported by GET /api/v1/server and written to the start-up log line. cmd/pi-ui
// copies its own link-time values here from an init(), because -ldflags stamps package main
// and this package must not depend on it (main.go stays a two-line entry point).
var (
	Version = "0.0.1-spike"
	Commit  = "none"
)
