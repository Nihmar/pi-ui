package auth

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// Defaults of the service.
const (
	// defaultTokenTTL is the sliding life of a device token.
	defaultTokenTTL = 30 * 24 * time.Hour
	// defaultPairingTTL is how long a pairing invitation stays valid.
	defaultPairingTTL = 10 * time.Minute
	// defaultMaxDevices bounds the paired devices.
	defaultMaxDevices = 32
	// defaultPairLimit and defaultPairWindow bound pairing and password attempts per
	// caller.
	defaultPairLimit  = 5
	defaultPairWindow = time.Minute
	// defaultSlidingRefresh is the coarsest rate at which an authenticated request
	// extends a token: one write per device per hour, not one per request.
	defaultSlidingRefresh = time.Hour

	// MinPasswordLength is the shortest admin password SetAdminPassword accepts.
	MinPasswordLength = 8
	// maxDeviceNameLength bounds a client-chosen name in runes.
	maxDeviceNameLength = 64

	// deviceIDPrefix and deviceIDBytes define the opaque device id ("d_" + 16 hex).
	deviceIDPrefix = "d_"
	deviceIDBytes  = 8
	// tokenSecretBytes is the entropy of a token secret.
	tokenSecretBytes = 32
	// minTokenSecretBytes is the shortest secret parseToken accepts, so a truncated
	// or bogus token fails before argon2 runs.
	minTokenSecretBytes = 16
)

// Options tunes one Service. Every zero field falls back to its default; clock and
// randomness are injectable so tests are deterministic and the argon2id cost is
// cheap there and real in production.
type Options struct {
	// TokenTTL is the sliding life of a device token (default 30 days).
	TokenTTL time.Duration
	// PairingTTL is how long an invitation stays usable (default 10 minutes).
	PairingTTL time.Duration
	// MaxDevices bounds the paired devices (default 32).
	MaxDevices int
	// Now is the clock (default time.Now).
	Now func() time.Time
	// Rand is the randomness behind ids, salts and secrets (default crypto/rand).
	Rand io.Reader
	// Argon2 is the credential hashing cost (default DefaultArgon2Params).
	Argon2 Argon2Params
	// PairLimit and PairWindow are the pairing/password attempt budget per caller
	// (default 5 per minute).
	PairLimit  int
	PairWindow time.Duration
	// SlidingRefresh is how much time must pass before an authenticated request
	// extends the token expiry (default one hour).
	SlidingRefresh time.Duration
	// Invites is where pairing invitations live (default NewMemoryInviteStore).
	Invites InviteStore
}

