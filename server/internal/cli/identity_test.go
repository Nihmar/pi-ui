package cli

import (
	"bytes"
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Nihmar/pi-ui/server/internal/auth"
)

// runCLI runs one command through the real entry point and returns the exit code and
// both streams.
func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// stateService opens the state directory the way the commands do, for assertions.
func stateService(t *testing.T, dir string) (*auth.Service, func()) {
	t.Helper()
	db, service, err := openState(context.Background(), dir)
	if err != nil {
		t.Fatalf("openState(%s): %v", dir, err)
	}
	return service, func() { db.Close() }
}

// inviteCodePattern reads the six-character Crockford base32 code off the pairing
// card (`  code    4K9M27`).
var inviteCodePattern = regexp.MustCompile(`(?m)^  code    ([0-9A-HJKMNP-TV-Z]{6})$`)

func TestPairMintsAnInvitationInTheSharedStateDatabase(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := runCLI(t, "pair", "--state-dir", dir, "--url", "http://pi-ui.local:8787")
	if code != ExitOK {
		t.Fatalf("pair = %d, want %d (stderr %s)", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout, "piui://pair?") || !strings.Contains(stdout, "url=http%3A%2F%2Fpi-ui.local%3A8787") {
		t.Fatalf("stdout does not carry the deep link:\n%s", stdout)
	}
	match := inviteCodePattern.FindStringSubmatch(stdout)
	if match == nil {
		t.Fatalf("stdout does not carry a six-character Crockford base32 code:\n%s", stdout)
	}

	// The invitation lives in the state database, so a server over the same directory
	// can consume it: that is the property `pi-ui pair` + `pi-ui serve` relies on.
	service, closeState := stateService(t, dir)
	defer closeState()
	if got := service.PendingInvites(); got != 1 {
		t.Fatalf("pending invitations = %d, want 1", got)
	}
	paired, err := service.Pair(auth.PairRequest{DeviceName: "phone", Code: match[1]}, "10.0.0.1")
	if err != nil {
		t.Fatalf("Pair with the minted code: %v", err)
	}
	if paired.Device.Scope != auth.ScopeOperator {
		t.Fatalf("scope = %q, want operator", paired.Device.Scope)
	}
}

func TestRenderPairCardPrintsTheCodeOriginsLinkAndQR(t *testing.T) {
	dir := t.TempDir()
	service, closeState := stateService(t, dir)
	defer closeState()
	invite, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}

	var out bytes.Buffer
	err = renderPairCard(&out, invite,
		[]string{"http://pi-ui.local:8787", "http://10.0.0.5:8787"}, "4f2a9c11")
	if err != nil {
		t.Fatalf("renderPairCard: %v", err)
	}
	rendered := out.String()
	for _, want := range []string{
		invite.Code,
		"  origin  http://pi-ui.local:8787",
		"  also    http://10.0.0.5:8787",
		"piui://pair?",
		"code=" + invite.Code,
		"fp=4f2a9c11",
		"█",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("the card misses %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "secret") {
		t.Fatalf("the card still mentions a secret:\n%s", rendered)
	}
}

func TestRenderPairCardWithoutAnOriginSaysSo(t *testing.T) {
	dir := t.TempDir()
	service, closeState := stateService(t, dir)
	defer closeState()
	invite, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}

	var out bytes.Buffer
	if err := renderPairCard(&out, invite, nil, ""); err != nil {
		t.Fatalf("renderPairCard: %v", err)
	}
	rendered := out.String()
	if !strings.Contains(rendered, "no network address was detected") {
		t.Fatalf("the card does not explain the missing origin:\n%s", rendered)
	}
	if strings.Contains(rendered, "fp=") || strings.Contains(rendered, "url=") {
		t.Fatalf("an origin-less card carries a fingerprint or a url:\n%s", rendered)
	}
}

func TestPairingLinkCarriesOnlyWhatItHas(t *testing.T) {
	invite := auth.Invite{Code: "4K9M27"}

	plain := pairingLink("http://pi-ui.local:8787", "", invite)
	if plain != "piui://pair?code=4K9M27&url=http%3A%2F%2Fpi-ui.local%3A8787&v=1" {
		t.Fatalf("pairingLink = %q, want the v/url/code shape", plain)
	}

	withFingerprint := pairingLink("", "4f2a9c11", invite)
	if !strings.Contains(withFingerprint, "fp=4f2a9c11") || strings.Contains(withFingerprint, "url=") {
		t.Fatalf("pairingLink without an origin = %q, want only the code and the fingerprint", withFingerprint)
	}
}

