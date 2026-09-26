// Package rpc drives one `pi --mode rpc` child process over JSONL stdio: it starts the
// child, frames its stdout records and reports its lifecycle. Policy (when to spawn,
// restart or stop a session) lives in internal/sessions; this package owns the pipe.
//
// # Framing
//
// Records are split on 0x0A only. Node's readline also splits on U+2028 and U+2029, which
// are valid characters inside a JSON string; pi streams arbitrary text through records,
// so those bytes must reach the caller untouched. One trailing 0x0D is stripped (a child
// that terminates records with CRLF), empty lines are skipped, and a final record without
// a terminator is still delivered before io.EOF. Unknown record types and unknown fields
// pass through unchanged: a client tolerates what it does not know.
//
// # Backpressure
//
// Child stdout is read continuously into the bounded Records channel (Spec/Options below).
// When the consumer falls behind, the reader stops reading instead of dropping records:
// pi honours stdout backpressure, so the child slows down and no record is lost. A
// consumer that never drains parks the reader goroutine, which is why Close kills and
// reaps the child on its own instead of waiting for the reader.
//
// # Lifecycle
//
// Start resolves argv[0] with exec.LookPath, merges Spec.Env over os.Environ(), starts the
// child in its own process group (Linux sets Pdeathsig to SIGKILL, so a server killed
// without cleanup leaves no orphan) and pipes stdin/stdout/stderr. Stderr lines are
// forwarded to the Spec.Stderr hook, never into the record stream: stdout stays
// protocol-only. Wait reports the exit status without closing the bridge; Close closes
// stdin first (pi's orderly shutdown), escalates to SIGTERM and SIGKILL on the process
// group and always reaps. Records() closes exactly once, after stdout reached EOF and the
// child was reaped.
//
// # Contract
//
// New applies the documented defaults to a zero Options (1024 buffered records, 15s per
// command, 5s KillGrace) and spawns nothing; Start does. A bridge reports failures through
// two sentinels: ErrClosed once the child is gone or Close ran (the wrapped message
// carries the exit status) and ErrTimeout when a response did not arrive inside the
// caller's deadline or, if the context had none, inside SendTimeout. Both are wrapped, so
// errors.Is keeps working while the message stays useful.
//
// Send correlates by id and never closes the child: a timeout leaves the bridge usable and
// the late response is dropped with a diagnostic instead of being handed to the next
// command. Write streams extension_ui_response records without waiting. Both serialise
// their writes, so a record can never interleave with another on the child's stdin.
package rpc
