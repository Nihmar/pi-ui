// Package cli is the single registration point for the pi-ui command line
// (docs/spike-interfaces.md §5.7).
//
// main() only wires the process context and calls Run; every subcommand is a
// file under cmd/pi-ui that registers itself from an init():
//
//	func init() {
//		cli.Register(cli.Command{
//			Name:    "version",
//			Summary: "print the version and exit",
//			Run:     runVersion,
//		})
//	}
//
// A new subcommand therefore lands as a new file plus one registration, never
// as a new branch in main or in Run. Commands stay testable by construction:
// Run receives the context, the arguments after the command name and the
// output writers, so a test can call it with buffers and never talks to the
// real stdout/stderr.
//
// Exit codes (returned by Run, see cli.Run):
//
//	0    success, or usage requested with no arguments / -h / --help / help
//	1    the command ran and returned an error (printed to stderr)
//	2    the command line was wrong: unknown command (printed to stderr)
//	130  the command was interrupted (a wrapped context.Canceled, e.g. SIGINT)
//
// Register panics on programmer errors (empty name or summary, nil Run,
// duplicate name): registration happens before main runs, so failing fast at
// start-up is the only behaviour that cannot silently ship a broken CLI.
package cli
