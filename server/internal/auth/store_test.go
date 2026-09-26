package auth

import (
	"errors"
	"testing"
	"time"
)

func TestMemoryStoreCopiesItsBytes(t *testing.T) {
	store := NewMemoryStore()
	record := DeviceRecord{
		Device: Device{
			ID:        "d_0000000000000001",
			Name:      "one",
			Scope:     ScopeOperator,
			CreatedAt: time.Unix(0, 0),
			ExpiresAt: time.Unix(100, 0),
		},
		Salt: []byte{1, 2, 3},
		Hash: []byte{4, 5, 6},
	}
	if err := store.SaveDevice(record); err != nil {
		t.Fatalf("SaveDevice: %v", err)
	}

	// Mutating the slice the caller still holds must not reach the store.
	record.Salt[0] = 9
	record.Hash[0] = 9
	got, ok := store.Device(record.ID)
	if !ok {
		t.Fatal("Device() did not find the saved record")
	}
	if got.Salt[0] != 1 || got.Hash[0] != 4 {
		t.Fatalf("stored verifier was mutated through the caller's slice: %v %v", got.Salt, got.Hash)
	}

	// And the slice handed out must not be a window into the store either.
	got.Salt[0] = 7
	again, _ := store.Device(record.ID)
	if again.Salt[0] != 1 {
		t.Fatalf("the returned record aliases the store: %v", again.Salt)
	}
}

func TestMemoryStoreKeepsCreationOrderAndRevokes(t *testing.T) {
	store := NewMemoryStore()
	now := time.Unix(0, 0)
	for _, id := range []string{"d_0000000000000002", "d_0000000000000001"} {
		if err := store.SaveDevice(DeviceRecord{Device: Device{ID: id, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}}); err != nil {
			t.Fatalf("SaveDevice(%s): %v", id, err)
		}
	}
	records := store.Devices()
	if len(records) != 2 || records[0].ID != "d_0000000000000002" {
		t.Fatalf("Devices() = %+v, want creation order", records)
	}
	if err := store.RevokeDevice("d_0000000000000001", now.Add(time.Minute)); err != nil {
		t.Fatalf("RevokeDevice: %v", err)
	}
	revoked, _ := store.Device("d_0000000000000001")
	if !revoked.Revoked {
		t.Fatal("the record was not marked revoked")
	}
	if err := store.RevokeDevice("d_ffffffffffffffff", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RevokeDevice(unknown) = %v, want ErrNotFound", err)
	}
}

func TestMemoryStorePasswordRoundTrip(t *testing.T) {
	store := NewMemoryStore()
	if _, ok := store.AdminPassword(); ok {
		t.Fatal("a fresh store reports a password")
	}
	password := AdminPassword{Salt: []byte{1}, Hash: []byte{2}, SetAt: time.Unix(5, 0)}
	if err := store.SetAdminPassword(password); err != nil {
		t.Fatalf("SetAdminPassword: %v", err)
	}
	password.Salt[0] = 9 // the caller's slice is not the store's
	got, ok := store.AdminPassword()
	if !ok || got.Salt[0] != 1 || got.SetAt.Unix() != 5 {
		t.Fatalf("AdminPassword() = %+v, want the stored verifier", got)
	}
}

func TestInviteStoreIsSingleUseAndKeepsMismatches(t *testing.T) {
	now := time.Unix(1000, 0)
	store := &inviteStore{}
	store.put(Invite{Kind: InviteQR, Code: "123456", Secret: "s3cret", ExpiresAt: now.Add(time.Minute)})
	store.put(Invite{Kind: InviteTyped, Code: "654321", ExpiresAt: now.Add(time.Minute)})
	store.put(Invite{Kind: InviteTyped, Code: "000000", ExpiresAt: now.Add(-time.Second)})

	if _, ok := store.take("123456", "wrong", now); ok {
		t.Fatal("a mismatched secret consumed an invitation")
	}
	if store.len() != 2 {
		t.Fatalf("pending invites = %d, want 2 (the expired one was pruned)", store.len())
	}
	if _, ok := store.take("123456", "s3cret", now); !ok {
		t.Fatal("the correct code+secret did not match")
	}
	if _, ok := store.take("654321", "", now); !ok {
		t.Fatal("a typed invitation did not match without a secret")
	}
	if _, ok := store.take("000000", "", now); ok {
		t.Fatal("an expired invitation was accepted")
	}
	if store.len() != 0 {
		t.Fatalf("pending invites = %d, want 0", store.len())
	}
}

func TestAttemptLimiterWindowAndRetryAfter(t *testing.T) {
	now := time.Unix(1000, 0)
	limiter := newAttemptLimiter(2, time.Minute)

	if !limiter.allow("ip", now) {
		t.Fatal("the first attempt was refused")
	}
	limiter.record("ip", now)
	now = now.Add(10 * time.Second)
	limiter.record("ip", now)
	if limiter.allow("ip", now) {
		t.Fatal("a third attempt inside the window was allowed")
	}
	if got := limiter.retryAfter("ip", now); got != 50*time.Second {
		t.Fatalf("retryAfter = %s, want 50s", got)
	}
	if !limiter.allow("other", now) {
		t.Fatal("another key was rate limited")
	}
	now = now.Add(51 * time.Second)
	if !limiter.allow("ip", now) {
		t.Fatal("the window did not free a slot")
	}
	if got := limiter.retryAfter("ip", now); got != 0 {
		t.Fatalf("retryAfter = %s, want 0", got)
	}
	if len(limiter.attempts) != 1 {
		t.Fatalf("limiter kept %d keys, want only the active one", len(limiter.attempts))
	}
}

func TestArgon2ParamsValidate(t *testing.T) {
	for name, params := range map[string]Argon2Params{
		"no time":    {Time: 0, Memory: 8, Threads: 1, KeyLen: 32},
		"no memory":  {Time: 1, Memory: 4, Threads: 1, KeyLen: 32},
		"no threads": {Time: 1, Memory: 8, Threads: 0, KeyLen: 32},
		"short key":  {Time: 1, Memory: 8, Threads: 1, KeyLen: 8},
	} {
		if err := params.validate(); err == nil {
			t.Errorf("%s: validate() = nil, want an error", name)
		}
	}
	if err := DefaultArgon2Params().validate(); err != nil {
		t.Errorf("the default params do not validate: %v", err)
	}
}
