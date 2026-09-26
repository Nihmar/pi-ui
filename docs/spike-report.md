# Phase 1 spike — acceptance measurement report

**Owner:** measure workstream (`docs/spike-interfaces.md` §13).
**Runner:** `server/scripts/measure.sh`, a wrapper around `pi-ui measure`
(`server/internal/cli/measure.go`) and `internal/spike` (`docs/spike-interfaces.md` §5.6).
**Tree measured:** HEAD `a0c64e0` plus the harness fixes the run was built with, now in
history: `df82ca0` (deadline pacer in `server/test/fake-pi/emit.go`), `6e4bbd8` (latency
reservoir window) and the runner as of `0194bd4` (`pi-ui measure` plus the paced rapid run).
**Host measured (one machine, all runs):** 13th Gen Intel Core i5-13400F, 12 cores,
31 925 MiB RAM, kernel `7.2.7-1-cachyos`; `pi` 0.87.1 at `~/.local/bin/pi`. Host load
average 2.68 before the run and 2.12 after; the host is shared with other agent work, so
the absolute latency numbers are load-sensitive (see §4).
**Measured:** 2026-09-26.

This report records the Phase 1 acceptance matrix C1–C9 of `docs/spike-interfaces.md` §12
as it was measured, together with the raw samples behind every number. It is deliberately
narrow: each value below is produced by the runner named here, on the tree and host above,
and the raw sample file next to it carries the individual samples. C7 and C8 are not
measurements — they are verified by the adversarial suite the runner executes first (§2,
`docs/verification-report.md` §2/§5).

## 1. How to reproduce

The whole matrix with one command:

```bash
server/scripts/measure.sh
```

The runner builds the static `pi-ui` binary and the `fake-pi` harness into the output
directory, runs `go test -race ./test/adversarial/... ./test/e2e/...` (the C7 and C8
checks), then runs one `pi-ui measure` per criterion and concatenates the summaries. Its
knobs are the `SPIKE_*` environment variables listed at the top of the script
(`SPIKE_SESSIONS`, `SPIKE_RSS_2`, `SPIKE_RSS_8`, `SPIKE_RAPID_*`, `SPIKE_SOAK_*`,
`SPIKE_SKIP_SOAK`), and `PI_BIN` selects the real pi for the C1/C4 run.

One criterion at a time. Build the two binaries first, then run the measurement that
carries it; a `--fake-pi` invocation runs the idle RSS measurement and the throughput
measurement in one call, so read the `server-rss:` line for C2/C3 and the `throughput:`
line for C5/C6/C9:

```bash
out=.piui/spike
( cd server \
  && CGO_ENABLED=0 go build -trimpath -o "$OLDPWD/$out/pi-ui" ./cmd/pi-ui \
  && go build -trimpath -o "$OLDPWD/$out/fake-pi" ./test/fake-pi )

# C7/C8 — the framing and lifecycle suites, run first by the runner.
( cd server && go test -race ./test/adversarial/... ./test/e2e/... )

# C1/C4 — real pi spawn latency and per-child RSS, 8 children in parallel.
"$out/pi-ui" measure --pi "$HOME/.local/bin/pi" --sessions 8 \
  --out "$out/real-pi-spawn/summary.txt"

# C2 — idle server RSS, 2 sessions + 1 WS client (the throughput pass of this
# invocation is a 1-event smoke run and is ignored for the criterion).
"$out/pi-ui" measure --fake-pi "$out/fake-pi" --sessions 2 --clients 1 --events 1 --rate 0 \
  --out "$out/server-rss-2/summary.txt"

# C3 — idle server RSS, 8 sessions.
"$out/pi-ui" measure --fake-pi "$out/fake-pi" --sessions 8 --clients 1 --events 1 --rate 0 \
  --out "$out/server-rss-8/summary.txt"

# C5/C6 — 30 s sustained rapid stream, one client.
"$out/pi-ui" measure --fake-pi "$out/fake-pi" --sessions 1 --clients 1 \
  --events 210000 --rate 7000 --out "$out/throughput-rapid/summary.txt"

# C9 — 3 minute soak at ~2 000 events/s.
"$out/pi-ui" measure --fake-pi "$out/fake-pi" --sessions 1 --clients 1 \
  --events 360000 --rate 2000 --out "$out/throughput-soak/summary.txt"
```

The headroom probes of §2 are separate, ad-hoc runs and are **not** part of
`measure.sh`:

```bash
"$out/pi-ui" measure --fake-pi "$out/fake-pi" --sessions 1 --clients 1 \
  --events 300000 --rate 10000 --out "$out/headroom-10k/summary.txt"
"$out/pi-ui" measure --fake-pi "$out/fake-pi" --sessions 1 --clients 1 \
  --events 450000 --rate 15000 --out "$out/headroom-15k/summary.txt"
```

