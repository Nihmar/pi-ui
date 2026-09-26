# ADR 0007 — Go server stands: Phase 1 spike decision

## 1. Status

Accepted — 2026-09-26.

The decision is: **Go stands, no Python fallback evaluation.** The Phase 1 spike met every
acceptance criterion on the development host, so the driver chosen in `PLAN.md` §3.2 —
one `pi --mode rpc` child process per session — is kept, and the language fallback recorded
in the ADR's scope is not opened.

## 2. Context

Phase 1 exists to validate the riskiest assumptions of the server **before** Phase 3 builds
on them, not during it (`docs/spike-interfaces.md` §1). Two assumptions carried the most
risk: that a Go process can orchestrate one real `pi` child per session within the host's
memory and startup budget (the N-processes RAM/startup risk in `PLAN.md` §9), and that the
byte-level LF-only JSONL pipeline survives real throughput without corrupting records or
stalling a child (`PLAN.md` §3.6). The spike therefore built the real pipeline end to end —
child driver, WebSocket hub with replay, session supervisor, a minimal bridge extension, the
CLI and the `fake-pi` harness — and measured it against a matrix frozen before any number
was taken.

The criteria are `docs/spike-interfaces.md` §12 (C1–C9), each with a fixed threshold and an
explicit fallback trigger. Freezing the contract first is what makes the numbers admissible:
the acceptance thresholds could not be moved to fit the measurements. All figures come from
one development host — 13th Gen Intel Core i5-13400F, 12 cores, 31 925 MiB RAM, kernel
7.2.7-1-cachyos — running pi 0.87.1. Raw samples live in `docs/spike-report.md`; C7 and C8
were verified adversarially in `docs/verification-report.md`.

Before the numbers could be trusted, the harness itself had to be verified, and it failed
that check twice. Two defects were found in the spike's own measurement code:

- **A `time.Ticker`-paced event source.** A `time.Ticker` cannot exceed the host's timer
  granularity (~1 ms), so a ticker-driven load generator could not reach the rate C5 asks
  for: a naive run would have measured the ticker, not the pipeline. The source was rebuilt
  to pace events on a deadline schedule that catches up after a coarse sleep, with no timer
  floor.
- **A sampler inside the measured process.** The latency harness retained one sample per
  event inside the same process whose RSS was being measured for C2, C3 and C9, so the
  instrument contributed to the value it reported: 360 000 samples cost ≈10.2 MiB, more than
  the entire growth the first soak reported. The sampler was bounded to a fixed
  65 536-sample reservoir window, so it no longer grows with the event count.

Both defects are fixed and pinned by tests recorded in `docs/spike-report.md`. They are the
reason this ADR states pass/fail per criterion while the raw samples, the harness fixes and
the tests that guard them stay in the spike report.

## 3. Decision

Keep the Go server with the child-per-session RPC driver (`PLAN.md` §3.2): one
`pi --mode rpc` child per session, one goroutine per session/PTY/WS, byte-level LF-only
framing, `pi.*` payloads forwarded verbatim. Every criterion in the frozen matrix passed.

### 3.1 Acceptance matrix result

| # | Criterion | Measured | Threshold | Result |
|---|---|---|---|---|
| C1 | Spawn → `get_state` ready, real pi, 8 children in parallel | p50 547.0 ms, p95 584.4 ms | p95 ≤ 5 000 ms | PASS |
| C2 | Go server RSS, idle, 2 sessions + 1 WS client | 16.9 MiB | ≤ 128 MiB | PASS |
| C3 | Go server RSS, 8 sessions | 19.5 MiB | ≤ 192 MiB | PASS |
| C4 | Per-child RSS, real pi, idle | ≈142 MiB each; sum 1 136.4 MiB for 8 | sum of 8 ≤ 2 400 MiB | PASS |
| C5 | fake-pi → rpc → sessions → hub → 1 WS client | 6 999.9 events/s sustained 30 s, loss 0 | ≥ 5 000 events/s, loss 0 | PASS |
| C6 | End-to-end latency under load | p95 0.34 ms (rapid run and 3-minute soak) | p95 ≤ 300 ms | PASS |
| C7 | Framing: U+2028/U+2029, CRLF, 8 MiB record, split reads | adversarial framing + pipeline suite green | 100 % intact, no panic | PASS |
| C8 | SIGTERM to the server reaps all children | lifecycle suite green, against the real binary | ≤ 2 s, zero orphans | PASS |
| C9 | 3-minute soak at ~2 000 events/s | RSS −2.10 % after warm-up | growth ≤ 10 % | PASS |

