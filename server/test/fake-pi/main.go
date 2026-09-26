package main

import (
	"bufio"
	"fmt"
	"os"
	"time"
)

// Exit codes of the harness (see doc.go).
const (
	exitOK    = 0
	exitIO    = 1
	exitUsage = 2
	exitCrash = 9
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake-pi: %v\n", err)
		usage(os.Stderr)
		return exitUsage
	}
	script, err := loadScript(cfg.scriptPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake-pi: %v\n", err)
		return exitUsage
	}

	em := newEmitter(os.Stdout, cfg)
	startFaultTimers(cfg, script, em)

	// Diagnostics go to stderr before the first record, never into the protocol stream.
	for i := 1; i <= cfg.stderrLines; i++ {
		fmt.Fprintf(os.Stderr, "fake-pi: stderr line %d\n", i)
	}
	if err := em.emitStartup(script.Startup); err != nil {
		fmt.Fprintf(os.Stderr, "fake-pi: %v\n", err)
		return exitIO
	}

	if cfg.ignoreStdin {
		// Stay alive without touching stdin: only --exit-after/--crash-after (or the
		// parent killing this process) ends the run.
		for {
			time.Sleep(time.Hour)
		}
	}
	if err := serve(bufio.NewReader(os.Stdin), em, script, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "fake-pi: %v\n", err)
		return exitIO
	}
	return exitOK
}
