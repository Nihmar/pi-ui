package main

import (
	"context"
	"io"

	"github.com/Nihmar/pi-ui/server/internal/cli"
)

// init publishes the link-time stamps to internal/cli and registers the command. -ldflags
// stamps package main (-X main.version=...), and main.go stays the process entry point
// (docs/spike-interfaces.md §5.7).
func init() {
	cli.Version, cli.Commit = version, commit
	cli.Register(cli.Command{
		Name:    "serve",
		Summary: "run the HTTP + WebSocket server for pi sessions",
		Run:     runServe,
	})
}

// runServe keeps this file to a registration: the wiring (and its tests) live in
// internal/cli/serve.go.
func runServe(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return cli.Serve(ctx, args, stdout, stderr)
}
