package auth

import (
	"errors"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// testParams is a cheap argon2id cost: production uses 64 MiB, the suite must stay
// fast.
func testParams() Argon2Params {
	return Argon2Params{Time: 1, Memory: 8, Threads: 1, KeyLen: 32}
}

// testClock is a manually advanced clock.
type testClock struct{ at time.Time }

func (c *testClock) now() time.Time          { return c.at }
func (c *testClock) advance(d time.Duration) { c.at = c.at.Add(d) }

// newTestService builds a service with deterministic clock and randomness.
func newTestService(t *testing.T, mutate func(*Options)) (*Service, *testClock) {
	t.Helper()
	clock := &testClock{at: time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)}
	opts := Options{
		Now:    clock.now,
		Rand:   rand.New(rand.NewSource(1)),
		Argon2: testParams(),
	}
	if mutate != nil {
		mutate(&opts)
	}
	return New(NewMemoryStore(), opts), clock
}

func TestPairWithAValidCodeMintsAnOperatorDevice(t *testing.T) {
	service, clock := newTestService(t, nil)
	invite, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	if len(invite.Code) != inviteCodeLength {
		t.Fatalf("invite = %+v, want a %d character code", invite, inviteCodeLength)
	}
	if want := clock.at.Add(defaultPairingTTL); !invite.ExpiresAt.Equal(want) {
		t.Fatalf("invite expiry = %s, want %s", invite.ExpiresAt, want)
	}

	result, err := service.Pair(PairRequest{
		DeviceName: "Pixel 9",
		Platform:   "android",
		Code:       invite.Code,
	}, "10.0.0.1")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if result.Device.Scope != ScopeOperator {
		t.Fatalf("scope = %q, want %q", result.Device.Scope, ScopeOperator)
	}
	if !strings.HasPrefix(result.Token, result.Device.ID+".") {
		t.Fatalf("token %q does not start with the device id %q", result.Token, result.Device.ID)
	}
	if got := len(service.Devices()); got != 1 {
		t.Fatalf("devices = %d, want 1", got)
	}
	if service.PendingInvites() != 0 {
		t.Fatalf("the invitation was not consumed")
	}

	// Single use: the same code cannot pair a second device.
	if _, err := service.Pair(PairRequest{DeviceName: "again", Code: invite.Code}, "10.0.0.1"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("reusing the invitation = %v, want ErrUnauthorized", err)
	}
}

func TestPairNormalizesTheTypedCode(t *testing.T) {
	service, _ := newTestService(t, nil)
	invite, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	// A lowercase, separator-laden spelling of the same code pairs: normalization is
	// applied before the comparison.
	lower := strings.ToLower(invite.Code)
	spelled := lower[:2] + "-" + lower[2:4] + " " + lower[4:]
	if _, err := service.Pair(PairRequest{DeviceName: "typed", Code: spelled}, "10.0.0.2"); err != nil {
		t.Fatalf("normalized Pair (%q): %v", spelled, err)
	}
}

func TestPairRejectsAWrongCodeWithoutBurningTheInvite(t *testing.T) {
	service, _ := newTestService(t, nil)
	invite, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	wrong := "000000"
	if wrong == invite.Code {
		wrong = "111111"
	}
	if _, err := service.Pair(PairRequest{DeviceName: "x", Code: wrong}, "10.0.0.3"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong code = %v, want ErrUnauthorized", err)
	}
	if service.PendingInvites() != 1 {
		t.Fatalf("a wrong code consumed the invitation")
	}
	// The legitimate attempt still works.
	if _, err := service.Pair(PairRequest{DeviceName: "x", Code: invite.Code}, "10.0.0.3"); err != nil {
		t.Fatalf("Pair after a failed attempt: %v", err)
	}
}

func TestPairRejectsAnExpiredInvite(t *testing.T) {
	service, clock := newTestService(t, nil)
	invite, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	clock.advance(defaultPairingTTL + time.Second)
	if _, err := service.Pair(PairRequest{DeviceName: "late", Code: invite.Code}, "10.0.0.4"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired invite = %v, want ErrUnauthorized", err)
	}
}

