package main

import (
	"context"
	"fmt"
	"io"
	"runtime"

	"github.com/Nihmar/pi-ui/server/internal/cli"
)

// Build stamps, overridable at link time:
//
//	go build -ldflags "-X main.version=0.1.0 -X main.commit=$(git rev-parse --short HEAD)"
var (
	version = "0.0.1-spike"
	commit  = "none"
)

func init() {
	cli.Register(cli.Command{
		Name:    "version",
		Summary: "print the version and exit",
		Run:     runVersion,
	})
}

func runVersion(_ context.Context, _ []string, stdout, _ io.Writer) error {
	_, err := fmt.Fprintf(stdout, "pi-ui %s (%s, %s)\n", version, commit, runtime.Version())
	return err
}
