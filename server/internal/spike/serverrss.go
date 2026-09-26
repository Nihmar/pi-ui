package spike

import (
	"context"
	"fmt"
	"math"
	"os"
)

// ServerRSSResult is the outcome of MeasureServerRSS: the resident memory of the
// Go server process while it is idle with cfg.Sessions live children and
// cfg.Clients WebSocket subscribers. It is the measurement behind acceptance
// criteria C2 and C3.
type ServerRSSResult struct {
	Sessions       int       `json:"sessions"`
	Clients        int       `json:"clients"`
	ServerRSSMiB   float64   `json:"serverRssMiB"`   // the measuring process alone
	ChildrenRSSMiB float64   `json:"childrenRssMiB"` // sum of the child trees
	ChildRSSMiB    []float64 `json:"childRssMiB"`
	TotalRSSMiB    float64   `json:"totalRssMiB"` // the process tree, descendants included
}

// Summary renders the one-line report cmd/pi-ui measure prints.
func (r ServerRSSResult) Summary() string {
	return fmt.Sprintf("server-rss: sessions=%d clients=%d serverMiB=%.1f childrenMiB=%.1f totalMiB=%.1f",
		r.Sessions, r.Clients, r.ServerRSSMiB, r.ChildrenRSSMiB, r.TotalRSSMiB)
}

// MeasureServerRSS starts the in-process server (hub + sessions + API) with
// cfg.Sessions fake-pi children and cfg.Clients subscribers, waits for the idle
// state, and reads three numbers: the server's own RSS without descendants, the
// sum of the child process trees, and the whole tree through ProcessRSSMiB.
//
// The children are fake-pi on purpose: C2/C3 bound the server's footprint, and
// the child kind must not be able to move a number about the server. The child
// RSS of the real pi is measured separately by MeasureSpawn (C4).
func MeasureServerRSS(ctx context.Context, cfg Config) (ServerRSSResult, error) {
	cfg = cfg.withDefaults()
	if cfg.Sessions < 1 {
		return ServerRSSResult{}, fmt.Errorf("spike: sessions = %d, want >= 1", cfg.Sessions)
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	h, err := newHarness(ctx, cfg, false)
	if err != nil {
		return ServerRSSResult{}, err
	}
	defer h.shutdown()

	// The clients only need to be connected and subscribed; no prompt is ever
	// sent, so the expected count is never reached and only the barrier matters.
	clients, clientCancel, err := h.connectClients(ctx, cfg, math.MaxInt)
	if err != nil {
		return ServerRSSResult{}, err
	}
	defer closeClients(clients, clientCancel)

	if err := sleepCtx(ctx, cfg.Settle); err != nil {
		return ServerRSSResult{}, err
	}

	result := ServerRSSResult{Sessions: cfg.Sessions, Clients: cfg.Clients}
	result.ServerRSSMiB, err = selfRSSMiB()
	if err != nil {
		return ServerRSSResult{}, err
	}
	for index, pid := range h.pids {
		rss, rssErr := ProcessRSSMiB(pid)
		if rssErr != nil {
			return ServerRSSResult{}, fmt.Errorf("spike: child %d rss: %w", index, rssErr)
		}
		result.ChildRSSMiB = append(result.ChildRSSMiB, rss)
		result.ChildrenRSSMiB += rss
	}
	result.TotalRSSMiB, err = ProcessRSSMiB(os.Getpid())
	if err != nil {
		return ServerRSSResult{}, err
	}

	raw := struct {
		Machine Machine         `json:"machine"`
		Config  configSnapshot  `json:"config"`
		Result  ServerRSSResult `json:"result"`
	}{
		Machine: machineOrZero(),
		Config:  cfg.snapshot(),
		Result:  result,
	}
	if err := writeRaw(cfg.RawDir, "server-rss.json", raw); err != nil {
		return result, err
	}
	return result, nil
}
