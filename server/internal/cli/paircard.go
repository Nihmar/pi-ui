package cli

import (
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/skip2/go-qrcode"

	"github.com/Nihmar/pi-ui/server/internal/auth"
	pitui "github.com/Nihmar/pi-ui/server/internal/tls"
)

// pairingLink is the deep link the app parses (docs/api-v1.md, "Pairing flow"):
// `piui://pair?v=1&url=<origin>&code=<CODE>[&fp=<sha256>]`. The fingerprint travels only
// when the server terminates TLS, so the app can trust the certificate the link was minted
// against instead of asking about it.
func pairingLink(origin, fingerprint string, invite auth.Invite) string {
	query := url.Values{}
	query.Set("v", "1")
	if origin != "" {
		query.Set("url", origin)
	}
	query.Set("code", invite.Code)
	if fingerprint != "" {
		query.Set("fp", fingerprint)
	}
	return "piui://pair?" + query.Encode()
}

// renderPairCard prints one invitation: the expiry, the code a user can type, the origins,
// the link and a QR a phone can scan. `pi-ui pair` writes it to stdout, `serve` writes the
// bootstrap one to stderr, and both show the same thing.
//
// The first origin is the one the link and the QR carry; the others are alternatives the
// same server answers on (a second interface, a VPN address).
func renderPairCard(w io.Writer, invite auth.Invite, origins []string, fingerprint string) error {
	origin := ""
	if len(origins) > 0 {
		origin = origins[0]
	}
	link := pairingLink(origin, fingerprint, invite)
	fmt.Fprintf(w, "pairing invitation (expires %s, in %s)\n",
		invite.ExpiresAt.UTC().Format(time.RFC3339), time.Until(invite.ExpiresAt).Round(time.Second))
	fmt.Fprintf(w, "  code    %s\n", invite.Code)
	if origin != "" {
		fmt.Fprintf(w, "  origin  %s\n", origin)
		for _, extra := range origins[1:] {
			fmt.Fprintf(w, "  also    %s\n", extra)
		}
	}
	if fingerprint != "" {
		fmt.Fprintf(w, "  tls     sha256 %s\n", pitui.Grouped(fingerprint))
	}
	fmt.Fprintf(w, "  link    %s\n", link)
	if origin == "" {
		fmt.Fprintln(w, "  note    no network address was detected; pass --url <origin> so the link carries one")
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
