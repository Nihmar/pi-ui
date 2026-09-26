package spike

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/rpc"
)

// spawnArgs is the pi command line the measurement launches: RPC mode, no session
// persistence, because the measurement only needs the cheapest ready child
// (docs/spike-interfaces.md §5.6).
var spawnArgs = []string{"--mode", "rpc", "--no-session"}

// SpawnResult is the outcome of MeasureSpawn (docs/spike-interfaces.md §5.6): the
// per-child samples in measurement order, their percentiles and the resident
// memory of every child tree plus the total.
type SpawnResult struct {
	N           int       `json:"n"`
	ReadyMs     []float64 `json:"readyMs"`
	P50Ms       float64   `json:"p50Ms"`
	P95Ms       float64   `json:"p95Ms"`
	ChildRSSMiB []float64 `json:"childRssMiB"`
	TotalRSSMiB float64   `json:"totalRssMiB"`
}

// Summary renders the one-line report cmd/pi-ui measure prints.
func (r SpawnResult) Summary() string {
	return fmt.Sprintf("spawn: n=%d readyMs p50=%.1f p95=%.1f max=%.1f min=%.1f totalRssMiB=%.1f childRssMiB=%s",
		r.N, r.P50Ms, r.P95Ms, maxFloat(r.ReadyMs), minFloat(r.ReadyMs), r.TotalRSSMiB, formatFloats(r.ChildRSSMiB))
}

// MeasureSpawn spawns cfg.Sessions children in parallel, times each from Start to a
// successful get_state, then samples the RSS of every child tree. It is the
// measurement behind acceptance criteria C1 and C4 (and, with the real pi, the
// child side of C2/C3).
//
// The children are closed before the call returns, so a measurement never leaves
// processes behind. Any child that fails to become ready fails the whole call:
// a spawn latency distribution is only useful when every sample is a real ready.
func MeasureSpawn(ctx context.Context, cfg Config) (SpawnResult, error) {
	cfg = cfg.withDefaults()
	if cfg.Sessions < 1 {
		return SpawnResult{}, fmt.Errorf("spike: sessions = %d, want >= 1", cfg.Sessions)
	}
	executable, err := cfg.spawnExecutable()
	if err != nil {
		return SpawnResult{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	root, remove, err := cfg.workRoot("spike-spawn-")
	if err != nil {
		return SpawnResult{}, err
	}
	defer remove()

	type slot struct {
		bridge  rpc.Bridge
		readyMs float64
		err     error
	}
	slots := make([]slot, cfg.Sessions)

	var wg sync.WaitGroup
	for index := 0; index < cfg.Sessions; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			dir, dirErr := sessionDir(root, index)
			if dirErr != nil {
				slots[index].err = dirErr
				return
			}
			bridge, readyMs, spawnErr := spawnAndReady(ctx, cfg, executable, dir, index)
			slots[index] = slot{bridge: bridge, readyMs: readyMs, err: spawnErr}
		}(index)
	}
	wg.Wait()

	// Close every child that started, including the ones whose measurement
	// failed: a failed sample must not leak a process.
	defer func() {
		for _, s := range slots {
			if s.bridge != nil {
				_ = s.bridge.Close()
			}
		}
	}()
	for index, s := range slots {
		if s.err != nil {
			return SpawnResult{}, fmt.Errorf("spike: session %d: %w", index, s.err)
		}
	}

	if err := sleepCtx(ctx, cfg.Settle); err != nil {
		return SpawnResult{}, err
	}

	result := SpawnResult{N: cfg.Sessions}
	for index, s := range slots {
		result.ReadyMs = append(result.ReadyMs, s.readyMs)
		rss, rssErr := ProcessRSSMiB(s.bridge.PID())
		if rssErr != nil {
			return SpawnResult{}, fmt.Errorf("spike: session %d rss: %w", index, rssErr)
		}
		result.ChildRSSMiB = append(result.ChildRSSMiB, rss)
		result.TotalRSSMiB += rss
	}
	result.P50Ms = percentileOf(result.ReadyMs, 50)
	result.P95Ms = percentileOf(result.ReadyMs, 95)

	raw := struct {
		Machine Machine        `json:"machine"`
		Config  configSnapshot `json:"config"`
		Result  SpawnResult    `json:"result"`
	}{
		Machine: machineOrZero(),
		Config:  cfg.snapshot(),
		Result:  result,
	}
	if err := writeRaw(cfg.RawDir, "spawn.json", raw); err != nil {
		return result, err
	}
	return result, nil
}

// spawnAndReady starts one child and measures until get_state answers. The bridge
// is returned even on failure, so the caller can close it.
func spawnAndReady(ctx context.Context, cfg Config, executable, dir string, index int) (rpc.Bridge, float64, error) {
	command := append([]string{executable}, spawnArgs...)
	bridge := rpc.New(rpc.Spec{Command: command, Dir: dir}, rpc.Options{})

	start := time.Now()
	if err := bridge.Start(ctx); err != nil {
		return nil, 0, fmt.Errorf("spawn: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, cfg.ReadyTimeout)
	defer cancel()
	raw, err := bridge.Send(waitCtx, fmt.Sprintf("spike-spawn-%d", index), json.RawMessage(`{"type":"get_state"}`))
	if err != nil {
		return bridge, 0, fmt.Errorf("get_state: %w", err)
	}
	var response struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return bridge, 0, fmt.Errorf("get_state response: %w", err)
	}
	if !response.Success {
		return bridge, 0, fmt.Errorf("get_state rejected: %s", response.Error)
	}
	return bridge, float64(time.Since(start)) / float64(time.Millisecond), nil
}

// machineOrZero returns the host facts or a zero Machine, so raw output degrades
// instead of failing a measurement that succeeded.
func machineOrZero() Machine {
	machine, err := CurrentMachine()
	if err != nil {
		return Machine{}
	}
	return machine
}

// sleepCtx waits for d, returning early when ctx is done instead of holding a
// cancelled measurement.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// formatFloats renders a sample slice compactly for one-line summaries.
func formatFloats(samples []float64) string {
	if len(samples) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(samples))
	for _, sample := range samples {
		parts = append(parts, fmt.Sprintf("%.1f", sample))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// minFloat returns the smallest sample, or 0 for an empty slice.
func minFloat(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	best := samples[0]
	for _, sample := range samples[1:] {
		if sample < best {
			best = sample
		}
	}
	return best
}