func TestAuthSetPasswordStoresAVerifierAndThePasswordPairs(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := runCLI(t, "auth", "set-password", "--state-dir", dir, "--password", "correct horse battery staple")
	if code != ExitOK {
		t.Fatalf("auth set-password = %d, want %d (stderr %s)", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout, "admin password set") {
		t.Fatalf("stdout = %q, want the confirmation", stdout)
	}

	service, closeState := stateService(t, dir)
	defer closeState()
	if !service.HasAdminPassword() {
		t.Fatal("the password verifier is not in the state database")
	}
	paired, err := service.Pair(auth.PairRequest{DeviceName: "admin shell", Password: "correct horse battery staple"}, "10.0.0.2")
	if err != nil {
		t.Fatalf("Pair with the stored password: %v", err)
	}
	if paired.Device.Scope != auth.ScopeAdmin {
		t.Fatalf("scope = %q, want admin", paired.Device.Scope)
	}
}

func TestAuthSetPasswordRejectsAShortOne(t *testing.T) {
	dir := t.TempDir()
	code, _, stderr := runCLI(t, "auth", "set-password", "--state-dir", dir, "--password", "short")
	if code != ExitError {
		t.Fatalf("short password = %d, want %d", code, ExitError)
	}
	if !strings.Contains(stderr, "too short") {
		t.Fatalf("stderr = %q, want the length reason", stderr)
	}
	service, closeState := stateService(t, dir)
	defer closeState()
	if service.HasAdminPassword() {
		t.Fatal("a rejected password was stored")
	}
}

func TestAuthSetPasswordWithoutAValueNeedsATerminal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envAdminPassword, "")
	code, _, stderr := runCLI(t, "auth", "set-password", "--state-dir", dir)
	if code != ExitError {
		t.Fatalf("non-interactive set-password = %d, want %d", code, ExitError)
	}
	if !strings.Contains(stderr, "stdin is not a terminal") {
		t.Fatalf("stderr = %q, want the terminal hint", stderr)
	}
}

func TestAuthRejectsAnUnknownSubcommand(t *testing.T) {
	code, _, stderr := runCLI(t, "auth", "device-list")
	if code != ExitUsage {
		t.Fatalf("unknown subcommand = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "unknown subcommand") {
		t.Fatalf("stderr = %q, want the unknown-subcommand message", stderr)
	}
}

func TestStatusReportsPasswordDevicesAndInvitations(t *testing.T) {
	dir := t.TempDir()

	code, stdout, stderr := runCLI(t, "status", "--state-dir", dir)
	if code != ExitOK {
		t.Fatalf("status = %d, want %d (stderr %s)", code, ExitOK, stderr)
	}
	for _, want := range []string{"admin password  not set", "devices         0", "invitations     0 pending", "pi-ui pair"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("empty status misses %q:\n%s", want, stdout)
		}
	}

	// Seed one device through the same store, then the report shows it.
	service, closeState := stateService(t, dir)
	invite, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	if _, err := service.Pair(auth.PairRequest{DeviceName: "Pixel 9", Platform: "android", Code: invite.Code}, "10.0.0.3"); err != nil {
		t.Fatalf("Pair: %v", err)
	}
	closeState()

	code, stdout, _ = runCLI(t, "status", "--state-dir", dir)
	if code != ExitOK {
		t.Fatalf("status = %d, want %d", code, ExitOK)
	}
	for _, want := range []string{"devices         1", "Pixel 9", "operator"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("status misses %q:\n%s", want, stdout)
		}
	}
}

func TestStateDirResolution(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envStateDir, dir)
	resolved, err := resolveStateDir("")
	if err != nil {
		t.Fatalf("resolveStateDir: %v", err)
	}
	if resolved != dir {
		t.Fatalf("resolved = %q, want %q", resolved, dir)
	}

	flagDir := t.TempDir()
	resolved, err = resolveStateDir(flagDir)
	if err != nil {
		t.Fatalf("resolveStateDir(flag): %v", err)
	}
	if resolved != flagDir {
		t.Fatalf("flag did not win: %q, want %q", resolved, flagDir)
	}

	if _, err := os.Stat(stateDatabase); err == nil {
		t.Fatal("a stray state.db appeared in the working directory")
	}
}
