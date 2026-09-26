# Phase 1 spike — adversarial verification report

**Owner:** verify workstream (docs/spike-interfaces.md §13).
**Suite:** `server/test/adversarial/**` (external test package `adversarial_test`).
**Status of the tree at verification time:** commits `65bb85a`, `30a53d1`, `7687506`,
`e3f28c9` (this workstream) plus the packages as of `e83bf13`.

**Re-verified after the finding fixes:** commits `ac88717`, `fc1e03b`, `9c8d604`
(sessions) at report baseline `a0c64e0`; §6.7 records the outcome.

This report records what was attacked, what the pipeline did, and what remains
unverified. It is deliberately narrow: every claim below is backed by a test in
`server/test/adversarial/` that drives the real code paths — a real HTTP listener,
a real WebSocket connection, the real `rpc` bridge and a real `fake-pi` child
process — through exported seams only. No test reaches into package internals, no
test talks to a model, and no test leaves the loopback interface.

## 1. How to reproduce

```bash
cd server

# The whole adversarial suite (race detector on, deterministic, ~17 s).
go test -race -count=1 ./test/adversarial/

# One area at a time, verbose.
go test -race -count=1 -run 'TestFraming'   -v ./test/adversarial/
go test -race -count=1 -run 'TestPipeline'  -v ./test/adversarial/
go test -race -count=1 -run 'TestWS_'       -v ./test/adversarial/
go test -race -count=1 -run 'TestREST'      -v ./test/adversarial/
go test -race -count=1 -run 'TestLifecycle' -v ./test/adversarial/

# The repository gate this workstream is held to.
gofmt -l . && go vet ./... && go test -race ./... && CGO_ENABLED=0 go build ./...
```

Determinism and independence: each test builds its own stack (hub, supervisor,
`httptest` listener, child processes) inside `newStack`; no test shares a port, a
hub or a child. The only fixed sleeps are inside scripted child behaviour (a
record split across two writes); every assertion polls with a 15 s deadline. The
suite is run with the race detector.

### Suite inventory

| File | Covers | Commit |
|---|---|---|
| `framing_test.go` | rpc-level C7 bytes against a scripted child | `65bb85a` |
| `pipeline_test.go` | the same bytes through stdout → hub → WS, plus the invalid-JSON deviation | `30a53d1` |
| `ws_handshake_test.go` | handshake, auth, Host/Origin, frame validation, oversized frame | `7687506` |
| `ws_stream_test.go` | commands/ops, dialogs, replay cursors, slow consumer | `7687506` |
| `rest_test.go` | auth scopes, body limits, session limits, method/path abuse, failed readiness | `e3f28c9` |
| `lifecycle_test.go` | C8 SIGTERM reaping, crash vs clean-exit classification | `e3f28c9` |
| `harness_test.go` | shared stack, WS client, raw upgrade, process helpers | (with the files above) |

## 2. Framing and transport (acceptance criterion C7)

Method: `framing_test.go` spawns a POSIX shell that writes hand-crafted bytes to
stdout through the real `internal/rpc` bridge; `pipeline_test.go` repeats the
interesting cases through child → rpc → sessions → hub → WebSocket.

| Case | Result |
|---|---|
| Record containing U+2028/U+2029, literal code points | Pass — bytes survive the bridge untouched and reach the WS client as the same code points (`TestFraming_SeparatorsCRLFSplitAndInvalidPassThrough`, `TestPipeline_SeparatorsAndCRLFEndToEnd`) |
| CRLF-terminated records | Pass — only the `\r` is stripped; the record body is byte-exact |
| Record split across two writes (separate reads) | Pass — reassembled exactly once |
| 8 MiB record | Pass — byte-exact through the bridge and through the hub to the client (`TestFraming_LargeRecordSurvivesSplitReads`, `TestPipeline_BigRecordReachesAClient`) |
| Record without a terminator at EOF | Pass — delivered once |
| Invalid JSON on stdout | Pass at the rpc layer: delivered byte-for-byte as `Record{Type:""}`; §6.2 records how the line now survives the WS leg as `pi.unknown{"raw":…}` |
| stderr never mixed into the record stream | Pass — 51 diagnostic lines interleaved with 50 records, none present in any record (`TestFraming_StderrStaysOutOfTheRecordStream`) |
| Unknown event type from the child | Pass — forwarded verbatim as `pi.<type>` (`TestPipeline_UnknownEventTypeIsForwardedVerbatim`) |

## 3. WebSocket handshake and frame abuse

