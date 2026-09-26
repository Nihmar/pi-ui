package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/skip2/go-qrcode"

	"github.com/Nihmar/pi-ui/server/internal/auth"
)

// init registers the pairing command, so cmd/pi-ui and the CLI tests see it.
func init() {
	Register(Command{
		Name:    "pair",
		Summary: "mint a one-time pairing invitation (code, deep link, QR)",
		Run:     runPair,
	})
}

// runPair is the Run function of `pi-ui pair`: it mints one invitation in the state
// database and prints it. A running `pi-ui serve` over the same state directory
// consumes it, which is why the invitation lives in SQLite and not in this process.
func runPair(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writePairUsage(stderr) }

	kind := fs.String("kind", string(auth.InviteQR), "invitation kind: qr (code+secret) or typed (code only)")
	origin := fs.String("url", "", "server origin the app should use (e.g. http://pi-ui.local:8787)")
	stateDirFlag := fs.String("state-dir", "", "state directory (PIUI_STATE_DIR)")
	if err := fs.Parse(args); err != nil {
		return Usage(err)
	}
	if fs.NArg() > 0 {
		return Usagef("unexpected argument %q", fs.Arg(0))
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

	invite, err := service.NewInvite(auth.InviteKind(*kind))
	if err != nil {
		return err
	}
	return renderInvite(stdout, invite, *origin)
}

// pairingLink is the deep link the app parses (docs/api-v1.md, "Pairing flow").
func pairingLink(origin string, invite auth.Invite) string {
	query := url.Values{}
	query.Set("v", "1")
	if origin != "" {
		query.Set("url", origin)
	}
	query.Set("code", invite.Code)
	if invite.Secret != "" {
		query.Set("secret", invite.Secret)
	}
	return "piui://pair?" + query.Encode()
}

// renderInvite prints one invitation: the kind and expiry, the human code, the link a
// typed invitation carries and a QR a phone can scan.
//
// A QR invitation's secret is **never** printed: it travels inside the QR only, which
// is the point of the secret — someone reading the terminal over a shoulder sees the
// code but cannot pair with it. The typed kind has no secret and prints its full link.
func renderInvite(w io.Writer, invite auth.Invite, origin string) error {
	link := pairingLink(origin, invite)
	fmt.Fprintf(w, "pairing invitation (%s, expires %s)\n", invite.Kind, invite.ExpiresAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "  code    %s\n", invite.Code)
	if origin != "" {
		fmt.Fprintf(w, "  origin  %s\n", origin)
	}
	if invite.Secret != "" {
		fmt.Fprintln(w, "  link    inside the QR only (the secret is not printed);")
		fmt.Fprintln(w, "          use --kind typed to mint a hand-typable code instead")
	} else {
		fmt.Fprintf(w, "  link    %s\n", link)
	}
	if origin == "" {
		fmt.Fprintln(w, "  note: pass --url <origin> so the app knows where to connect")
	}
	fmt.Fprintln(w)
	code, err := qrcode.New(link, qrcode.Medium)
	if err != nil {
		fmt.Fprintln(w, "the QR could not be rendered; scan or type the code above")
		return nil
	}
	fmt.Fprint(w, code.ToSmallString(false))
	return nil
}

// writePairUsage prints the command's own flags.
func writePairUsage(w io.Writer) {
	fmt.Fprint(w, "usage: pi-ui pair [flags]\n\n"+
		"flags:\n"+
		"  --kind qr|typed        invitation kind (default qr: the QR carries a secret)\n"+
		"  --url <origin>         origin the app should connect to, e.g. http://pi-ui.local:8787\n"+
		"  --state-dir <path>     state directory (PIUI_STATE_DIR, default $XDG_STATE_HOME/pi-ui)\n\n"+
		"The invitation is single-use and expires after ten minutes; a running `pi-ui serve`\n"+
		"over the same state directory accepts it.\n")
}