func TestPairWithPasswordMintsAnAdminDevice(t *testing.T) {
	service, _ := newTestService(t, nil)
	if err := service.SetAdminPassword("correct horse battery staple"); err != nil {
		t.Fatalf("SetAdminPassword: %v", err)
	}
	if !service.HasAdminPassword() {
		t.Fatal("HasAdminPassword = false after setting one")
	}

	result, err := service.Pair(PairRequest{DeviceName: "admin shell", Password: "correct horse battery staple"}, "10.0.0.5")
	if err != nil {
		t.Fatalf("Pair with password: %v", err)
	}
	if result.Device.Scope != ScopeAdmin {
		t.Fatalf("scope = %q, want %q", result.Device.Scope, ScopeAdmin)
	}

	if _, err := service.Pair(PairRequest{DeviceName: "x", Password: "wrong"}, "10.0.0.5"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong password = %v, want ErrUnauthorized", err)
	}
}

func TestPairWithPasswordWithoutOneSetReportsNoPassword(t *testing.T) {
	service, _ := newTestService(t, nil)
	_, err := service.Pair(PairRequest{DeviceName: "x", Password: "guess"}, "10.0.0.6")
	if !errors.Is(err, ErrUnauthorized) || !errors.Is(err, ErrNoPassword) {
		t.Fatalf("Pair without a configured password = %v, want unauthorized+no password", err)
	}
}

func TestPairRateLimitBoundsAttemptsPerCaller(t *testing.T) {
	service, clock := newTestService(t, func(o *Options) {
		o.PairLimit = 3
		o.PairWindow = time.Minute
	})
	fail := func(caller string) error {
		_, err := service.Pair(PairRequest{DeviceName: "x", Code: "000000"}, caller)
		return err
	}
	for i := 0; i < 3; i++ {
		if err := fail("10.0.0.7"); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("attempt %d = %v, want ErrUnauthorized", i, err)
		}
	}
	err := fail("10.0.0.7")
	var limited *RateLimitError
	if !errors.As(err, &limited) {
		t.Fatalf("fourth attempt = %v, want a RateLimitError", err)
	}
	if limited.RetryAfter <= 0 {
		t.Fatalf("RetryAfter = %s, want > 0", limited.RetryAfter)
	}
	// Another caller is unaffected, and the window frees the first one.
	if _, err := service.Pair(PairRequest{DeviceName: "x", Code: "000000"}, "10.0.0.8"); errors.Is(err, ErrRateLimited) {
		t.Fatalf("a different caller was rate limited: %v", err)
	}
	clock.advance(time.Minute + time.Second)
	if err := fail("10.0.0.7"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("after the window = %v, want a normal unauthorized attempt", err)
	}
}

func TestAuthenticateTokenAndSlidingExpiry(t *testing.T) {
	service, clock := newTestService(t, func(o *Options) {
		o.SlidingRefresh = time.Hour
		o.TokenTTL = 30 * 24 * time.Hour
	})
	invite, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	result, err := service.Pair(PairRequest{DeviceName: "phone", Code: invite.Code}, "10.0.0.9")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}

	device, err := service.Authenticate(result.Token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if device.ID != result.Device.ID || device.Scope != ScopeOperator {
		t.Fatalf("device = %+v, want the paired one", device)
	}

	if _, err := service.Authenticate(result.Token + "x"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("tampered token = %v, want ErrUnauthorized", err)
	}
	if _, err := service.Authenticate("d_0123456789abcdef.notbase64!"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("malformed token = %v, want ErrUnauthorized", err)
	}
	if _, err := service.Authenticate("d_0123456789abcdef.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("unknown device = %v, want ErrUnauthorized", err)
	}

	// A request past the sliding window extends the expiry; one before it does not.
	before := device.ExpiresAt
	clock.advance(2 * time.Hour)
	slid, err := service.Authenticate(result.Token)
	if err != nil {
		t.Fatalf("Authenticate after the window: %v", err)
	}
	if !slid.ExpiresAt.After(before) {
		t.Fatalf("expiry = %s, want it extended past %s", slid.ExpiresAt, before)
	}

	// Expiry refuses a token no matter how valid the secret is.
	clock.advance(30 * 24 * time.Hour)
	if _, err := service.Authenticate(result.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired token = %v, want ErrUnauthorized", err)
	}
}