Method: real dials through `coder/websocket`, plus hand-written upgrade requests
over TCP for the headers the dialer cannot forge (Host, Origin), plus the hub
handler directly with a forged `RemoteAddr` for the non-loopback cases.

| Attack | Result |
|---|---|
| First frame is not `hello` | Pass — post-upgrade close 4401 (`TestWS_FirstFrameMustBeHello`) |
| Unsupported version, missing `client`, wrong type for `v` | Pass — close 4401 (`TestWS_WrongProtocolVersionCloses4401`) |
| Duplicate `hello` | Pass — ignored; the connection stays usable |
| Missing / wrong bearer token | Pass — HTTP 401 with the §11 envelope before the upgrade |
| Token in the query string | Pass — ignored; still 401 (`TestWS_RefusedHandshakeIsHTTP401`) |
| Forged `Host` (DNS rebinding) | Pass — 401 before the upgrade |
| Foreign `Origin`, `Origin: null` | Pass — 401 before the upgrade |
| Same-authority `Host`/`Origin` | Pass — 101 upgrade |
| Non-loopback peer without a token | Pass — 401 (`TestWS_NonLoopbackPeerNeedsAToken`) |
| 2 MiB inbound frame | Pass — that connection closes 1009, the listener keeps serving new ones (`TestWS_OversizedFrameIsRejectedWithoutKillingTheServer`) |
| Invalid JSON frame, unknown frame type | Pass — dropped silently, connection stays usable |
| Command frame failing schema validation with an id | Pass — `response{ok:false,error:{code:"bad_request"}}` |
| Unknown op, malformed payloads (scalar, missing fields) | Pass — coded `bad_request` responses, connection stays usable (`TestWS_UnknownOpAndMalformedPayloads`) |
| Subscribe to an id the server never issued | Pass — silent by contract; no error frame (`TestWS_SubscribeToUnknownSessionIsNotAnError`) |
| Double answer to one dialog | Pass — first answer `ok:true`, second `already_answered` (`TestWS_DialogFirstAnswerWins`) |
| Dialog nobody answers | Pass — `server.dialog.timeout{requestId}` and a late answer gets `already_answered` (`TestWS_DialogTimeoutAnswersAndRetains`) |
| Durable cursor for an unknown entry | Pass — `server.error{replay_cursor_invalid}` then `server.replay.end{complete:false}`, on the replaying connection only (`TestWS_ReplayUnknownEntryIdIsCursorInvalid`, finding 6.3) |
| `since.seq` older than the ring window | Pass — exactly the retained events, in order, `truncated:true` (`TestWS_ReplaySinceSeqOutOfWindowIsTruncated`) |
| Slow consumer | Pass — `server.error{slow_consumer}` then close 1008 (`TestWS_SlowConsumerIsDisconnected`) |

## 4. REST abuse

| Attack | Result |
|---|---|
| `health` without a credential | Pass — 200 `{"status":"ok"}`, `X-Piui-Protocol: 1` |
| Write from a loopback peer with a token configured (viewer scope) | Pass — 403 `forbidden_scope` |
| Read/write from a non-loopback peer without a valid token | Pass — 401 `unauthorized`, query-string token ignored |
| Body that is not JSON / scalar JSON / missing or blank `cwd` | Pass — 400 `bad_request` |
| Body above the 1 MiB cap | Pass — 400 `bad_request` (see observation 7.3) |
| Second session with `maxSessions=1` | Pass — 409 `session_limit` |
| Slot released after the child exits, session stays listed | Pass — the next create is 201 |
| Unknown session id (read and stop) | Pass — 404 `session_not_found` |
| Wrong method on a known path / unknown path | Pass — 405 / 404, both JSON with `X-Piui-Protocol: 1` |
| Child that never answers `get_state` | Pass — POST /sessions = 502 `pi_error`, the session stays listed and its child is reaped |

Method: `rest_test.go` against the in-process router; the non-loopback cases use a
request with a forged `RemoteAddr`, because the test host has no second interface.

## 5. Lifecycle

| Case | Result |
|---|---|
| SIGTERM to the real `pi-ui serve` binary with two sessions | Pass — server exits 0 within 2 s, both children are reaped within the same budget, zero orphan `fake-pi` processes (`/proc` scan) (`TestLifecycle_SIGTERMReapsAllChildrenWithinTwoSeconds`) |
| Child crash (exit code 9) | Pass — `server.crashed{exitCode:9}`, session listed with status `crashed` and exit code 9 |
| Child exits 0 | Pass — `server.exited{exitCode:0}`, status `exited`; both sessions stay listed (`TestLifecycle_CrashAndCleanExitClassified`) |



