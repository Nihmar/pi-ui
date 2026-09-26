package sessions

import (
	"strings"
	"time"
)

// watchIdle is the eviction watchdog (PLAN.md §4.2): every WatchInterval it looks for
// sessions that have been settled and quiet for IdleTimeout and wraps them up. It runs
// on the manager's lifecycle context, so Shutdown stops it.
func (m *Manager) watchIdle() {
	defer m.watchWG.Done()

	ticker := time.NewTicker(m.cfg.WatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.lifeCtx.Done():
			return
		case <-ticker.C:
		}
		now := time.Now().UTC()
		for _, s := range m.all() {
			if m.idleExpired(s, now) {
				// Each eviction gets its own goroutine: a slow handoff turn must not
				// hold the sweep from looking at every other session.
				go m.evict(s)
			}
		}
	}
}

// idleExpired reports whether the session should be wrapped up: it is settled (ready,
// not already being evicted) and has produced nothing for the idle window. A spawning or
// streaming session is never idle.
func (m *Manager) idleExpired(s *session, now time.Time) bool {
	s.mu.Lock()
	settled := s.status == StatusReady && !s.evicting && !s.finished
	s.mu.Unlock()
	if !settled {
		return false
	}
	return s.idleSince(now) >= m.cfg.IdleTimeout
}

// evict wraps one idle session up and stops it: ask for the handoff note, give the turn
// its budget, then stop the child through the ordinary graceful path (stdin close, TERM,
// KILL) so a client sees the same lifecycle as a manual stop.
func (m *Manager) evict(s *session) {
	s.mu.Lock()
	if s.status != StatusReady || s.evicting || s.finished {
		s.mu.Unlock()
		return
	}
	s.evicting = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.evicting = false
		s.mu.Unlock()
	}()

	if m.lifeCtx.Err() != nil {
		return
	}
	if prompt := strings.TrimSpace(m.cfg.WrapUpPrompt); prompt != "" {
		s.logf("idle for %s: asking for a wrap-up note before stopping", m.cfg.IdleTimeout)
		command, err := encodePiCommand(commandTypePrompt, map[string]any{"message": prompt})
		if err != nil {
			s.logf("the wrap-up prompt could not be built: %v", err)
		} else if _, err := s.call(m.lifeCtx, command); err != nil {
			s.logf("the wrap-up prompt did not reach the child: %v", err)
		} else {
			m.waitForWrapUp(s)
		}
	}
	if m.lifeCtx.Err() != nil {
		return
	}
	if err := s.stop(m.lifeCtx); err != nil {
		s.logf("eviction: %v", err)
	}
}

// waitForWrapUp gives a wrap-up turn its budget, or stops waiting as soon as it has been
// quiet for wrapUpSettle: whichever comes first.
func (m *Manager) waitForWrapUp(s *session) {
	deadline := time.Now().Add(m.cfg.WrapUpBudget)
	for time.Now().Before(deadline) {
		if m.lifeCtx.Err() != nil {
			return
		}
		if s.idleSince(time.Now().UTC()) >= wrapUpSettle {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
