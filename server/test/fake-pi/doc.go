// Command fake-pi impersonates `pi --mode rpc` on stdio, so the pi-ui server can be
// driven in tests without a model, without network and without the real agent.
//
// The contract is frozen in docs/spike-interfaces.md §9; the rpc, ws, sessions and
// measure workstreams all drive this binary through exec.
//
// Protocol:
//
//   - stdout carries LF-terminated JSON records only; --crlf terminates them with CRLF.
//     Diagnostics never mix into the protocol stream, they go to stderr.
//   - stdin is read as a byte stream and split on 0x0A only, so U+2028/U+2029 inside a
//     JSON string never start a record. A trailing CR is stripped. stdin EOF ends the
//     process with code 0, exactly like pi's orderly shutdown.
//   - Real pi argv is tolerated: unknown flags and their values (--mode rpc,
//     --no-session, --offline, --name x, -e path, ...) and a positional `rpc` are
//     ignored, so a caller can pass the production argv unchanged.
//
// Answers:
//
//   - `extension_ui_response` is consumed silently (it answers a dialog, it does not
//     produce a response).
//   - Every other command is answered from the --script file when it has an entry for
//     that command type, otherwise from the built-in defaults: get_state returns a
//     canned RpcSessionState, get_entries returns the script's entries filtered
//     strictly after `since` (an unknown cursor fails), get_commands returns
//     {"commands":[]} and anything else succeeds with no data.
//   - Scripted responses are auto-filled with the request `id`, the `command` name and
//     the `type` `response` when the script omits them; `error` in a command script
//     forces a failing response.
//   - `script.startup` records are emitted (with their delays) before stdin is read.
//   - A `prompt` additionally streams the synthetic run: --big emits one
//     message_update whose delta is that many bytes, --emit N streams N `chunk-<i>`
//     deltas paced at --rate (i is 0-based, rate 0 means "as fast as possible"), each
//     carrying {"i","ns"} in `spike` for latency measurements, and both are followed
//     by `agent_end` and `agent_settled`. Without --big/--emit a prompt is answered
//     without any events.
//
// Faults:
//
//   - --stall-ms sleeps before answering any command; the script's faults.stallMs does
//     the same, and the longer of the two wins so a delay never doubles.
//   - --crash-after and --exit-after are wall-clock timers armed at process start;
//     they exit with code 9 and 0. The CLI flag wins over the script's faults value.
//     Both wait for the record in flight, so a fault never truncates a record.
//   - --ignore-stdin keeps the process alive without reading stdin until one of those
//     timers (or the parent) ends it; --stderr LINES writes LINES diagnostics to stderr
//     before the first record.
//   - --separators embeds the literal U+2028/U+2029 code points inside a JSON string
//     value of every emitted record (the streaming delta when the record has one, an
//     extra top-level field otherwise), which is what proves the LF-only framing.
//
// Exit codes: 0 clean shutdown, 1 runtime I/O failure, 2 usage or script problem,
// 9 simulated crash.
package main