## 6. Findings

**6.1 [RESOLVED — owner: sessions] `Shutdown` returned before the terminal session status was written.**

- Repro when it was reported: `cd server && go test -race -count=2 ./internal/sessions/`
  — roughly one run in four failed with
  `lifecycle_test.go:131: session s_… status = "stopping", want "exited"`.
- Cause: `Manager.Shutdown` waited for the per-session `stop` goroutines, which
  return once `rpc.Bridge.Close` has reaped the child; the terminal status is
  written later by the session pump in `finish()`, after `Records()` closes. The
  window was small but observable under `-race` load. Process reaping itself was
  always correct: the C8 test in this suite passes every time.
- Resolution: `ac88717` (*sessions: publish the terminal status before Shutdown
  returns*). The pump closes a per-session `done` channel after `finish()` has
  written the terminal status, and `stop` waits for it under a `shutdownBudget`
  deadline (`session.awaitTerminal`), so a caller can no longer read `stopping`
  for a session whose child is already reaped.
- Pinned by `TestStopWaitsForTheTerminalStatus` (`internal/sessions/lifecycle_test.go`),
  which stalls the record stream so the previously probabilistic window is
  deterministic; `go test -race -count=2 ./internal/sessions/` is green. The
  adversarial suite adds `TestLifecycle_ShutdownLeavesNoStoppingSession`, which
  reads every session through REST the instant `Shutdown` returns.

**6.2 [RESOLVED — owners: sessions, ws] an invalid-JSON child record no longer loses its bytes on the WS leg.**

- Behaviour when it was reported: `rpc` delivered the line as `Record{Type:""}`
  (correct, see §2); `sessions` mapped it to `pi.unknown` with the raw bytes as
  payload, and the hub's `marshalEvent` refused a payload that is not valid JSON,
  so the subscriber got `server.error{code:"internal"}` and the bytes were lost;
  a replay re-emitted the same error.
- Resolution: `fc1e03b` (*sessions: keep an unparseable child line readable on the
  wire*). An unparseable line now travels as `pi.unknown` with
  `{"raw":"<line>"}` as payload, which the hub can frame, so the client recovers
  the child's bytes exactly — from `raw`, or from `rawBase64` when the line is not
  valid UTF-8 (follow-up to review finding S3) — and a replay re-emits the same
  `pi.unknown`.
- Pinned by `TestPipeline_InvalidJSONRecordIsSurfacedNotSwallowed`, which now
  asserts exactly one `pi.unknown` frame whose payload is valid JSON and whose
  `raw` field round-trips the line, that no `server.error{internal}` appears, and
  that one dirty line still does not poison the session; the rpc-level
  pass-through is asserted separately.

**6.3 [RESOLVED — owner: ws, sessions] replay failure ordering.** The unknown-cursor
failure was published into the session event stream, so it arrived *after*
`server.replay.end` and every other subscriber of the session saw it. `9c8d604`
(*sessions: report an invalid replay cursor as a failure, not an event*) returns
it as a coded `replay_cursor_invalid` error instead, and the hub reports it to the
replaying connection **before** `server.replay.end`; `complete:false` still arrives
either way, and the failure never enters the session event stream. Pinned by
`TestReplayWithUnknownCursorFailsWithACodedError` (`internal/sessions`) and by
`TestWS_ReplayUnknownEntryIdIsCursorInvalid`, which asserts the strict order and
that a second subscriber of the same session never sees the failure.

**6.4 [OBSERVATION] oversized REST body answers 400, not 413.** `maxBodyBytes`
is 1 MiB; a larger body comes back as `bad_request` (400). The taxonomy has
`too_large` mapped to 413. The brief asked to report the limit, not to impose
one; no change requested.

