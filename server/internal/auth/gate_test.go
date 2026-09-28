package auth

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// gateRand is a randomness source that holds every caller once it is armed, until `want`
// of them are inside the credential hashing or the fallback deadline passes. It exists to
// stand in the window between the capacity check and the stored record deterministically:
// in production that window is as wide as an argon2id hash, which a burst of pairing
// requests fits into comfortably.
type gateRand struct {
	mu          sync.Mutex
	armed       bool
	want        int
	waiters     int
	release     chan struct{}
	releaseOnce sync.Once
	next        byte
}

func newGateRand(want int, fallback time.Duration) *gateRand {
	gate := &gateRand{want: want, release: make(chan struct{})}
	time.AfterFunc(fallback, gate.open)
	return gate
}

// arm makes the gate hold callers from here on.
func (g *gateRand) arm() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.armed = true
}

// open releases every caller, whatever the arrival count.
func (g *gateRand) open() {
	g.releaseOnce.Do(func() { close(g.release) })
}

func (g *gateRand) Read(p []byte) (int, error) {
	g.mu.Lock()
	if g.armed {
		g.waiters++
		if g.waiters >= g.want {
			g.open()
		}
	}
	armed := g.armed
	g.mu.Unlock()

	if armed {
		<-g.release
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	for index := range p {
		g.next = g.next*31 + 7
		p[index] = g.next
	}
	return len(p), nil
}

// TestPairingCannotExceedTheDeviceLimitUnderConcurrency pins the capacity bound: the slot
// has to be reserved before the credential is hashed, or a burst of pairing requests all
// read the same device count and all of them store a device.
func TestPairingCannotExceedTheDeviceLimitUnderConcurrency(t *testing.T) {
	const limit = 2
	const attempts = 8
	password := "correct horse battery staple"

	gate := newGateRand(attempts, 250*time.Millisecond)
	service, _ := newTestService(t, func(o *Options) {
		o.MaxDevices = limit
		o.Rand = gate
	})
	if err := service.SetAdminPassword(password); err != nil {
		t.Fatal(err)
	}
	gate.arm()

	start := make(chan struct{})
	var mu sync.Mutex
	paired := 0
	var wg sync.WaitGroup
	for index := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := service.Pair(PairRequest{
				DeviceName: "burst",
				Password:   password,
			}, fmt.Sprintf("10.0.0.%d", index+1))
			if err == nil {
				mu.Lock()
				paired++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	if paired > limit {
		t.Fatalf("%d pairings succeeded at once, the limit is %d", paired, limit)
	}
	if stored := service.Devices(); len(stored) > limit {
		t.Fatalf("%d devices stored, the limit is %d", len(stored), limit)
	}
}
