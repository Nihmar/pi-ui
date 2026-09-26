package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/spike"
)

// measureSlack is the fixed budget a derived deadline adds on top of the stream
// itself: spawning the children, connecting the clients, reaping everything and
// ordinary scheduler jitter.
const measureSlack = 2 * time.Minute

// init registers the measurement command, so cmd/pi-ui and the CLI tests see it
// without a branch in any dispatcher.
func init() {
	Register(Command{
		Name:    "measure",
		Summary: "run a Phase 1 acceptance measurement (spawn, server RSS, throughput)",
		Run:     runMeasure,
	})
}

// measureConfig is one resolved `pi-ui measure` invocation. The measurement
// flags map one-to-one onto spike.Config, and an unset number stays zero so the
// defaults live in exactly one place: the exported spike.Default* constants
// that server/scripts/measure.sh and the usage text also quote.
type measureConfig struct {
	pi       string
	fakePi   string
	sessions int
	events   int
	rate     float64
	clients  int
	out      string
}

// runMeasure is the Run function of `pi-ui measure` (docs/spike-interfaces.md
// §5.6/§5.7). It runs one acceptance measurement against the real pi
// (--pi: criteria C1/C4) or against the deterministic harness (--fake-pi:
// C2/C3 first, then C5/C6/C9), prints every summary on stdout and — with --out —
// writes the same summaries to that file and the raw JSON samples to its
// directory, which is the contract server/scripts/measure.sh relies on.
//
// A cancelled context (SIGINT/SIGTERM) stops the measurement; every child is
// reaped by the measurement itself, so an interrupted run leaves no process
// behind.
func runMeasure(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cfg, err := parseMeasureConfig(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			// --help is a request, not a failure: the usage text is already on stderr.
			return nil
		}
		return err
	}

	measurement := spike.Config{
		Sessions: cfg.sessions,
		Events:   cfg.events,
		Rate:     cfg.rate,
		Clients:  cfg.clients,
		// The deadline is derived from the request, so the three minute C9 soak
		// is never cut off by the five minute default (see measureTimeout).
		Timeout: measureTimeout(cfg),
	}
	if cfg.out != "" {
		// The script reads <out>/<NAME>/summary.txt and expects the raw sample
		// files next to it; without --out nothing is written.
		measurement.RawDir = filepath.Dir(cfg.out)
	}

	var summaries []string
	emit := func(summary string) {
		summaries = append(summaries, summary)
		fmt.Fprintln(stdout, summary)
	}

	if cfg.pi != "" {
		measurement.Pi = cfg.pi
		result, err := spike.MeasureSpawn(ctx, measurement)
		if err != nil {
			return err
		}
		emit(result.Summary())
	} else {
		measurement.FakePi = cfg.fakePi
		idle, err := spike.MeasureServerRSS(ctx, measurement)
		if err != nil {
			return err
		}
		emit(idle.Summary())

		throughput, err := spike.MeasureThroughput(ctx, measurement)
		if err != nil {
			return err
		}
		emit(throughput.Summary())
	}

	if cfg.out != "" {
		if err := writeSummaries(cfg.out, summaries); err != nil {
			return err
		}
	}
	return nil
}

