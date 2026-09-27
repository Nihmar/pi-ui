package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	pitui "github.com/Nihmar/pi-ui/server/internal/tls"
)

// tlsCommand prints the fingerprint of a certificate.
//
// It exists so a user has something to compare by eye: the app shows a fingerprint during
// pairing, and this is the value the server actually serves. A self-signed certificate
// cannot be verified by the system store, so the fingerprint is the only part of it a
// person can check — the command makes that check a copy and a look.
func init() {
	Register(Command{
		Name:    "tls",
		Summary: "print the fingerprint of the certificate the server serves",
		Run:     runTLS,
	})
}

func runTLS(_ context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return Usagef(usageTLS)
	}
	switch args[0] {
	case "fingerprint":
		return runTLSFingerprint(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usageTLS)
		return nil
	default:
		return Usagef("unknown tls subcommand %q\n\n%s", args[0], usageTLS)
	}
}

const usageTLS = "usage: pi-ui tls fingerprint --cert <cert.pem>\n" +
	"       pi-ui tls fingerprint --url https://host:port\n\n" +
	"Prints the SHA-256 of the certificate as lowercase hex, optionally grouped by\n" +
	"colons — the value the app shows when it asks to pin a server.\n"

func runTLSFingerprint(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("tls fingerprint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cert := fs.String("cert", "", "path of the certificate the server serves")
	url := fs.String("url", "", "server URL: the certificate is fetched from it")
	colon := fs.Bool("colon", true, "group the fingerprint by colons, as a user compares it")
	if err := fs.Parse(args); err != nil {
		return Usage(err)
	}
	if fs.NArg() > 0 {
		return Usagef("unexpected argument %q", fs.Arg(0))
	}

	var info pitui.Info
	var err error
	switch {
	case *cert != "" && *url != "":
		return Usagef("--cert and --url are alternatives")
	case *cert != "":
		info, err = pitui.FingerprintFile(*cert)
	case *url != "":
		info, err = pitui.FingerprintURL(*url)
	default:
		return Usagef(usageTLS)
	}
	if err != nil {
		return err
	}

	fingerprint := info.FingerprintSHA256
	if *colon {
		fingerprint = pitui.Grouped(fingerprint)
	}
	fmt.Fprintf(stdout, "%s\n", fingerprint)
	fmt.Fprintf(stdout, "subject: %s\n", info.Subject)
	fmt.Fprintf(stdout, "expires: %s\n", info.NotAfter.Format("2006-01-02"))
	return nil
}
