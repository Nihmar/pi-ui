package ratelimit

import (
	"testing"
	"time"
)

func TestBurstThenSustainedRate(t *testing.T) {
	now := time.Unix(1000, 0)
	limiter := newLimiter(1, 3, func() time.Time { return now }) // 1/s, burst 3

	// The first three requests spend the burst.
	for i := 0; i < 3; i++ {
		if ok, _ := limiter.Allow("a"); !ok {
			t.Fatalf("request %d denied inside the burst", i+1)
		}
	}
	ok, wait := limiter.Allow("a")
	if ok {
		t.Fatal("the fourth request was allowed with an empty bucket")
	}
	if wait <= 0 || wait > time.Second {
		t.Fatalf("retryAfter = %s, want up to 1s", wait)
	}

	// Another key has its own bucket.
	if ok, _ := limiter.Allow("b"); !ok {
		t.Fatal("a different key was denied")
	}

	// The sustained rate refills one token per second.
	now = now.Add(time.Second)
	if ok, _ := limiter.Allow("a"); !ok {
		t.Fatal("the token did not refill after one second")
	}
	if ok, _ := limiter.Allow("a"); ok {
		t.Fatal("the bucket refilled more than one token in one second")
	}
}

func TestDisabledLimiterAllowsEverything(t *testing.T) {
	var limiter *Limiter
	for i := 0; i < 100; i++ {
		if ok, wait := limiter.Allow("a"); !ok || wait != 0 {
			t.Fatalf("disabled limiter denied request %d", i+1)
		}
	}
	if got := limiter.Len(); got != 0 {
		t.Fatalf("disabled limiter tracks %d keys", got)
	}
	if limiter != New(0) {
		t.Fatal("New(0) should return the disabled limiter")
	}
}

func TestSweepDropsIdleBuckets(t *testing.T) {
	now := time.Unix(1000, 0)
	limiter := newLimiter(1, 1, func() time.Time { return now })

	if ok, _ := limiter.Allow("a"); !ok {
		t.Fatal("the first request was denied")
	}
	now = now.Add(idleAfter + sweepEvery + time.Second)
	if ok, _ := limiter.Allow("b"); !ok {
		t.Fatal("the request that triggers the sweep was denied")
	}
	if got := limiter.Len(); got != 1 {
		t.Fatalf("tracked keys = %d, want only the fresh one", got)
	}
	// The stale key starts fresh again, which is the point of dropping it.
	if ok, _ := limiter.Allow("a"); !ok {
		t.Fatal("a swept key did not start fresh")
	}
}

func TestNewDerivesABurst(t *testing.T) {
	limiter := New(120)
	if limiter == nil {
		t.Fatal("New(120) returned the disabled limiter")
	}
	if limiter.burst != 30 {
		t.Fatalf("burst = %v, want 30 (a quarter of the rate)", limiter.burst)
	}
	if small := New(2); small.burst != 1 {
		t.Fatalf("burst for 2/min = %v, want at least 1", small.burst)
	}
}