// parseMeasureConfig resolves the command line. The numbers default to zero on
// purpose: the spike package turns a zero into its exported default, so the
// CLI, the usage text and the measurement script cannot drift apart.
func parseMeasureConfig(args []string, stderr io.Writer) (measureConfig, error) {
	fs := flag.NewFlagSet("measure", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { writeMeasureUsage(stderr) }

	pi := fs.String("pi", "", "real pi executable to measure (criteria C1/C4)")
	fakePi := fs.String("fake-pi", "", "fake-pi harness binary to measure (criteria C2/C3, C5/C6/C9)")
	sessions := fs.Int("sessions", 0, "concurrent children (default "+fmt.Sprint(spike.DefaultSessions)+")")
	events := fs.Int("events", 0, "message_update records each child streams per prompt (default "+fmt.Sprint(spike.DefaultEvents)+")")
	rate := fs.Float64("rate", 0, "per-child event rate in events/s, 0 = unpaced")
	clients := fs.Int("clients", 0, "WebSocket subscribers, each subscribed to every session (default "+fmt.Sprint(spike.DefaultClients)+")")
	out := fs.String("out", "", "write the summaries here and the raw JSON samples next to it")

	if err := fs.Parse(args); err != nil {
		return measureConfig{}, err
	}
	if fs.NArg() > 0 {
		return measureConfig{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	cfg := measureConfig{
		pi:       *pi,
		fakePi:   *fakePi,
		sessions: *sessions,
		events:   *events,
		rate:     *rate,
		clients:  *clients,
		out:      *out,
	}

	switch {
	case cfg.pi == "" && cfg.fakePi == "":
		writeMeasureUsage(stderr)
		return measureConfig{}, errors.New("one of --pi or --fake-pi is required")
	case cfg.pi != "" && cfg.fakePi != "":
		writeMeasureUsage(stderr)
		return measureConfig{}, errors.New("--pi and --fake-pi are mutually exclusive")
	}
	if cfg.sessions < 0 || cfg.events < 0 || cfg.clients < 0 {
		return measureConfig{}, errors.New("--sessions, --events and --clients must not be negative")
	}
	if cfg.rate < 0 || math.IsNaN(cfg.rate) || math.IsInf(cfg.rate, 0) {
		return measureConfig{}, fmt.Errorf("--rate: %v is not a finite rate in events/s", cfg.rate)
	}
	return cfg, nil
}

// measureTimeout derives the whole-call deadline from the requested run instead
// of relying on spike.DefaultTimeout alone: a paced run lasts events/rate
// seconds per child (the children stream in parallel) plus the warm-up window
// that criterion C9 excludes from its growth calculation, plus the fixed slack.
// The C9 soak — 360000 events at 2000/s, i.e. three minutes of streaming plus a
// 30s warm-up — is longer than the five minute default, so a hard-coded default
// would cut it off mid-stream. Short runs keep spike.DefaultTimeout.
func measureTimeout(cfg measureConfig) time.Duration {
	if cfg.rate <= 0 {
		// Unpaced: the stream is as fast as the pipeline allows, so only the
		// generous default bounds run-to-run variance.
		return spike.DefaultTimeout
	}
	events := cfg.events
	if events <= 0 {
		events = spike.DefaultEvents
	}
	paced := time.Duration(float64(events) / cfg.rate * float64(time.Second))
	if derived := paced + spike.DefaultWarmup + measureSlack; derived > spike.DefaultTimeout {
		return derived
	}
	return spike.DefaultTimeout
}

// writeSummaries lands one run's summaries in one file: exactly the lines that
// were printed, so the file is what a report quotes. The parent directory is
// created, because the script passes <out>/<NAME>/summary.txt before that
// directory exists.
func writeSummaries(path string, summaries []string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("--out %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(summaries, "\n")+"\n"), 0o644); err != nil {
		return fmt.Errorf("--out %s: %w", path, err)
	}
	return nil
}

// writeMeasureUsage prints the command's own flags and the shape of the
// deadline. The defaults are interpolated from the spike constants, so the help
// text cannot advertise a number the measurement does not use.
func writeMeasureUsage(w io.Writer) {
	fmt.Fprintf(w, `usage: pi-ui measure [--sessions %d] (--pi <path> | --fake-pi <path>) [--events %d] [--rate N] [--clients %d] [--out <file>]

--pi measures the real pi: spawn → get_state readiness and per-child RSS
(acceptance criteria C1/C4). --fake-pi measures the deterministic harness: idle
server RSS (C2/C3), then a paced throughput run with end-to-end latency and RSS
growth (C5/C6/C9). One of the two is required; --events, --rate and --clients
only shape the throughput run.

flags:
  --pi <path>        real pi executable to measure
  --fake-pi <path>   fake-pi harness binary (build it with: go build ./test/fake-pi)
  --sessions N       concurrent children (default %d)
  --events N         message_update records each child streams per prompt (default %d)
  --rate N           per-child event rate in events/s, 0 = unpaced (default 0)
  --clients N        WebSocket subscribers, each subscribed to every session (default %d)
  --out <file>       write the summaries to <file> and the raw JSON samples to its
                     directory; without it no file is written (default: none)

The whole-call deadline is derived from the request: events/rate seconds of
streaming plus the %s warm-up plus %s of slack, and never below the %s default,
so a longer soak is not cut off mid-stream.
`, spike.DefaultSessions, spike.DefaultEvents, spike.DefaultClients,
		spike.DefaultSessions, spike.DefaultEvents, spike.DefaultClients,
		spike.DefaultWarmup, measureSlack, spike.DefaultTimeout)
}
