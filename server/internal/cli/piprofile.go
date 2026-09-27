package cli

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// piProbeTimeout bounds `pi --version`: a CLI that does not answer is a fact to report, not
// something to wait for.
const piProbeTimeout = 10 * time.Second

// versionPattern reads a semantic version out of whatever a CLI prints. `pi --version`
// answers `0.87.1` today; a future one that prefixes it with the package name still parses.
var versionPattern = regexp.MustCompile(`\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?`)

// probePiVersion asks the pi binary what it is.
//
// Every session runs this binary, so what it reports is what the server is actually driving;
// `GET /server` carries it and the update panel compares it with what is published. A
// failure is not fatal — a session that cannot start will say so — but it must be visible,
// which is why the caller logs it and reports an empty version rather than a guess.
func probePiVersion(ctx context.Context, command []string) (string, error) {
	return probePiVersionWithin(ctx, command, piProbeTimeout)
}

// probePiVersionWithin is the probe with an explicit budget, so a test does not have to wait
// ten seconds to see what happens when a CLI stops answering.
func probePiVersionWithin(ctx context.Context, command []string, timeout time.Duration) (string, error) {
	if len(command) == 0 {
		return "", fmt.Errorf("no pi command is configured")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	probe := exec.CommandContext(ctx, command[0], "--version")
	// A CLI that spawns something which survives it (`sh -c 'sleep 5; …'`) keeps the output
	// pipe open, and `Wait` would then wait for the orphan rather than for the command.
	// `WaitDelay` bounds that wait, so the probe reports a timeout instead of hanging on a
	// process it never started. It is a knob, not a signal, so this stays portable.
	probe.WaitDelay = timeout
	output, err := probe.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s --version did not answer in %s", command[0], timeout)
		}
		return "", fmt.Errorf("%s --version: %w", command[0], err)
	}
	version := parsePiVersion(string(output))
	if version == "" {
		return "", fmt.Errorf("%s --version printed no version", command[0])
	}
	return version, nil
}

// parsePiVersion reads the version out of a `--version` answer.
func parsePiVersion(output string) string {
	return versionPattern.FindString(strings.TrimSpace(output))
}

// versionMismatch describes a difference between the version a deployment was built for and
// the one it is running.
//
// It is a warning and not a failure on purpose (AGENTS.md: a version mismatch degrades with
// a clear message instead of failing mid-run): pi's RPC surface is what this server speaks,
// and a newer patch release usually still speaks it. The message names both versions, so
// whoever reads the log knows which one to change.
func versionMismatch(expected, actual string) string {
	switch {
	case expected == "" || actual == "" || expected == actual:
		return ""
	default:
		return fmt.Sprintf(
			"pi %s is running but this server was built for %s; if a command misbehaves, align them",
			actual, expected,
		)
	}
}