Raw samples: every run writes `<out>/<name>/summary.txt` and, next to it, the raw JSON of
that measurement — `spawn.json`, `server-rss.json` or `throughput.json` — with every
individual sample (`readyMs`, `childRssMiB`, the `latencyMs` window, the `rss` series,
`latencyRetained`/`latencyTaken`). The runner concatenates all summaries into
`.piui/spike/summary.txt` and writes its log to `.piui/spike/measure.log`. `.piui/` is
gitignored (`.gitignore`), and the runner refuses to start unless its output directory is
ignored, so raw samples never enter a commit.

## 2. Acceptance matrix

All sample paths are relative to `.piui/spike/`.

| # | Criterion | Threshold | Measured | Result | Raw sample |
|---|---|---|---|---|---|
| C1 | real `pi` child spawn → `get_state` ready, 8 samples in parallel | p95 ≤ 5 000 ms | p50 547.0 ms, p95 584.4 ms | PASS | `real-pi-spawn/spawn.json` |
| C2 | Go server RSS, idle + 2 sessions + 1 WS client | ≤ 128 MiB | 16.9 MiB (children 11.4 MiB, whole tree 29.1 MiB) | PASS | `server-rss-2/server-rss.json` |
| C3 | Go server RSS, 8 sessions | ≤ 192 MiB | 19.5 MiB (children 49.6 MiB, tree 75.6 MiB) | PASS | `server-rss-8/server-rss.json` |
| C4 | per-child RSS, real `pi`, idle | sum of 8 ≤ 2 400 MiB | 1 136.4 MiB (≈142 MiB/child) | PASS | `real-pi-spawn/spawn.json` |
| C5 | fake-pi → rpc → sessions → hub → 1 WS client throughput | ≥ 5 000 events/s sustained 30 s, loss 0 | 210 002 events in 30.00 s = 6 999.9 events/s, loss 0 | PASS | `throughput-rapid/throughput.json` |
| C6 | end-to-end event latency under load (p95) | p95 ≤ 300 ms | rapid p50 0.10 ms, p95 0.34 ms; 3-minute soak p50 0.13 ms, p95 0.34 ms, max 25.6 ms | PASS | `throughput-rapid/throughput.json`, `throughput-soak/throughput.json` |
| C7 | framing correctness: U+2028/U+2029, CRLF, 8 MiB record, split reads | 100 % of records intact, no panic | adversarial framing + pipeline suite green (31 PASS, 0 FAIL, race on, at the measured tree) | PASS | `server/test/adversarial` (run first by the runner) |
| C8 | shutdown reaps all children | ≤ 2 s, zero orphans | adversarial lifecycle suite green; `TestServerSigtermReapsChildren` (e2e, same tree) exits in 9.76 ms with 2 of 2 children reaped, zero orphans | PASS | `server/test/e2e` + `server/test/adversarial` (real binary, SIGTERM) |
| C9 | memory stability: 3 min soak at ~2 000 events/s | Go RSS growth ≤ 10 % after warm-up | 360 002 events in 180.00 s = 2 000.0 events/s, loss 0; RSS 20.6 MiB after warm-up → 20.2 MiB at the end = −2.10 % | PASS | `throughput-soak/throughput.json` |

### Raw samples behind the matrix

| Criterion | Individual samples |
|---|---|
| C1 ready latency (ms) | 535.3, 578.5, 536.0, 577.4, 584.4, 547.0, 535.2, 567.1 |
| C4 per-child RSS (MiB) | 142.7, 141.1, 141.0, 142.0, 140.7, 142.7, 145.6, 140.6 (sum 1 136.4) |
| C6 latency window | rapid: 65 536 samples retained out of 210 000 taken; soak: 65 536 retained out of 360 000 taken |
| C9 RSS series (MiB) | flat between 19.9 and 21.1 for the whole run (readings every 5 s, 20.6 after warm-up → 20.2 at the end) |

**Why the source is paced at 7 000/s.** A source paced at exactly 5 000 events/s can only
ever measure at or below 5 000 events/s, so it could never show the pipeline meeting the
threshold; the rapid run therefore paces the source 40 % above it (7 000/s for 30 s). On
the same host, as separate ad-hoc runs outside `measure.sh` (samples in `.piui/diag/headroom-10k/`
and `.piui/diag/headroom-15k/`), a source at 10 000/s sustained 9 999.7 events/s and a source
at 15 000/s sustained 14 999.9 events/s for 30 s each, loss 0, so the 7 000/s run measures
the pipeline, not the source.

## 3. Measurement defects found and fixed

Two defects had to be fixed before the matrix could be trusted. Both were in the measuring
apparatus, not in the server pipeline, and either would have failed a criterion as a
property of the harness.

