package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// Exit codes returned by Run.
const (
	ExitOK        = 0   // command succeeded, or usage was requested
	ExitError     = 1   // the command ran and failed
	ExitUsage     = 2   // the command line was wrong
	ExitInterrupt = 130 // the command was interrupted (context.Canceled / SIGINT)
)

const usage = "pi-ui is the server companion for the pi coding agent.\n" +
	"\n" +
	"usage:\n" +
	"  pi-ui <command> [flags] [arguments]\n" +
	"  pi-ui help [command]\n"

// Run executes one pi-ui invocation. args are the arguments that follow the
// program name; every write goes to stdout/stderr and the process exit code is
// returned instead of calling os.Exit, so a test can drive the CLI with
// buffers. Run never touches os.Stdout/os.Stderr.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		writeUsage(stdout)
		return ExitOK
	}

	switch args[0] {
	case "-h", "--help", "help":
		return runHelp(args[1:], stdout, stderr)
	}

	name := args[0]
	cmd, ok := lookup(name)
	if !ok {
		fmt.Fprintf(stderr, "pi-ui: unknown command %q\n", name)
		fmt.Fprintln(stderr, "Run 'pi-ui help' for usage.")
		return ExitUsage
	}

	if err := cmd.Run(ctx, args[1:], stdout, stderr); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintf(stderr, "pi-ui %s: interrupted\n", name)
			return ExitInterrupt
		}
		fmt.Fprintf(stderr, "pi-ui %s: %v\n", name, err)
		return ExitError
	}
	return ExitOK
}

// runHelp renders usage (no arguments) or one command's name and summary.
func runHelp(args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0:
		writeUsage(stdout)
		return ExitOK
	case len(args) > 1:
		fmt.Fprintln(stderr, "pi-ui help: expected at most one command name")
		return ExitUsage
	}

	cmd, ok := lookup(args[0])
	if !ok {
		fmt.Fprintf(stderr, "pi-ui help: unknown command %q\n", args[0])
		return ExitUsage
	}
	fmt.Fprintf(stdout, "%s  %s\n", cmd.Name, cmd.Summary)
	return ExitOK
}

// writeUsage lists the registered commands, so a new command appears in the
// help text as soon as it registers itself.
func writeUsage(w io.Writer) {
	fmt.Fprint(w, usage)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "commands:")

	names := append([]string{"help"}, commandNames()...)
	width := 0
	for _, name := range names {
		if len(name) > width {
			width = len(name)
		}
	}
	for _, name := range names {
		summary := "show this help"
		if cmd, ok := lookup(name); ok {
			summary = cmd.Summary
		}
		fmt.Fprintf(w, "  %-*s  %s\n", width, name, summary)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run 'pi-ui help <command>' for a command's summary.")
}
