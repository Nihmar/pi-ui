# Phase 1 spike — adversarial verification report

**Owner:** verify workstream (docs/spike-interfaces.md §13).
**Suite:** `server/test/adversarial/**` (external test package `adversarial_test`).
**Status of the tree at verification time:** commits `65bb85a`, `30a53d1`, `7687506`,
`e3f28c9` (this workstream) plus the packages as of `e83bf13`.

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
| Invalid JSON on stdout | Pass at the rpc layer: delivered byte-for-byte as `Record{Type:""}`; see §6.2 for the WS-leg deviation |
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
| Durable cursor for an unknown entry | Pass — `server.error{replay_cursor_invalid}` and `server.replay.end{complete:false}` (`TestWS_ReplayUnknownEntryIdIsCursorInvalid`) |
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

**6.1 [OPEN — owner: sessions] `Shutdown` can return before the terminal session status is written.**

- Repro: `cd server && go test -race -count=2 ./internal/sessions/` — roughly one run
  in four fails with
  `lifecycle_test.go:131: session s_… status = "stopping", want "exited"`.
- Evidence: reproduced before this workstream added any test (the failing package
  is untouched by `test/adversarial/**`). `Manager.Shutdown` waits for the
  per-session `stop` goroutines, which return once `rpc.Bridge.Close` has reaped
  the child; the terminal status is written later by the session pump in
  `finish()`, after `Records()` closes. The window is small but observable under
  `-race` load.
- Impact: a client (or an operator's script) can read
  `GET /api/v1/sessions/{id}` as `stopping` for a session whose child is already
  reaped. Process reaping itself is correct: the C8 test in this suite passes
  every time.
- Reported to the sessions workstream and broadcast to the team; not fixed by
  this workstream (ownership rule). Suggested fix: make `Shutdown` (or `stop`)
  wait for the pump/`finish` before returning, e.g. a `done` channel closed by
  `pump`, since the existing test asserts the stronger property.

**6.2 [DEVIATION — owners: ws, sessions] an invalid-JSON child record is not passed through on the WS leg.**

- Behaviour: `rpc` delivers the line as `Record{Type:""}` (correct, see §2).
  `sessions` maps it to `pi.unknown` with the raw bytes as payload; the hub's
  `marshalEvent` refuses a payload that is not valid JSON and the subscriber gets
  `server.error{code:"internal"}`, so the raw bytes are lost on the WS leg and a
  replay re-emits the same error.
- The adversarial test pins the observed fallback and the property that one
  dirty line does not poison the session
  (`TestPipeline_InvalidJSONRecordIsSurfacedNotSwallowed`); the rpc-level
  pass-through is asserted separately.
- Reported to ws and sessions with options (document the degradation, or encode
  the bytes as a JSON string, or have sessions publish `server.error` itself).
  The test will be updated when the behaviour changes.

**6.3 [OBSERVATION] replay failure ordering.** For an unknown durable cursor the
subscriber sees `server.replay.begin` → `server.replay.end{complete:false}` →
`server.error{replay_cursor_invalid}`: the reason arrives *after* the replay is
closed. A client that clears its replay state on `replay.end` may miss it. The
test waits for both frames, so the suite is not order-sensitive; ws may want the
error before the end frame.

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