### 3.1 The paced source could not exceed the host's timer granularity

`fake-pi` metered its synthetic `--emit` stream with a `time.Ticker`
(`server/test/fake-pi/emit.go`). A ticker fires on the host's timer granularity and never
makes up a missed tick, so an interval finer than that granularity caps the delivered rate
well below the request. Measured on this host with single 3 s samples: a 200 µs ticker fires
≈1 000 times/s, a 500 µs ticker ≈960/s, and even a 1 ms ticker only ≈950/s. The consequence was visible on the wire: a
5 000 events/s run produced 1 031.9 events/s end-to-end (pre-fix summary in
`.piui/spike/pre-fix-summaries.txt`), so C5 would have "failed" as a property of the
harness.

Fixed by emitting on a deadline schedule instead of a ticker: record `i` is due at
`start + i/rate`, and the loop sleeps only while it is ahead of that deadline, so the
records it fell behind on are written back to back and the sustained rate is the requested
one (the `pacer` in `server/test/fake-pi/emit.go`). Pinned by
`TestSyntheticPromptStreamCatchesUp` (2 000 records at 4 000/s — a 250 µs interval — must
finish within 1 s instead of the ~2 s a ticker would take) and by
`TestNewPacerRejectsUnusableRates`, a unit test of the pacer's edge cases (rate ≤ 0, NaN,
±Inf, an interval that rounds to zero, and an already-elapsed deadline). After the fix the
C5 run sustained 6 999.9 events/s (§2).

### 3.2 The harness retained one latency sample per event inside the process C9 measures

The measuring WebSocket client kept one `float64` end-to-end sample per event, inside the
same process whose RSS criterion C9 measures: 360 000 samples cost ≈10.2 MiB of RSS on
this host, more than the entire growth the first (pre-fix) soak reported (+43.98 %, a
FAIL). The harness was measuring its own sample buffer.

Fixed by reservoir-sampling the samples into a fixed 65 536-sample window (Vitter's
algorithm R — `latencyLimit` and `client.addLatency` in `server/internal/spike/harness.go`),
so every sample of the run is equally likely to be in the window, the percentiles stay
unbiased, and the retained memory stops growing with the event count. The raw file records
both numbers: `latencyRetained` / `latencyTaken` are 65 536 retained out of 360 000 taken
for the soak. The first number was a measurement artefact, not a leak; the evidence is the
same run measured twice — +43.98 % before the fix, −2.10 % after it (the pre-fix summary is
kept in `.piui/spike/pre-fix-summaries.txt`; its raw `throughput.json` was overwritten by
the fixed run) — together with the RSS series, which is flat between 19.9 and 21.1 MiB for
the whole run (§2). The retained diagnostic runs under `.piui/diag/` show the same mechanism
from another angle: the same 60 s stream grows RSS by 0.75 MiB with one subscriber and
2.14 MiB with two (`soak-60s-1c/`, `soak-60s-2c/`), i.e. with the number of in-process
sample accumulators, not with the server's event handling.

## 4. Residual risks and what this does not verify

- **No real provider model.** The throughput, latency and soak measurements run against
  `fake-pi` to stay deterministic; the real pi is only spawned and stopped (C1/C4), never
  driven through a prompt. Nothing here exercises a provider, a model turn or pi's own
  network path.
- **The host is shared.** Every run took place on a machine shared with other agent work,
  so the absolute latencies and rates are load-sensitive even though the runner logs the
  load average before (2.68) and after (2.12) the run. The numbers are reproducible in
  shape, not to the millisecond.
- **C7 and C8 are verified, not measured.** They are properties of the verify
  workstream's suite (`server/test/adversarial`, `docs/verification-report.md` §2 and §5),
  which the runner executes first; this report reports their outcome, it does not
  re-derive it.
- **The C9 window is three minutes.** A leak below roughly 1 MiB/3 min would not be
  visible in the growth number; the criterion bounds growth over that window only.
- **RSS is the OS high-water mark.** `ProcessRSSMiB` reads `VmRSS`, the resident set the
  kernel reports, not the live Go heap; freed-but-unreturned pages and the Go runtime's
  own arena behaviour are inside the number.
- **Windows and macOS are not measured.** Every measurement reads `/proc` and spawns
  POSIX children; server portability is not verified here.

## 5. Where to read the decision

The pass/fail decision and its rationale live in
[`docs/adr/0007-go-spike-decision.md`](adr/0007-go-spike-decision.md). The fallback
trigger of `docs/spike-interfaces.md` §12 — failing C2+C3 together, or C5 by more than 2×
— was not triggered: C2 and C3 both pass, and C5 measured 6 999.9 events/s against a
5 000 events/s threshold (§2).