// withDefaults fills the zero fields of Options.
func withDefaults(o Options) Options {
	if o.TokenTTL <= 0 {
		o.TokenTTL = defaultTokenTTL
	}
	if o.PairingTTL <= 0 {
		o.PairingTTL = defaultPairingTTL
	}
	if o.MaxDevices <= 0 {
		o.MaxDevices = defaultMaxDevices
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Rand == nil {
		o.Rand = cryptoRandom
	}
	if o.Argon2 == (Argon2Params{}) {
		o.Argon2 = DefaultArgon2Params()
	}
	if o.PairLimit <= 0 {
		o.PairLimit = defaultPairLimit
	}
	if o.PairWindow <= 0 {
		o.PairWindow = defaultPairWindow
	}
	if o.SlidingRefresh <= 0 {
		o.SlidingRefresh = defaultSlidingRefresh
	}
	if o.Invites == nil {
		o.Invites = NewMemoryInviteStore()
	}
	return o
}

// PairRequest is one pairing attempt: the credential plus the device metadata. The
// api layer builds it from a validated PairRequest DTO.
type PairRequest struct {
	// DeviceName is the name shown in the device list; empty becomes
	// "unnamed device".
	DeviceName string
	// Platform is the optional client platform hint.
	Platform string
	// Code is the pairing code from a QR invite or typed by the user.
	Code string
	// Secret is the QR-only secret; empty for a typed invitation and for the
	// password branch.
	Secret string
	// Password is the admin password; takes precedence over a code.
	Password string
}

// PairResult is what pairing and refresh hand back: the device projection and the
// token, delivered exactly once.
type PairResult struct {
	// Device is the projection; it never contains a hash.
	Device Device
	// Token is the bearer credential, "<deviceId>.<secret>".
	Token string
	// ExpiresAt is the token's sliding expiry.
	ExpiresAt time.Time
}

// Service is the auth core: invitations, pairing, verification, refresh and
// revocation. It is safe for concurrent use.
type Service struct {
	opts    Options
	store   Store
	invites InviteStore
	limiter *attemptLimiter

	mu     sync.Mutex
	revoke func(deviceID string)
	// pairing counts the slot reservations of the pairings being hashed right now; see
	// reserveDevice.
	pairing int
}

// New builds a Service over store. It panics on an invalid configuration (an argon2
// parameter set argon2 itself would reject, a missing store): those are wiring
// defects, not runtime conditions.
func New(store Store, opts Options) *Service {
	if store == nil {
		panic("auth: New called without a Store")
	}
	opts = withDefaults(opts)
	if err := opts.Argon2.validate(); err != nil {
		panic("auth: " + err.Error())
	}
	return &Service{
		opts:    opts,
		store:   store,
		invites: opts.Invites,
		limiter: newAttemptLimiter(opts.PairLimit, opts.PairWindow),
	}
}

// now reads the injected clock.
func (s *Service) now() time.Time { return s.opts.Now() }

// NewInvite creates a pairing invitation for the given kind. The caller (the CLI or
// the admin page) renders it as a QR or as a typed code; nothing about it is stored
// on disk.
func (s *Service) NewInvite(kind InviteKind) (Invite, error) {
	if kind != InviteQR && kind != InviteTyped {
		return Invite{}, fmt.Errorf("auth: unknown invite kind %q", kind)
	}
	code, err := newInviteCode(s.opts.Rand)
	if err != nil {
		return Invite{}, err
	}
	invite := Invite{Kind: kind, Code: code, ExpiresAt: s.now().Add(s.opts.PairingTTL)}
	if kind == InviteQR {
		secret := make([]byte, tokenSecretBytes)
		if _, err := io.ReadFull(s.opts.Rand, secret); err != nil {
			return Invite{}, fmt.Errorf("auth: pairing secret: %w", err)
		}
		invite.Secret = base64.RawURLEncoding.EncodeToString(secret)
	}
	if err := s.invites.Save(invite); err != nil {
		return Invite{}, fmt.Errorf("auth: saving the invitation: %w", err)
	}
	return invite, nil
}

// PendingInvites reports how many invitations are waiting, for diagnostics.
func (s *Service) PendingInvites() int { return s.invites.Pending(s.now()) }

// Pair exchanges a code (with the QR secret) or the admin password for a device
// token. callerKey bounds the attempts (the client IP); every attempt counts, so a
// success does not buy extra guesses.
func (s *Service) Pair(req PairRequest, callerKey string) (PairResult, error) {
	now := s.now()
	if !s.limiter.allow(callerKey, now) {
		return PairResult{}, &RateLimitError{RetryAfter: s.limiter.retryAfter(callerKey, now)}
	}
	s.limiter.record(callerKey, now)

	// Capacity is reserved before the credential, and the reservation is what makes the
	// limit hold: a pairing refused for the device limit must not burn the invitation that
	// was presented, and the check cannot be a bare count — hashing the credential takes
	// long enough that a burst of requests all read the same count and all store a device.
	if err := s.reserveDevice(now); err != nil {
		return PairResult{}, err
	}
	defer s.releaseDevice()

	switch {
	case req.Password != "":
		return s.pairWithPassword(req, now)
	case req.Code != "":
		return s.pairWithCode(req, now)
	default:
		return PairResult{}, unauthorized("neither a pairing code nor a password was sent")
	}
}

// pairWithCode consumes an invitation and mints an operator device.
func (s *Service) pairWithCode(req PairRequest, now time.Time) (PairResult, error) {
	_, ok := s.invites.Take(req.Code, req.Secret, now)
	if !ok {
		return PairResult{}, unauthorized("unknown, expired or mismatched pairing invitation")
	}
	return s.createDevice(req, ScopeOperator, now)
}

// pairWithPassword verifies the admin password and mints an admin device. It is
// the recovery path for an installation with no enabled devices.
func (s *Service) pairWithPassword(req PairRequest, now time.Time) (PairResult, error) {
	stored, ok := s.store.AdminPassword()
	if !ok {
		return PairResult{}, fmt.Errorf("%w: %w", ErrUnauthorized, ErrNoPassword)
	}
	if !verifyCredential([]byte(req.Password), stored.Salt, stored.Hash, stored.Params) {
		return PairResult{}, unauthorized("admin password does not match")
	}
	return s.createDevice(req, ScopeAdmin, now)
}

// createDevice mints the device id, its token secret and the stored record.
func (s *Service) createDevice(req PairRequest, scope Scope, now time.Time) (PairResult, error) {
	id, err := newDeviceID(s.opts.Rand)
	if err != nil {
		return PairResult{}, err
	}
	secret := make([]byte, tokenSecretBytes)
	if _, err := io.ReadFull(s.opts.Rand, secret); err != nil {
		return PairResult{}, fmt.Errorf("auth: token secret: %w", err)
	}
	salt, hash, err := hashCredential(secret, s.opts.Argon2, s.opts.Rand)
	if err != nil {
		return PairResult{}, err
	}
	record := DeviceRecord{
		Device: Device{
			ID:         id,
			Name:       clipDeviceName(req.DeviceName),
			Platform:   strings.TrimSpace(req.Platform),
			Scope:      scope,
			CreatedAt:  now,
			LastSeenAt: now,
			ExpiresAt:  now.Add(s.opts.TokenTTL),
		},
		Salt:   salt,
		Hash:   hash,
		Params: s.opts.Argon2,
	}
	if err := s.store.SaveDevice(record); err != nil {
		return PairResult{}, fmt.Errorf("auth: saving device: %w", err)
	}
	return PairResult{
		Device:    record.Device,
		Token:     encodeToken(id, secret),
		ExpiresAt: record.ExpiresAt,
	}, nil
}

// reserveDevice takes one of the MaxDevices slots: the count plus the reservations already
// held. Pairing hashes a credential between this check and the stored record, which is why
// the slot is reserved rather than counted — the two halves have to be one decision.
func (s *Service) reserveDevice(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	active := 0
	for _, rec := range s.store.Devices() {
		if rec.Device.Active(now) {
			active++
		}
	}
	if active+s.pairing >= s.opts.MaxDevices {
		return fmt.Errorf("%w: %d devices are paired", ErrDeviceLimit, active+s.pairing)
	}
	s.pairing++
	return nil
}

// releaseDevice gives a reservation back: a pairing that failed stored nothing, and the
// slot has to be usable by the next attempt.
func (s *Service) releaseDevice() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pairing > 0 {
		s.pairing--
	}
}