**6.5 [OBSERVATION] `/ws/v1` method/path errors are not JSON.** `POST /ws/v1`
returns 405 `text/plain`, `GET /ws/v1` without an upgrade returns 426
`text/plain` (the library's own answer). Both carry `X-Piui-Protocol: 1`. The
§5.4 "JSON everywhere" wording is about `/api/v1`; recorded for completeness.

**6.6 [OBSERVATION] the hello deadline is fixed at 10 s.** The hub reads the first
frame under `handshakeTimeout = 10 * time.Second`, a package variable that
`Options` does not expose, so the test suite does not attack "upgrade then stay
silent" (it would add 10 s per case). A hostile client can hold a socket for 10 s
without sending `hello`; no resource amplification beyond the socket itself.

### 6.7 Post-fix re-verification

The three findings above were re-verified through the adversarial suite on the
fixed sessions code at `9c8d604` (fix commits `ac88717`, `fc1e03b`, `9c8d604`;
report baseline `a0c64e0`). All commands were run from `server/` with the race
detector on.

| Finding | Adversarial case | Command | Result |
|---|---|---|---|
| 6.1 | `TestLifecycle_ShutdownLeavesNoStoppingSession` (new) | `go test -race -count=3 -run TestLifecycle_ShutdownLeavesNoStoppingSession ./test/adversarial/` | 3/3 pass |
| 6.2 | `TestPipeline_InvalidJSONRecordIsSurfacedNotSwallowed` (re-pinned) | `go test -race -count=1 -run TestPipeline_InvalidJSONRecordIsSurfacedNotSwallowed ./test/adversarial/` | pass |
| 6.3 | `TestWS_ReplayUnknownEntryIdIsCursorInvalid` (extended) | `go test -race -count=1 -run TestWS_ReplayUnknownEntryIdIsCursorInvalid ./test/adversarial/` | pass |

Whole-suite and repository gates on the fixed tree:

    go test -race -count=1 ./test/adversarial/             # 31 tests, 0 failures, ~18 s
    go test -race -count=1 ./test/adversarial/ ./test/e2e/ # both packages green
    gofmt -l . && go vet ./... && CGO_ENABLED=0 go build ./... && go test -race ./...  # clean

Documentation follow-up outside this workstream: the `pi.*` row of
`docs/ws-protocol.md` ("payload is the record byte for byte") and
`docs/spike-interfaces.md` §5.3/§7 (verbatim `pi.<record.type>`; the unknown
cursor published as a `server.error` event) still describe the pre-fix
behaviour; flagged to the lead for a follow-up edit.

## 7. Residual risks and gaps (what this suite does not verify)

- **No real pi, no model, no provider.** Everything runs against `fake-pi`
  (`server/test/fake-pi`). C1–C4 and C9 (spawn latency, RSS, soak) are the
  measure workstream's; C5/C6 (throughput, latency) likewise. This report covers
  C7 and C8 only.
- **No TLS, no reverse proxy, no second interface.** Host/Origin allow-lists are
  exercised only in their empty (default) configuration; a non-empty
  `AllowHosts`/`AllowOrigins` positive path is untested. Non-loopback peers are
  forged via `RemoteAddr` at the handler, not observed from a real remote host.
- **Heartbeat close path not attacked.** The suite sets `Heartbeat` to one minute
  and never provokes three missed pongs or counts server pings; `internal/ws`
  covers that path in its own tests.
- **Binary and fragmented WS messages** are not sent; the server ignores
  non-text messages, but no adversarial case drives that branch.
- **Ring time eviction (`ReplayWindow`)** is not exercised; only size eviction
  (`ReplayEvents`) is. A clock-based case would need a controllable clock.
- **Dialog timeout answers `cancelled:true` to the child** — the child-side write
  is not directly observable (`fake-pi` consumes `extension_ui_response`
  silently); only the `server.dialog.timeout` event and the `already_answered`
  retention are asserted.
- **Slow-consumer thresholds** are exercised with `SendBuffer=2` and 256 KiB
  frames, not with the production defaults (512 queued frames), so the exact
  production pressure point is not measured.
- **REST beyond the spike surface**: concurrency limits, rate limiting, header
  and connection limits, request smuggling — out of scope by §2.
- **Windows/macOS**: the framing tests skip without a POSIX shell and the SIGTERM
  test is POSIX-only; server portability is not verified here.
- **Fuzzing**: the hostile corpus is fixed; there is no continuous fuzzing of the
  frame schema or of rpc framing.

## 8. Acceptance matrix mapping

| Criterion | Where verified |
|---|---|
| C7 framing correctness | This suite (§2): U+2028/U+2029, CRLF, 8 MiB record, split reads — 100 % intact, no panic |
| C8 shutdown reaps all children ≤ 2 s, zero orphans | This suite (§5), against the real binary |
| C1–C4 spawn/RSS, C5 throughput/loss, C6 latency, C9 soak | Not verified here — measure workstream (`server/internal/spike`, `server/test/e2e`, `docs/spike-report.md`) |
