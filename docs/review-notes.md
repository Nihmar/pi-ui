# Phase 1 code review — findings and status

**Owner:** measure workstream, on behalf of the whole tree (`docs/spike-interfaces.md` §13).
**Scope reviewed:** every path in the repository — `server/**` (rpc, ws, sessions, api, cli,
protocol/gen, spike, test/**), `bridge/**`, `schemas/**`, `docs/**`, `README.md`,
`.github/workflows/ci.yml`.
**Method:** four read-only reviewers, each owning one area, reading the tree at HEAD
`0d9df72` plus the working-tree fixes listed below. No reviewer ran a build, a test or the
measurement runner — a timing measurement was in flight on the same host and load would have
corrupted it — so every finding below comes from reading the code against the frozen
contract (`docs/spike-interfaces.md`, `docs/ws-protocol.md`, `AGENTS.md`) and the schemas.
**Fixed in this pass** means the fix is in history with a test that fails without it.

## 1. Blockers, both fixed

| # | Area | Finding | Status |
|---|---|---|---|
| B1 | ws | `subscription.beginReplay` cleared the replay buffer, silently dropping every live event of the session that was published between the subscription's registration and the replay's first frame — the ring snapshot does not contain them and nothing else replays them. This is the durable-reconnect path. | Fixed `f0119bb`, pinned by `TestBeginReplayKeepsEventsBufferedBeforeIt` (fails with the reset restored) |
| B2 | sessions | A `command` frame carrying `"payload":null` nils the map `decodeObject` builds, and the `setField` that adds the pi command type panics on a nil map: a client-triggerable crash of the whole server through `session.prompt`, `session.steer`, `session.follow_up`, `session.abort` or `session.clear_queue`. | Fixed `0b340d4`, pinned by `TestCommandFromPayloadNullIsAnEmptyPayload` and the adversarial malformed-payload table |

## 2. `internal/rpc` — child driver

| # | Severity | Finding | Status |
|---|---|---|---|
| R1 | should-fix | A response that reaches the bridge while its sender is already leaving (timed out, or released) still matches `pending`, so it lands in the one-slot result channel after nobody waits for it; the next `Send` returned that answer as its own — the previous command's payload and id. | Fixed, pinned by `TestSendIgnoresALateResponseForAClosedExpectation` (fails without the id check) |
| R2 | should-fix | `Close` could leave a zombie: the fallback reap was gated on `Signal(0)`, which an unreaped child still accepts, so a killed child whose reader goroutine is parked on a full `Records` channel was never reaped. The SIGKILL path never reaped at all. | Fixed, pinned by `TestCloseLeavesNoZombieWhenConsumerStops` — the test now emits `RecordBuffer+1` records so the reader really parks on the channel send, and it fails against the previous semantics |
| R3 | should-fix | The old `TestCloseLeavesNoZombieWhenConsumerStops` did not exercise the path it documented: one record fits a one-slot buffer, so the reader waited on the pipe and reaped the child itself. | Fixed with R2 |
| R4 | should-fix | The new pacer accepted a NaN rate (`rate <= 0` is false for NaN) and `time.Duration(NaN)` is implementation-defined. | Fixed `df82ca0`, pinned by `TestNewPacerRejectsUnusableRates` |
| R5 | nit | `time.Duration(i) * p.interval` overflows int64 for absurdly small rates and the wrapped deadline made every record look behind schedule. | Fixed `df82ca0` |
| R6 | nit | `Start` racing `Close` between `claimStart` and `register` can leave the just-spawned child unkilled. | Fixed `b3d7c97`: `register` kills a child it publishes to an already-closed bridge, pinned by `TestRegisterAfterCloseKillsTheChild` |
| R7 | nit | The elapsed bound in `TestSyntheticPromptStreamCatchesUp` only discriminates on a host with ~1 ms timer granularity. | Partly addressed (`TestNewPacerRejectsUnusableRates` covers the rate edges; the timing bound stays host-dependent) |
| R8 | nit | No test covers `ErrTimeout`, an unknown-id response being dropped, or the "already in flight" rejection. | Fixed `ae8eb4c`, pinned by `TestSendTimeoutReturnsErrTimeout`, `TestResponseForAnUnknownIDIsDropped` and `TestSendRejectsASecondCommandInFlight` |

## 3. `internal/ws` — hub, replay, handshake

| # | Severity | Finding | Status |
|---|---|---|---|
| W1 | blocker | See B1. | Fixed |
| W2 | should-fix | Durable-replay events are stamped from the live `seq` counter, so a live event published during a durable replay can carry a lower seq than the last replayed event and is dropped by the `seq <= lastSent` flush dedup even though it was never replayed. | Fixed `30a4703`: the durable flush dedups by entry id over the replay's last `SendBuffer` ids and keeps arrival order, pinned by `TestEntryReplayKeepsALiveEventPublishedDuringTheReplay` (fails with the seq dedup restored) |
| W3 | should-fix | A replay buffer that overflowed enqueued `server.error{slow_consumer}` but never closed the connection, while `docs/ws-protocol.md` ("Buffered events are capped by `SendBuffer` … disconnected like any other slow consumer") promises `1008`. | Fixed, pinned by `TestReplayBufferOverflowClosesTheSubscriber` |
| W4 | should-fix | `since.entryId` for a session the server does not know answers `server.error{session_not_found}`, while the `since.seq` path is silently empty and the doc says an unknown session "is not an error". | Fixed `30a4703`: both cursor paths answer an empty replay, pinned by `TestEntryReplayForAnUnknownSessionIsEmptyNotAnError` |
| W5 | should-fix | `h.rings` is never reduced when a session goes away, so a long-running server that churns sessions keeps one ring per session forever. | Fixed `628cbda`: the heartbeat sweep releases a ring with no subscriber and nothing replayable left, pinned by `TestSweepRingsReleasesHistoryNobodyCanReplay` |
| W6 | nit | A binary first frame is fatal in the handshake (close 4401) but ignored after it. | Fixed `b33afbc`: the handshake skips binary frames exactly like the read loop, pinned by `TestHandshakeSkipsBinaryFrames` |
| W7 | nit | The `frame.V != protocolVersion` branch is dead (the schema already pins `v: 1`) and its message misleads for a well-formed `v:2`. | Fixed `b33afbc`: the dead branch is gone and a well-formed foreign hello reports its version, pinned by `TestHandshakeRejectsUnsupportedVersion` |
| W8 | nit | A handler result that is not valid JSON makes `marshalResponse` fail and the terminal `response` frame is dropped, so the client waits forever for that id. | Fixed `e0fba9f`: the command path answers `internal` instead, pinned by `TestCommandWithUnencodableDataStillAnswers` |
| W9 | nit | `sameAuthority` accepts a portless `Origin` against any port (the Host check does compare ports). | Open |
| W10 | nit | `server.heartbeat.sessions` counts sessions with a subscriber, not live sessions. | Documented in `docs/ws-protocol.md` (the hub counts its fan-out, not session lifetimes) |
| W11 | nit | `server.replay.begin`/`end` carry a higher `seq` than the replayed events they wrap, so a client that advances its cursor on every frame sees a backwards stream. | Documented in `docs/ws-protocol.md`: meta frames are not cursors |
| W12 | observation | Close codes `4403`, `4409`, `4426` are specified in §5.2/§6 but appear nowhere in the repository; only `4401` is implemented. | Scoped: those codes belong to the plan's protocol (in the local, unversioned `PLAN.md`), while the versioned contract reserves 4401 only; `docs/ws-protocol.md` now says so instead of inventing the codes early |

## 4. `internal/sessions`, `internal/api`, `internal/cli`

| # | Severity | Finding | Status |
|---|---|---|---|
| S1 | blocker | See B2. | Fixed |
| S2 | should-fix | `awaitTerminal`'s `shutdownBudget` converts "the terminal status was never written" into a successful stop, so the 6.1 fix can mask the failure it was meant to expose; and a per-session stop can take `KillGrace + termGrace + shutdownBudget ≈ 3.7 s` while `Shutdown` bounds itself with the same 2 s, so a wedged child makes SIGTERM exit non-zero. | Fixed `2c0f315`: `awaitTerminal` reports `timeout`, `Shutdown` bounds itself by `stopBudget` (the whole-stop worst case) and `rpc.TermGrace` is exported so the two packages cannot drift, pinned by `TestStopReportsAStalledTerminalStatus` |
| S3 | should-fix | An unparseable child line is carried as a Go `string`, and `json.Marshal` replaces invalid UTF-8 bytes with U+FFFD: the "the client recovers the child's bytes exactly" claim holds only for valid UTF-8. | Fixed `14af92d`: the payload also carries `rawBase64` when the line is not valid UTF-8, pinned by `TestRecordPayloadKeepsTheBytesOfAnUnparseableLine` |
| S4 | should-fix | An oversized REST body answers `bad_request` (400): `MaxBytesReader`'s error is wrapped with `%v`, so the declared `too_large` → 413 mapping is dead code. | Fixed `86cba37`, pinned by `TestCreateSessionOversizedBodyIsTooLarge` and the adversarial oversized-body case (413 `too_large`); observation 6.4 of `docs/verification-report.md` is marked superseded |
| S5 | should-fix | `busy_streaming` is defined, mapped and advertised, but `responseData` maps every `success:false` to `pi_rejected`, so the code is unreachable. | Fixed `86cba37`, pinned by `TestResponseDataMapsPiRejections`: pi's 0.87.1 mid-turn rejection text maps to `busy_streaming`, every other rejection stays `pi_rejected` |
| S6 | nit | `stop` ignores its `context.Context`. | Open |
| S7 | nit | `stopSession`'s 202 comment still says the child is exiting when the response is written, while `Stop` now blocks until the terminal status is written. | Fixed `86cba37` (the comment now states that the 202 carries the state the session is in when Stop returns) |
| S8 | nit | CLI usage errors exit 1 while `doc.go` documents `ExitUsage = 2` for a wrong command line. | Fixed in this commit: `UsageError`/`Usage`/`Usagef` mark command-line failures and `Run` maps them to `ExitUsage`, pinned by the `Run` and `Serve` usage-error tests |
| S9 | nit | The dialog fallback branches publish `Payload: raw` directly instead of going through `recordPayload`. | Fixed `4eff62d`: every dialog payload goes through `recordPayload` |
| S10 | nit | `Stop` in the window between the registry insert and `s.attach(bridge)` is discarded and the session ends up ready. | Open |
| S11 | nit | `Info.ExitCode` hands out the session's own pointer. | Fixed `6135465`, pinned by `TestInfoDoesNotAliasTheExitCode` |

## 5. Repository, tests, CI and bridge

| # | Severity | Finding | Status |
|---|---|---|---|
| C1 | should-fix | The generated-code drift check (`git diff --exit-code -- internal/protocol/gen`) cannot see an untracked new generated file or a deleted one, which is exactly the drift it exists to catch. | Fixed in `.github/workflows/ci.yml` and `server/Makefile` (`git status --porcelain` must be empty) |
| C2 | should-fix | `AGENTS.md` and §5.5 tell developers to keep `go generate ./...` drift-free, but the module had no `//go:generate` directive: the documented command was a no-op. | Fixed (directive added to `internal/protocol/gen/doc.go`) |
| C3 | should-fix | §5.6 did not declare `MeasureServerRSS`, `ServerRSSResult` or `RSSSample`, which drive C2/C3. | Fixed `0f464f1` |
| C4 | should-fix | `withDefaults` documented that a negative value is preserved for validation, but every field used `<= 0`, so negatives were silently defaulted and the measurements' guards were unreachable. | Fixed `02a4d38` |
| C5 | should-fix | The gofmt gate (`test -z "$(gofmt -l .)"`) only read stdout: a gofmt that fails to parse a file passed the gate. | Fixed in CI and the Makefile |
| C6 | nit | `README.md` linked `docs/spike-report.md` and `docs/adr/0007-go-spike-decision.md` before they existed. | Fixed `bd62918` |
| C7 | nit | `bridge/pi-ui-bridge.ts` let one `any` escape strict mode through `Array.isArray`. | Fixed with an explicit `isStringArray` type guard (`npx tsc --noEmit` clean) |
| C8 | nit | `server/deps.go` claimed the pinned runtime dependencies "arrive in their own commits" after they had arrived. | Fixed |
| C9 | nit | Commit `a5b393b` ends with a `Generated with …` line, which `AGENTS.md` forbids verbatim. | Open — history is not rewritten |
| C10 | nit | `server/test/fixtures/*.jsonl` embed host paths (`/home/alessandro/...`) and a loopback endpoint. | Open |
| C11 | nit | Fixtures are checked for shape only; no fixture is replayed through the pipeline or validated against `schemas/pi.json`. | Open |
| C12 | nit | Outbound event `type` is never validated against `schemas/ws.json`'s pattern (deliberate lenient pass-through, but the schema reads as if it were the gate). | Documented in `docs/ws-protocol.md`: the pattern documents the namespaces, the hub publishes what it does not recognise |
| C13 | nit | Timing/pressure-dependent assertions: `test/adversarial/lifecycle_test.go` arms `--crash-after`/`--exit-after` on wall-clock budgets, and `internal/ws/TestSlowConsumerIsDisconnected` depends on socket-buffer pressure — it failed once under a full-suite `-race` run in this session and passed 3/3 in isolation. | Open |
| C14 | nit | A `--fake-pi` run also prints a `throughput:` line for the idle-RSS measurements (a 1-event smoke pass), and `rssGrowthPct` reads `0.00` when the sampler produced no samples rather than "n/a". | Open — documented in `docs/spike-report.md` §1 so the C2/C3 rows cannot be misread |
| C15 | nit | `spike.machineSnapshot` has no callers; `writeRaw`'s comment claims raw samples "never half-land" while `os.WriteFile` truncates first. | Open |
| C16 | nit | `pi-ui measure --out <dir>` has no gitignore guard of its own (only the shell runner refuses a non-ignored directory). | Open |
| C17 | nit | `test/e2e` requires the binary's very first stdout line to be the `listening …` line. | Open |
| C18 | observation | `ThroughputResult`'s latency percentiles come from the first client only, which matters as soon as a run uses more than one subscriber. | Documented (`RawLatencyMs`), unchanged |

## 6. What the review confirmed as clean

- LF-only framing: split on `0x0A` only, one trailing `\r` stripped, empty lines skipped,
  U+2028/U+2029 byte-exact, records reassembled exactly once across reads, no readline-style
  splitting.
- Backpressure: the reader never drops a non-response record and never buffers without
  bound; stdout stays protocol-only and stderr is drained separately.
- Process lifecycle: `Setpgid` + `Pdeathsig` armed under `LockOSThread`, TERM→KILL
  escalation, idempotent `Close`, `Records()` closed exactly once by the reader.
- Inbound WS frames are validated against the embedded `ws.json` before any handler; unknown
  frame types and unknown fields are tolerated; unknown `pi.*` types are forwarded verbatim.
- Schemas: `ws.json` matches the frames the code writes and accepts, its error-code enum
  equals `core.json`'s, the embedded schema literals are byte-identical to `schemas/`, every
  `$ref` is file-local, and the generated types are never hand-edited.
- Sessions: nothing publishes while holding a lock; lock order is never inverted; one pump
  per session; dialogs are first-answer-wins with timers armed under the lock; the terminal
  status is written before `done` closes.
- REST: method+wildcard patterns, JSON envelope and `X-Piui-Protocol` on every answer
  including 404/405, fail-closed auth, no cwd or name leakage on `/health`.
- Bridge: strict TypeScript, defensive config parsing that degrades to `off`, only Node APIs
  the pi runtime provides, versions pinned in `package-lock.json`.
- Repository hygiene: no secrets, tokens, transcripts, build outputs or `PLAN.md` in the
  tree; commit subjects follow `area: imperative summary`; no AI-attribution trailers.
