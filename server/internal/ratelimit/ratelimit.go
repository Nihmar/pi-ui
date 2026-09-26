// Package ratelimit is one small token bucket per key, shared by every surface that
// needs a budget: REST requests per device, token refreshes, WebSocket connects and
// prompts per session (PLAN.md §4.6).
//
// The bucket shape is deliberate: a burst the client can spend at once followed by the
// sustained rate, which is what an interactive client needs (a screen issuing several
// requests on open) without letting it hammer the server. A nil *Limiter allows
// everything, so a caller can disable a budget by passing nil.
package ratelimit

import (
	"sync"
	"time"
)

// Defaults of the buckets.
const (
	// burstFraction is the burst a per-minute rate gets when the caller does not pick
	// one: a quarter of the rate, at least one (120/min → burst 30).
	burstFraction = 4
	// idleAfter is how long an untouched bucket is kept before a sweep drops it.
	idleAfter = 10 * time.Minute
	// sweepEvery is how often Allow checks for stale buckets.
	sweepEvery = time.Minute
)

// Limiter is a token bucket per key. It is safe for concurrent use.
type Limiter struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64
	buckets map[string]*bucket
	now     func() time.Time

	lastSweep time.Time
}

// bucket is one key's state: fractional tokens and when they were refilled.
type bucket struct {
	tokens float64
	last   time.Time
}

// New builds a limiter of perMinute requests with a burst of a quarter of the rate
// (at least one). perMinute <= 0 returns nil, the limiter that allows everything.
func New(perMinute int) *Limiter {
	if perMinute <= 0 {
		return nil
	}
	burst := perMinute / burstFraction
	if burst < 1 {
		burst = 1
	}
	return newLimiter(float64(perMinute)/60, float64(burst), time.Now)
}

// newLimiter is the injectable-clock constructor the tests use.
func newLimiter(perSecond, burst float64, now func() time.Time) *Limiter {
	return &Limiter{
		rate:      perSecond,
		burst:     burst,
		buckets:   map[string]*bucket{},
		now:       now,
		lastSweep: now(),
	}
}

// Allow spends one token for key. It reports whether the request may proceed and, when
// it may not, how long the caller should wait. A nil limiter always allows.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked(now)

	b, ok := l.buckets[key]
	if !ok {
		// A fresh key starts full, so the first burst of a client is free.
		l.buckets[key] = &bucket{tokens: l.burst - 1, last: now}
		return true, 0
	}

	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens = min(l.burst, b.tokens+elapsed*l.rate)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	// The time until the bucket holds one token again.
	missing := 1 - b.tokens
	wait := time.Duration(missing / l.rate * float64(time.Second))
	if wait < time.Millisecond {
		wait = time.Millisecond
	}
	return false, wait
}

// sweepLocked drops the buckets nobody touched for a while, so a long-running server
// does not keep one per device or session forever. Callers hold the lock.
func (l *Limiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < sweepEvery {
		return
	}
	l.lastSweep = now
	for key, b := range l.buckets {
		if now.Sub(b.last) > idleAfter {
			delete(l.buckets, key)
		}
	}
}

// Len reports the tracked keys, for tests and diagnostics.
func (l *Limiter) Len() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
