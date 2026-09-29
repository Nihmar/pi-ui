package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"

	pitui "github.com/Nihmar/pi-ui/server/internal/tls"
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
// database and prints the pairing card. A running `pi-ui serve` over the same state
// directory consumes it, which is why the invitation lives in SQLite and not in this
// process.
func runPair(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writePairUsage(stderr) }

	origin := fs.String("url", "", "origin the app should connect to (default: the host's LAN addresses)")
	fs.String("addr", "", "address the server binds, for the port (default "+defaultAddr+"; PIUI_ADDR)")
	tlsCert := fs.String("tls-cert", "", "certificate the server serves; its fingerprint goes into the link")
	stateDirFlag := fs.String("state-dir", "", "state directory (PIUI_STATE_DIR)")
	if err := fs.Parse(args); err != nil {
		return Usage(err)
	}
	if fs.NArg() > 0 {
		return Usagef("unexpected argument %q", fs.Arg(0))
	}

	scheme, fingerprint := "http", ""
	if *tlsCert != "" {
		info, err := pitui.FingerprintFile(*tlsCert)
		if err != nil {
			return err
		}
		scheme, fingerprint = "https", info.FingerprintSHA256
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

	invite, err := service.NewInvite()
	if err != nil {
		return err
	}
	bind := resolve(fs, "addr", envAddr, "")
	return renderPairCard(stdout, invite, pairOrigins(*origin, bind, scheme, systemAddrs()), fingerprint)
}

// pairOrigins decides what the card advertises. An explicit --url wins; an explicit --addr
// describes where the server binds (so a bound LAN address becomes the origin); neither
// means the operator did not say, and the card lists the host's LAN addresses on the default
// port — the case a phone needs.
func pairOrigins(origin, bindAddr, scheme string, addrs []net.Addr) []string {
	if origin != "" {
		return []string{origin}
	}
	if bindAddr == "" {
		bindAddr = "0.0.0.0:" + fmt.Sprint(defaultListenPort)
	}
	origins, _ := advertiseOrigins(bindAddr, addrPort(bindAddr, defaultListenPort), scheme, addrs)
	return origins
}

// writePairUsage prints the command's own flags.
func writePairUsage(w io.Writer) {
	fmt.Fprint(w, "usage: pi-ui pair [flags]\n\n"+
		"flags:\n"+
		"  --url <origin>         origin the app should connect to (default: the host's LAN addresses)\n"+
		"  --addr <host:port>     address the server binds, for the port (default "+defaultAddr+", PIUI_ADDR)\n"+
		"  --tls-cert <path>      certificate the server serves; its fingerprint goes into the link\n"+
		"  --state-dir <path>     state directory (PIUI_STATE_DIR, default $XDG_STATE_HOME/pi-ui)\n\n"+
		"The invitation is single-use and expires after ten minutes; a running `pi-ui serve`\n"+
		"over the same state directory consumes it. The card carries a code a user can type,\n"+
		"a piui://pair link and a QR a phone can scan.\n")
}
