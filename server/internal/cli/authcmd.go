package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// init registers the auth command group.
func init() {
	Register(Command{
		Name:    "auth",
		Summary: "manage the admin password and device access (auth set-password)",
		Run:     runAuth,
	})
}

// runAuth dispatches the auth subcommands. The CLI is a flat command registry, so a
// group (auth set-password) is one registered command with its own arguments.
func runAuth(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		writeAuthUsage(stderr)
		return Usagef("missing subcommand")
	}
	switch args[0] {
	case "-h", "--help", "help":
		writeAuthUsage(stdout)
		return nil
	case "set-password":
		return runAuthSetPassword(ctx, args[1:], stdout, stderr)
	default:
		writeAuthUsage(stderr)
		return Usagef("unknown subcommand %q", args[0])
	}
}

// runAuthSetPassword is the Run function of `pi-ui auth set-password`: it stores the
// argon2id verifier of the admin password, which is the second way into a server
// (admin scope) and the recovery path when no device can pair.
func runAuthSetPassword(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("auth set-password", flag.ContinueOnError)
	fs.SetOutput(stderr)
	password := fs.String("password", "", "the new password (else PIUI_ADMIN_PASSWORD, else a prompt)")
	stateDirFlag := fs.String("state-dir", "", "state directory (PIUI_STATE_DIR)")
	if err := fs.Parse(args); err != nil {
		return Usage(err)
	}
	if fs.NArg() > 0 {
		return Usagef("unexpected argument %q", fs.Arg(0))
	}

	secret := strings.TrimSpace(*password)
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv(envAdminPassword))
	}
	if secret == "" {
		prompted, err := promptPassword(stderr)
		if err != nil {
			return err
		}
		secret = prompted
	}

	dir, err := resolveStateDir(*stateDirFlag)
	if err != nil {
		return err
	}
	db, service, err := openState(ctx, dir)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := service.SetAdminPassword(secret); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "admin password set in %s\n", db.Path())
	return nil
}

// promptPassword asks twice on the terminal, with the echo off. A non-interactive
// stdin (a pipe, a container without a TTY) gets a clear instruction instead of a
// password read from whatever happens to be on stdin.
func promptPassword(stderr io.Writer) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("stdin is not a terminal: pass --password or set %s", envAdminPassword)
	}
	fmt.Fprint(stderr, "New admin password: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(stderr)
	if err != nil {
		return "", fmt.Errorf("reading the password: %w", err)
	}
	fmt.Fprint(stderr, "Repeat the password: ")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(stderr)
	if err != nil {
		return "", fmt.Errorf("reading the password: %w", err)
	}
	if string(first) != string(second) {
		return "", Usagef("the passwords do not match")
	}
	return string(first), nil
}

// writeAuthUsage documents the group.
func writeAuthUsage(w io.Writer) {
	fmt.Fprint(w, "usage: pi-ui auth <subcommand>\n\n"+
		"subcommands:\n"+
		"  set-password [--password <value>] [--state-dir <path>]\n"+
		"      store the argon2id verifier of the admin password (PIUI_ADMIN_PASSWORD,\n"+
		"      or an interactive prompt when neither is given)\n\n"+
		"The admin password mints an admin-scoped device at pairing time and recovers an\n"+
		"installation whose devices were all revoked.\n")
}