C5 was additionally probed for headroom on the same host: the same pipeline sustained
10 000 events/s and 15 000 events/s for 30 s each with loss 0, three times the threshold.

### 3.2 Fallback trigger

The trigger defined in `docs/spike-interfaces.md` §12 fires if C2 and C3 fail together, or if
C5 misses its threshold by more than 2×. Neither condition occurred. C2 and C3 passed with
7.6× and 9.8× margin respectively; C5 passed and the pipeline sustained three times the
threshold in the headroom probes. The Python fallback is not triggered and no fallback
evaluation is opened.

### 3.3 Alternatives considered

- **In-process SDK** (`PLAN.md` §3.2). Lowest latency and typed APIs, but a single process for
  every session (one failure takes the whole server down), coupling to an SDK whose internal
  APIs change between releases while the RPC wire stays stable, and weaker per-session
  isolation because cwd, env and flags are process state rather than per-child argv. Rejected
  in the driver comparison for those reasons and not revisited: the spike's spawn and RSS
  numbers (C1, C4) show the per-child process cost is affordable, so the isolation the child
  driver buys is not the liability it was assumed to be.
- **Python server** (the fallback language recorded for this decision in `PLAN.md` §3.1 and
  §9). This is the alternative the §12 trigger would activate. It is not chosen because the
  trigger did not fire: the Go spike met every criterion, so there is nothing to fall back
  for, and switching here would trade a measured, passing implementation for an unmeasured
  one.
- **ACP** (`pi-acp`, `PLAN.md` §3.2). Interoperability with ACP clients, but poorly suited to
  server-side multi-session work in different working directories, and less control over the
  pi-specific commands and events that the verbatim pass-through depends on. Rejected in the
  driver comparison, for the same reason it was not made the driver.

## 4. Consequences

**Capacity.** C4, not the server's own footprint, is the binding number. The Go server is
cheap — 16.9 MiB for 2 sessions and 19.5 MiB for 8 — but each idle real `pi` child costs
≈142 MiB, so eight concurrent real sessions add ≈1.1 GiB of child memory on top of a ≈20 MiB
server. The session limit (8 by default) and idle eviction are what hold that budget; they
stay in the plan and are not relaxed on the grounds that the server itself is small.

**What Phase 3 must keep doing.**

- **Measure in a separate process.** A harness that samples latency or allocates per event
  inside the process whose RSS is under measurement reports its own cost, not the server's —
  the second defect above. Phase 3 keeps RSS measurement and latency sampling out of the
  measured process.
- **Do not accept a criterion whose source rate equals its threshold.** C5's threshold is
  5 000 events/s; sustaining exactly 5 000/s would demonstrate that the generator can be set
  to the target, not that the pipeline holds. The spike proved the margin by driving the same
  pipeline at 10 000/s and 15 000/s with no loss, which is why 6 999.9/s is a pass and not a
  tie. The ticker defect is the failure mode this rule guards against: a source capped below
  the threshold cannot certify the threshold.

**What remains unproven.**

- **No real provider traffic.** C5/C6/C9 ran through `fake-pi`; C1 and C4 touched real `pi`
  only for spawn and idle RSS. Streaming against a live model, provider errors, rate limits
  and the provider-latency tail are outside these numbers.
- **RSS is not live heap.** Every memory figure is `/proc` VmRSS including descendants. It
  bounds what the host sees; it says nothing about live Go heap, GC headroom or fragmentation
  over a run longer than the 3-minute soak.
- **Shared-host latency.** All figures come from one 12-core desktop measured while other
  work was running on it (load average 2.68 before and 2.12 after the run), so C6's p95 of
  0.34 ms is a pipeline cost on a lightly loaded host, not a latency guarantee under a busy
  one; 300 ms is the contract, and the measurement is the margin.

## 5. References

- `docs/spike-interfaces.md` §12 — frozen acceptance matrix C1–C9, thresholds and the
  fallback trigger answered here; §1 for the spike scope; §13 for ownership.
- `docs/spike-report.md` — the measured numbers and raw samples, the two harness defects and
  the tests that pin their fixes.
- `docs/verification-report.md` — adversarial verification of C7 (framing) and C8 (shutdown)
  against the real binary.
- `PLAN.md` §3.2 (driver comparison), §3.6 (Go stack) and §9 (the N-processes RAM/startup risk
  and its mitigation) — the local plan, not a versioned document.
- ADR 0008 — Docker Compose as the reference deployment.
- ADR 0009 — the Niman markdown engine extracted into `packages/piui-markdown`.