// Authenticate verifies a bearer token and returns the device behind it. Every
// failure is ErrUnauthorized with a reason that stays in the server log. A valid
// request extends the sliding expiry at most once per SlidingRefresh window.
func (s *Service) Authenticate(token string) (Device, error) {
	id, secret, ok := parseToken(token)
	if !ok {
		return Device{}, unauthorized("malformed token")
	}
	record, ok := s.store.Device(id)
	if !ok {
		return Device{}, unauthorized("unknown device %s", id)
	}
	now := s.now()
	switch {
	case record.Revoked:
		return Device{}, unauthorized("revoked device %s", id)
	case !now.Before(record.ExpiresAt):
		return Device{}, unauthorized("expired device %s", id)
	case !verifyCredential(secret, record.Salt, record.Hash, record.Params):
		return Device{}, unauthorized("bad secret for device %s", id)
	}

	if now.Sub(record.LastSeenAt) >= s.opts.SlidingRefresh {
		// Best effort: a request must not fail because the slide could not be
		// written, and the coarse window keeps this to one write an hour.
		record.LastSeenAt = now
		record.ExpiresAt = now.Add(s.opts.TokenTTL)
		_ = s.store.SaveDevice(record)
	}
	return record.Device, nil
}

// Refresh rotates the token of the device presenting it: new secret, extended
// sliding expiry, same device id and scope. The previous token stops working as
// soon as the new record is stored.
func (s *Service) Refresh(token string) (PairResult, error) {
	device, err := s.Authenticate(token)
	if err != nil {
		return PairResult{}, err
	}
	record, ok := s.store.Device(device.ID)
	if !ok {
		return PairResult{}, unauthorized("device %s vanished during refresh", device.ID)
	}
	secret := make([]byte, tokenSecretBytes)
	if _, err := io.ReadFull(s.opts.Rand, secret); err != nil {
		return PairResult{}, fmt.Errorf("auth: token secret: %w", err)
	}
	salt, hash, err := hashCredential(secret, s.opts.Argon2, s.opts.Rand)
	if err != nil {
		return PairResult{}, err
	}
	now := s.now()
	record.Salt = salt
	record.Hash = hash
	record.Params = s.opts.Argon2
	record.LastSeenAt = now
	record.ExpiresAt = now.Add(s.opts.TokenTTL)
	if err := s.store.SaveDevice(record); err != nil {
		return PairResult{}, fmt.Errorf("auth: saving device: %w", err)
	}
	return PairResult{
		Device:    record.Device,
		Token:     encodeToken(record.ID, secret),
		ExpiresAt: record.ExpiresAt,
	}, nil
}

