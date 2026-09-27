// Command pi-ui is the spike's server entry point: it wires the process
// context and hands the command line to internal/cli.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/Nihmar/pi-ui/server/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer stop()

	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
