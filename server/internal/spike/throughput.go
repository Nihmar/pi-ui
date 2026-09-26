package spike

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// RSSSample is one reading of the measuring process's own resident memory. The
// server side of the throughput pipeline runs in this process, so this is the
// Go server RSS of acceptance criteria C2/C3/C9.
type RSSSample struct {
	ElapsedSec float64 `json:"elapsedSec"`
	MiB        float64 `json:"mib"`
}

// ThroughputResult is the outcome of MeasureThroughput (docs/spike-interfaces.md
// §5.6). The exported fields are the frozen contract; the raw samples behind them
// stay reachable through the accessors and are written to Config.RawDir.
type ThroughputResult struct {
	Events       int     `json:"events"`
	DurationSec  float64 `json:"durationSec"`
	EventsPerSec float64 `json:"eventsPerSec"`
	Loss         int     `json:"loss"`
	P50Ms        float64 `json:"p50Ms"`
	P95Ms        float64 `json:"p95Ms"`

	sessions     int
	clients      int
	samplesMs    []float64
	samplesTaken int
	clientCounts []int
	rssSeries    []RSSSample
	rssWarmup    float64
	rssFinal     float64
	rssGrowthPct float64
}

// RawLatencyMs returns a copy of the retained end-to-end samples of the first client,
// in arrival order. A long run retains a bounded reservoir window rather than every
// sample: the percentiles are computed from that window, and LatencySamplesTaken reports
// how many samples it stands for.
func (r ThroughputResult) RawLatencyMs() []float64 {
	return append([]float64(nil), r.samplesMs...)
}

// LatencySamplesTaken is the number of end-to-end samples the run actually took, which
// exceeds len(RawLatencyMs()) once the reservoir window is full.
func (r ThroughputResult) LatencySamplesTaken() int {
	return r.samplesTaken
}

// ClientEvents returns the pi.* event count each client received, in connection
// order. Fan-out correctness is the property that all counts are equal.
func (r ThroughputResult) ClientEvents() []int {
	return append([]int(nil), r.clientCounts...)
}

// RSSSeries returns the resident-memory samples of the measuring process.
func (r ThroughputResult) RSSSeries() []RSSSample {
	return append([]RSSSample(nil), r.rssSeries...)
}

// RSSGrowthPercent is the growth from the first post-warm-up sample to the last
// (acceptance criterion C9).
func (r ThroughputResult) RSSGrowthPercent() float64 { return r.rssGrowthPct }

// RSSWarmupMiB is the baseline of the growth calculation.
func (r ThroughputResult) RSSWarmupMiB() float64 { return r.rssWarmup }

// RSSFinalMiB is the last RSS sample.
func (r ThroughputResult) RSSFinalMiB() float64 { return r.rssFinal }

// Summary renders the one-line report cmd/pi-ui measure prints.
func (r ThroughputResult) Summary() string {
	return fmt.Sprintf("throughput: sessions=%d clients=%d events=%d durationSec=%.2f eventsPerSec=%.1f loss=%d p50Ms=%.2f p95Ms=%.2f rssWarmupMiB=%.1f rssFinalMiB=%.1f rssGrowthPct=%.2f",
		r.sessions, r.clients, r.Events, r.DurationSec, r.EventsPerSec, r.Loss, r.P50Ms, r.P95Ms,
		r.rssWarmup, r.rssFinal, r.rssGrowthPct)
}