// Devices lists the active devices, oldest first.
func (s *Service) Devices() []Device {
	now := s.now()
	devices := make([]Device, 0)
	for _, record := range s.store.Devices() {
		if record.Device.Active(now) {
			devices = append(devices, record.Device)
		}
	}
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].CreatedAt.Equal(devices[j].CreatedAt) {
			return devices[i].ID < devices[j].ID
		}
		return devices[i].CreatedAt.Before(devices[j].CreatedAt)
	})
	return devices
}

// Revoke makes a device stop authenticating immediately and reports the id to the
// revoke hook (the WebSocket layer closes that device's connections there).
func (s *Service) Revoke(deviceID string) error {
	if err := s.store.RevokeDevice(deviceID, s.now()); err != nil {
		return err
	}
	s.mu.Lock()
	hook := s.revoke
	s.mu.Unlock()
	if hook != nil {
		hook(deviceID)
	}
	return nil
}

// SetRevokeHook installs the callback a revocation runs after the store is
// updated. Passing nil removes it.
func (s *Service) SetRevokeHook(fn func(deviceID string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revoke = fn
}

// SetAdminPassword stores a new admin password verifier. It is the operator
// action behind `pi-ui auth set-password`.
func (s *Service) SetAdminPassword(password string) error {
	if len([]rune(password)) < MinPasswordLength {
		return fmt.Errorf("%w: at least %d characters", ErrWeakPassword, MinPasswordLength)
	}
	salt, hash, err := hashCredential([]byte(password), s.opts.Argon2, s.opts.Rand)
	if err != nil {
		return err
	}
	return s.store.SetAdminPassword(AdminPassword{
		Salt:   salt,
		Hash:   hash,
		Params: s.opts.Argon2,
		SetAt:  s.now(),
	})
}

// HasAdminPassword reports whether the password branch is usable.
func (s *Service) HasAdminPassword() bool {
	_, ok := s.store.AdminPassword()
	return ok
}

// Configured reports whether any identity exists yet: a paired device or an admin
// password. An unconfigured server is the bootstrap state the plan describes (loopback,
// first start), where a local peer is trusted as the operator because there is nothing
// to check it against; the moment an identity exists, a local peer without a token is
// only a viewer.
func (s *Service) Configured() bool {
	return s.HasAdminPassword() || len(s.Devices()) > 0
}

// encodeToken builds the bearer credential: the public device id plus the secret.
func encodeToken(id string, secret []byte) string {
	return id + "." + base64.RawURLEncoding.EncodeToString(secret)
}

// parseToken splits a token and validates its shape, so a bogus value never reaches
// argon2.
func parseToken(token string) (id string, secret []byte, ok bool) {
	id, encoded, found := strings.Cut(token, ".")
	if !found || !validDeviceID(id) || encoded == "" {
		return "", nil, false
	}
	secret, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(secret) < minTokenSecretBytes {
		return "", nil, false
	}
	return id, secret, true
}

// newDeviceID mints an opaque device id.
func newDeviceID(random io.Reader) (string, error) {
	raw := make([]byte, deviceIDBytes)
	if _, err := io.ReadFull(random, raw); err != nil {
		return "", fmt.Errorf("auth: device id: %w", err)
	}
	return deviceIDPrefix + hex.EncodeToString(raw), nil
}

// validDeviceID reports whether id has the shape newDeviceID produces.
func validDeviceID(id string) bool {
	if !strings.HasPrefix(id, deviceIDPrefix) || len(id) != len(deviceIDPrefix)+deviceIDBytes*2 {
		return false
	}
	for _, char := range id[len(deviceIDPrefix):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

// clipDeviceName trims a client name to the stored bound in runes, so a long name
// cannot bloat the device list.
func clipDeviceName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "unnamed device"
	}
	runes := []rune(trimmed)
	if len(runes) > maxDeviceNameLength {
		return string(runes[:maxDeviceNameLength])
	}
	return trimmed
}