func TestRefreshRotatesTheTokenAndKeepsTheDevice(t *testing.T) {
	service, _ := newTestService(t, nil)
	invite, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	paired, err := service.Pair(PairRequest{DeviceName: "laptop", Code: invite.Code}, "10.0.1.1")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}

	refreshed, err := service.Refresh(paired.Token)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if refreshed.Token == paired.Token {
		t.Fatal("Refresh returned the same token")
	}
	if refreshed.Device.ID != paired.Device.ID || refreshed.Device.Scope != paired.Device.Scope {
		t.Fatalf("refresh changed the device: %+v → %+v", paired.Device, refreshed.Device)
	}
	if _, err := service.Authenticate(paired.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old token after refresh = %v, want ErrUnauthorized", err)
	}
	if _, err := service.Authenticate(refreshed.Token); err != nil {
		t.Fatalf("new token after refresh: %v", err)
	}
}

func TestRevokeIsImmediateAndNotifies(t *testing.T) {
	service, _ := newTestService(t, nil)
	invite, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	paired, err := service.Pair(PairRequest{DeviceName: "old phone", Code: invite.Code}, "10.0.2.1")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	var revoked []string
	service.SetRevokeHook(func(id string) { revoked = append(revoked, id) })

	if err := service.Revoke(paired.Device.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if len(revoked) != 1 || revoked[0] != paired.Device.ID {
		t.Fatalf("revoke hook = %v, want [%s]", revoked, paired.Device.ID)
	}
	if _, err := service.Authenticate(paired.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked token = %v, want ErrUnauthorized", err)
	}
	if got := len(service.Devices()); got != 0 {
		t.Fatalf("Devices() = %d, want the revoked one hidden", got)
	}
	if err := service.Revoke("d_ffffffffffffffff"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Revoke(unknown) = %v, want ErrNotFound", err)
	}
}

func TestDeviceLimitRefusesFurtherPairing(t *testing.T) {
	service, _ := newTestService(t, func(o *Options) { o.MaxDevices = 1 })
	first, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	paired, err := service.Pair(PairRequest{DeviceName: "one", Code: first.Code}, "10.0.3.1")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	second, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	if _, err := service.Pair(PairRequest{DeviceName: "two", Code: second.Code}, "10.0.3.2"); !errors.Is(err, ErrDeviceLimit) {
		t.Fatalf("pair over the limit = %v, want ErrDeviceLimit", err)
	}
	// Revoking frees the slot.
	if err := service.Revoke(paired.Device.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := service.Pair(PairRequest{DeviceName: "three", Code: second.Code}, "10.0.3.2"); err != nil {
		t.Fatalf("Pair after revoke: %v", err)
	}
}

func TestSetAdminPasswordRejectsAShortOne(t *testing.T) {
	service, _ := newTestService(t, nil)
	if err := service.SetAdminPassword("short"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("SetAdminPassword(short) = %v, want ErrWeakPassword", err)
	}
	if service.HasAdminPassword() {
		t.Fatal("a rejected password was stored")
	}
}

func TestClipDeviceNameBoundsAndDefaults(t *testing.T) {
	if got := clipDeviceName("   "); got != "unnamed device" {
		t.Fatalf("empty name = %q", got)
	}
	long := strings.Repeat("é", maxDeviceNameLength+10)
	if got := []rune(clipDeviceName(long)); len(got) != maxDeviceNameLength {
		t.Fatalf("long name clipped to %d runes, want %d", len(got), maxDeviceNameLength)
	}
}