// MeasureThroughput drives cfg.Sessions fake-pi children with `--emit cfg.Events
// --rate cfg.Rate` through the real rpc → sessions → ws pipeline and measures what
// cfg.Clients WebSocket subscribers receive. It is the measurement behind
// acceptance criteria C5, C6 and C9.
//
// The result is returned even when the run failed to settle or lost events, because
// the raw samples are the evidence; the error says what went wrong.
func MeasureThroughput(ctx context.Context, cfg Config) (ThroughputResult, error) {
	cfg = cfg.withDefaults()
	if cfg.Sessions < 1 {
		return ThroughputResult{}, fmt.Errorf("spike: sessions = %d, want >= 1", cfg.Sessions)
	}
	if cfg.Events < 1 {
		return ThroughputResult{}, fmt.Errorf("spike: events = %d, want >= 1", cfg.Events)
	}
	if cfg.Clients < 1 {
		return ThroughputResult{}, fmt.Errorf("spike: clients = %d, want >= 1", cfg.Clients)
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	h, err := newHarness(ctx, cfg, true)
	if err != nil {
		return ThroughputResult{}, err
	}
	defer h.shutdown()

	// Every child streams cfg.Events message_update records per prompt, then
	// agent_end and agent_settled: one pi.* event per record.
	expectedPerClient := cfg.Sessions * (cfg.Events + 2)
	clients, clientCancel, err := h.connectClients(ctx, cfg, expectedPerClient)
	if err != nil {
		return ThroughputResult{}, err
	}
	defer closeClients(clients, clientCancel)

	sampler := newRSSSampler(cfg)
	defer sampler.stop()

	start := time.Now()
	if err := dispatchPrompts(ctx, clients[0], h.ids); err != nil {
		return ThroughputResult{}, err
	}
	settled := waitForClients(ctx, clients)
	duration := time.Since(start)
	sampler.finish()

	series, warmup, final := sampler.snapshot(cfg.Warmup)
	result := buildThroughputResult(cfg, clients, expectedPerClient, duration, series, warmup, final)

	raw := struct {
		Machine      Machine        `json:"machine"`
		Config       configSnapshot `json:"config"`
		Result       rawThroughput  `json:"result"`
		LatencyMs    []float64      `json:"latencyMs"`
		ClientEvents []int          `json:"clientEvents"`
		RSS          []RSSSample    `json:"rss"`
	}{
		Machine:      machineOrZero(),
		Config:       cfg.snapshot(),
		Result:       result.raw(),
		LatencyMs:    result.RawLatencyMs(),
		ClientEvents: result.ClientEvents(),
		RSS:          result.RSSSeries(),
	}
	if err := writeRaw(cfg.RawDir, "throughput.json", raw); err != nil {
		return result, err
	}

	if !settled {
		return result, fmt.Errorf("spike: throughput did not settle within %s: %d of %d events arrived (client errors: %s)",
			cfg.Timeout, result.Events, expectedPerClient, clientErrors(clients))
	}
	if result.Loss > 0 {
		return result, fmt.Errorf("spike: throughput lost %d of %d events (client counts %v)",
			result.Loss, expectedPerClient, result.clientCounts)
	}
	return result, nil
}

// dispatchPrompts starts one streaming turn per session. The first client drives
// the commands; every connected client sees the resulting events.
func dispatchPrompts(ctx context.Context, c *client, ids []string) error {
	for index, id := range ids {
		frame := fmt.Sprintf(`{"type":"command","id":"spike-cmd-%d","sessionId":%q,"op":"session.prompt","payload":{"message":"spike"}}`,
			index, id)
		if err := c.write(ctx, frame); err != nil {
			return err
		}
	}
	return nil
}

// waitForClients reports whether every client received its expected event count
// before the context ended.
func waitForClients(ctx context.Context, clients []*client) bool {
	for _, c := range clients {
		select {
		case <-c.complete:
		case <-ctx.Done():
			return false
		}
	}
	return true
}

// buildThroughputResult folds the client counters and the RSS series into one
// result. Loss is measured against the client that saw the fewest events, so a
// loss on any subscriber is visible even when the primary client was fine.
func buildThroughputResult(cfg Config, clients []*client, expected int, duration time.Duration,
	series []RSSSample, warmup, final float64) ThroughputResult {
	result := ThroughputResult{
		sessions:  cfg.Sessions,
		clients:   cfg.Clients,
		rssSeries: series,
		rssWarmup: warmup,
		rssFinal:  final,
	}
	minCount := -1
	for _, c := range clients {
		count, latencies, observed := c.counts()
		result.clientCounts = append(result.clientCounts, count)
		if minCount < 0 || count < minCount {
			minCount = count
		}
		if c.index == 0 {
			result.Events = count
			result.samplesMs = latencies
			result.samplesTaken = observed
		}
	}
	if minCount < 0 {
		minCount = 0
	}
	if loss := expected - minCount; loss > 0 {
		result.Loss = loss
	}
	result.DurationSec = duration.Seconds()
	if duration > 0 {
		result.EventsPerSec = float64(result.Events) / duration.Seconds()
	}
	result.P50Ms = percentileOf(result.samplesMs, 50)
	result.P95Ms = percentileOf(result.samplesMs, 95)
	if warmup > 0 {
		result.rssGrowthPct = (final - warmup) / warmup * 100
	}
	return result
}

// rawThroughput is the flat, JSON-friendly view of a throughput result; the
// latency samples and the RSS series travel as their own arrays.
type rawThroughput struct {
	Sessions        int     `json:"sessions"`
	Clients         int     `json:"clients"`
	Events          int     `json:"events"`
	DurationSec     float64 `json:"durationSec"`
	EventsPerSec    float64 `json:"eventsPerSec"`
	Loss            int     `json:"loss"`
	P50Ms           float64 `json:"p50Ms"`
	P95Ms           float64 `json:"p95Ms"`
	LatencyRetained int     `json:"latencyRetained"`
	LatencyTaken    int     `json:"latencyTaken"`
	RSSWarmupMiB    float64 `json:"rssWarmupMiB"`
	RSSFinalMiB     float64 `json:"rssFinalMiB"`
	RSSGrowthPct    float64 `json:"rssGrowthPct"`
}

// raw returns the flat view for raw output.
func (r ThroughputResult) raw() rawThroughput {
	return rawThroughput{
		Sessions:        r.sessions,
		Clients:         r.clients,
		Events:          r.Events,
		DurationSec:     r.DurationSec,
		EventsPerSec:    r.EventsPerSec,
		Loss:            r.Loss,
		P50Ms:           r.P50Ms,
		P95Ms:           r.P95Ms,
		LatencyRetained: len(r.samplesMs),
		LatencyTaken:    r.samplesTaken,
		RSSWarmupMiB:    r.rssWarmup,
		RSSFinalMiB:     r.rssFinal,
		RSSGrowthPct:    r.rssGrowthPct,
	}
}

// clientErrors summarises the reader errors of every client for the failure
// message of a run that did not settle.
func clientErrors(clients []*client) string {
	var messages []string
	for _, c := range clients {
		if err := c.readErr(); err != nil {
			messages = append(messages, err.Error())
		}
	}
	if len(messages) == 0 {
		return "none"
	}
	return strings.Join(messages, "; ")
}

// rssSampler samples the measuring process's own RSS while a throughput run is in
// flight: a background goroutine takes a reading on every interval, plus one at
// the start and one at the end, so even a short run has a series.
type rssSampler struct {
	mu      sync.Mutex
	start   time.Time
	samples []RSSSample
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// newRSSSampler starts sampling until stop is called.
func newRSSSampler(cfg Config) *rssSampler {
	s := &rssSampler{
		start:  time.Now(),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	s.sample()
	go func() {
		defer close(s.doneCh)
		ticker := time.NewTicker(cfg.RSSInterval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.sample()
			}
		}
	}()
	return s
}

// sample records one reading; a platform without /proc simply contributes none.
func (s *rssSampler) sample() {
	mib, err := selfRSSMiB()
	if err != nil {
		return
	}
	s.mu.Lock()
	s.samples = append(s.samples, RSSSample{ElapsedSec: time.Since(s.start).Seconds(), MiB: mib})
	s.mu.Unlock()
}

// finish takes the closing reading of the run.
func (s *rssSampler) finish() { s.sample() }

// stop ends the sampling goroutine.
func (s *rssSampler) stop() {
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	<-s.doneCh
}

// snapshot returns the series and the warm-up/final pair of the growth
// calculation: the first sample at or after the warm-up window, and the last.
func (s *rssSampler) snapshot(warmup time.Duration) (series []RSSSample, warm, final float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	series = append([]RSSSample(nil), s.samples...)
	if len(series) == 0 {
		return series, 0, 0
	}
	warm = series[0].MiB
	for _, sample := range series {
		if sample.ElapsedSec >= warmup.Seconds() {
			warm = sample.MiB
			break
		}
	}
	return series, warm, series[len(series)-1].MiB
}
