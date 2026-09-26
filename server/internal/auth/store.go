package auth

import (
	"sync"
	"time"
)

// Scope is what a device token may do (docs/api-v1.md, "Scopes"). It mirrors the
// wire enum of schemas/core.json; the api layer maps it onto its own scope type.
type Scope string

const (
	// ScopeViewer reads.
	ScopeViewer Scope = "viewer"
	// ScopeOperator drives sessions (the default of a pairing).
	ScopeOperator Scope = "operator"
	// ScopeAdmin manages the server itself; only the password branch grants it.
	ScopeAdmin Scope = "admin"
)

// Valid reports whether the scope is one of the three the taxonomy defines.
func (s Scope) Valid() bool {
	switch s {
	case ScopeViewer, ScopeOperator, ScopeAdmin:
		return true
	default:
		return false
	}
}

// Device is the projection a client sees: never the token, never a hash.
type Device struct {
	// ID is the opaque device identifier ("d_" + 16 hex), also the public half of
	// the token.
	ID string
	// Name is the name chosen at pairing time.
	Name string
	// Platform is the optional platform hint ("android", "linux", "windows").
	Platform string
	// Scope is what the device may do.
	Scope Scope
	// CreatedAt is when the device was paired.
	CreatedAt time.Time
	// LastSeenAt is the last authenticated request (coarse: one write window).
	LastSeenAt time.Time
	// ExpiresAt is the current sliding expiry.
	ExpiresAt time.Time
	// Revoked marks a device that was revoked; it stays in the store for the audit.
	Revoked bool
}

// Active reports whether the device may still authenticate at now.
func (d Device) Active(now time.Time) bool {
	return !d.Revoked && now.Before(d.ExpiresAt)
}

// DeviceRecord is what the store persists: the projection plus the verifier of the
// token secret. The secret itself is never stored.
type DeviceRecord struct {
	Device
	// Salt is the per-device salt of the secret hash.
	Salt []byte
	// Hash is the argon2id hash of the token secret.
	Hash []byte
	// Params are the parameters the hash was written with.
	Params Argon2Params
}

// AdminPassword is the stored verifier of the admin password.
type AdminPassword struct {
	// Salt is the per-password salt.
	Salt []byte
	// Hash is the argon2id hash of the password.
	Hash []byte
	// Params are the parameters the hash was written with.
	Params Argon2Params
	// SetAt is when the password was last set.
	SetAt time.Time
}

// Store persists what must survive a restart: the devices and the admin password
// hash. MemoryStore ships with the first slice; the SQLite implementation lands
// behind this same interface, so nothing above it changes.
type Store interface {
	// SaveDevice inserts or replaces one device record.
	SaveDevice(rec DeviceRecord) error
	// Device returns one record by id.
	Device(id string) (DeviceRecord, bool)
	// Devices returns every record, including revoked ones, for the store to own
	// the retention decision; callers filter.
	Devices() []DeviceRecord
	// RevokeDevice marks a device revoked at a moment; an unknown id is ErrNotFound.
	RevokeDevice(id string, at time.Time) error
	// AdminPassword returns the password verifier, or false when none is set.
	AdminPassword() (AdminPassword, bool)
	// SetAdminPassword stores or replaces the password verifier.
	SetAdminPassword(password AdminPassword) error
}

// MemoryStore is the in-process Store: enough for tests and for a server whose
// devices die with the process. It copies every byte slice in and out, so a caller
// can never reach the stored verifier by holding on to a record.
type MemoryStore struct {
	mu        sync.Mutex
	devices   map[string]DeviceRecord
	order     []string
	password  AdminPassword
	hasPasswd bool
}

// NewMemoryStore returns an empty in-process store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{devices: map[string]DeviceRecord{}}
}

// SaveDevice implements Store.
func (m *MemoryStore) SaveDevice(rec DeviceRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.devices[rec.ID] = cloneRecord(rec)
	if !containsID(m.order, rec.ID) {
		m.order = append(m.order, rec.ID)
	}
	return nil
}

// Device implements Store.
func (m *MemoryStore) Device(id string) (DeviceRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.devices[id]
	if !ok {
		return DeviceRecord{}, false
	}
	return cloneRecord(rec), true
}

// Devices implements Store, in creation order.
func (m *MemoryStore) Devices() []DeviceRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]DeviceRecord, 0, len(m.order))
	for _, id := range m.order {
		if rec, ok := m.devices[id]; ok {
			out = append(out, cloneRecord(rec))
		}
	}
	return out
}

// RevokeDevice implements Store.
func (m *MemoryStore) RevokeDevice(id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.devices[id]
	if !ok {
		return ErrNotFound
	}
	rec.Revoked = true
	rec.ExpiresAt = at
	m.devices[id] = rec
	return nil
}

// AdminPassword implements Store.
func (m *MemoryStore) AdminPassword() (AdminPassword, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.hasPasswd {
		return AdminPassword{}, false
	}
	return clonePassword(m.password), true
}

// SetAdminPassword implements Store.
func (m *MemoryStore) SetAdminPassword(password AdminPassword) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.password = clonePassword(password)
	m.hasPasswd = true
	return nil
}

func cloneRecord(rec DeviceRecord) DeviceRecord {
	rec.Salt = append([]byte(nil), rec.Salt...)
	rec.Hash = append([]byte(nil), rec.Hash...)
	return rec
}

func clonePassword(p AdminPassword) AdminPassword {
	p.Salt = append([]byte(nil), p.Salt...)
	p.Hash = append([]byte(nil), p.Hash...)
	return p
}

func containsID(ids []string, id string) bool {
	for _, got := range ids {
		if got == id {
			return true
		}
	}
	return false
}
