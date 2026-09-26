package auth

import (
	"sync"
	"time"
)

// attemptLimiter is a sliding-window counter per caller key (the client IP of a
// pairing attempt). It bounds brute force on a short code or password without a
// dependency and without unbounded memory: an empty key is forgotten as soon as its
// window closes.
type attemptLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	attempts map[string][]time.Time
}

// newAttemptLimiter builds a limiter of limit attempts per window.
func newAttemptLimiter(limit int, window time.Duration) *attemptLimiter {
	return &attemptLimiter{limit: limit, window: window, attempts: map[string][]time.Time{}}
}

// allow reports whether key may attempt now.
func (l *attemptLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.pruneLocked(key, now)) < l.limit
}

// record counts one attempt from key.
func (l *attemptLimiter) record(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts[key] = append(l.pruneLocked(key, now), now)
}

// retryAfter reports how long key must wait before an attempt is allowed, or zero
// when one is allowed now.
func (l *attemptLimiter) retryAfter(key string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.pruneLocked(key, now)
	if len(recent) < l.limit || len(recent) == 0 {
		return 0
	}
	// The oldest attempt of the window leaves it after window, freeing one slot.
	return max(recent[0].Add(l.window).Sub(now), 0)
}

// pruneLocked drops the attempts that fell out of the window. Callers hold the lock.
func (l *attemptLimiter) pruneLocked(key string, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
	kept := l.attempts[key][:0]
	for _, at := range l.attempts[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) == 0 {
		delete(l.attempts, key)
		return nil
	}
	l.attempts[key] = kept
	return kept
}
