package store

import (
	"context"
	"errors"
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/auth"
)

// openTest opens a state database in a temp file.
func openTest(t *testing.T) *DB {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// testService builds an auth service over this store with cheap params.
func testService(t *testing.T, db *DB) *auth.Service {
	t.Helper()
	return auth.New(db.Auth(), auth.Options{
		Invites:  db.Auth(),
		Argon2:   auth.Argon2Params{Time: 1, Memory: 8, Threads: 1, KeyLen: 32},
		Rand:     rand.New(rand.NewSource(3)),
		TokenTTL: time.Hour,
	})
}

func TestSchemaIsVersionedAndMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	ctx := context.Background()
	first, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var version int
	if err := first.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if want := len(migrations); version != want {
		t.Fatalf("schema version = %d, want %d", version, want)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopening an up-to-date database applies nothing and keeps its rows.
	second, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer second.Close()
	if got := second.Path(); got != path {
		t.Fatalf("Path() = %q, want %q", got, path)
	}
}

func TestDevicesSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	ctx := context.Background()
	firstDB, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	service := testService(t, firstDB)
	invite, err := service.NewInvite(auth.InviteTyped)
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	paired, err := service.Pair(auth.PairRequest{DeviceName: "phone", Platform: "android", Code: invite.Code}, "10.0.0.1")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if err := firstDB.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A fresh process (a new handle, a new service) sees the device and accepts its
	// token: this is the property the whole store exists for.
	secondDB, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer secondDB.Close()
	reopened := testService(t, secondDB)
	device, err := reopened.Authenticate(paired.Token)
	if err != nil {
		t.Fatalf("Authenticate after restart: %v", err)
	}
	if device.ID != paired.Device.ID || device.Name != "phone" || device.Platform != "android" {
		t.Fatalf("device after restart = %+v, want the paired one", device)
	}
	if got := len(reopened.Devices()); got != 1 {
		t.Fatalf("Devices() = %d, want 1", got)
	}
}

func TestRevocationAndThePasswordSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	ctx := context.Background()
	firstDB, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	service := testService(t, firstDB)
	if err := service.SetAdminPassword("correct horse battery staple"); err != nil {
		t.Fatalf("SetAdminPassword: %v", err)
	}
	invite, err := service.NewInvite(auth.InviteTyped)
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	paired, err := service.Pair(auth.PairRequest{DeviceName: "old", Code: invite.Code}, "10.0.0.2")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if err := service.Revoke(paired.Device.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := firstDB.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	secondDB, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer secondDB.Close()
	reopened := testService(t, secondDB)
	if _, err := reopened.Authenticate(paired.Token); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("revoked token after restart = %v, want ErrUnauthorized", err)
	}
	if !reopened.HasAdminPassword() {
		t.Fatal("the admin password did not survive the restart")
	}
	admin, err := reopened.Pair(auth.PairRequest{DeviceName: "admin", Password: "correct horse battery staple"}, "10.0.0.3")
	if err != nil {
		t.Fatalf("pairing with the stored password: %v", err)
	}
	if admin.Device.Scope != auth.ScopeAdmin {
		t.Fatalf("scope = %q, want admin", admin.Device.Scope)
	}
}

func TestInvitationsAreSharedAcrossHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	ctx := context.Background()
	minting, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer minting.Close()
	serving, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer serving.Close()

	// One handle (the `pi-ui pair` process) mints the invitation…
	pairingService := testService(t, minting)
	invite, err := pairingService.NewInvite(auth.InviteQR)
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}

	// …another (the running server) consumes it.
	servingService := testService(t, serving)
	if got := servingService.PendingInvites(); got != 1 {
		t.Fatalf("PendingInvites() on the server handle = %d, want 1", got)
	}
	paired, err := servingService.Pair(auth.PairRequest{DeviceName: "phone", Code: invite.Code, Secret: invite.Secret}, "10.0.0.4")
	if err != nil {
		t.Fatalf("Pair across handles: %v", err)
	}
	if paired.Device.Scope != auth.ScopeOperator {
		t.Fatalf("scope = %q, want operator", paired.Device.Scope)
	}
	if got := pairingService.PendingInvites(); got != 0 {
		t.Fatalf("the invitation was not consumed: %d pending", got)
	}
}

func TestInvitationTakeKeepsAMismatchedSecret(t *testing.T) {
	store := openTest(t).Auth()
	now := time.Now()
	if err := store.Save(auth.Invite{Kind: auth.InviteQR, Code: "135790", Secret: "s", ExpiresAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, ok := store.Take("135790", "wrong", now); ok {
		t.Fatal("a mismatched secret consumed the invitation")
	}
	if got := store.Pending(now); got != 1 {
		t.Fatalf("pending = %d, want the invitation kept", got)
	}
	if _, ok := store.Take("135790", "s", now); !ok {
		t.Fatal("the right code+secret did not match")
	}
	if got := store.Pending(now); got != 0 {
		t.Fatalf("pending = %d, want 0", got)
	}
}

func TestExpiredInvitationsAreNotPending(t *testing.T) {
	store := openTest(t).Auth()
	now := time.Now()
	if err := store.Save(auth.Invite{Kind: auth.InviteTyped, Code: "246801", ExpiresAt: now.Add(-time.Second)}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := store.Pending(now); got != 0 {
		t.Fatalf("pending = %d, want the expired invitation excluded", got)
	}
	if _, ok := store.Take("246801", "", now); ok {
		t.Fatal("an expired invitation was accepted")
	}
}

func TestRevokingAnUnknownDeviceIsNotFound(t *testing.T) {
	store := openTest(t).Auth()
	if err := store.RevokeDevice("d_ffffffffffffffff", time.Now()); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("RevokeDevice(unknown) = %v, want auth.ErrNotFound", err)
	}
	if _, ok := store.Device("d_ffffffffffffffff"); ok {
		t.Fatal("an unknown device was found")
	}
}
