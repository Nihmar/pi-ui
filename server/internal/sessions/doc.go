// Package sessions owns the child processes: one `pi --mode rpc` per session, its
// lifecycle, its event stream and the extension dialogs it raises
// (docs/spike-interfaces.md §5.3, §7, §8).
//
// The supervisor is the only place that talks to rpc.Bridge, and it never interprets pi
// payloads: every non-response record is published as `pi.<record.type>` with the record
// bytes as the payload, so a new pi event reaches clients without a server change. A line
// the child got wrong (not valid JSON) cannot be framed as-is, so it travels as
// `pi.unknown` with a `{"raw":"<line>"}` payload: the line survives as a JSON string
// instead of being refused by the hub, and a line that is not valid UTF-8 additionally
// carries `rawBase64` with the exact bytes, because a JSON string would replace them with
// U+FFFD. The single exception is the extension UI subprotocol of §8, which the server
// terminates (dialogs are answerable) instead of forwarding verbatim.
//
// Wiring (one seam, no globals):
//
//	hub := ws.New(ws.Options{...})
//	sup := sessions.New(sessions.Config{PiCommand: []string{"pi", "--mode", "rpc"}, Hub: hub})
//	hub.SetCommandHandler(sup)  // session.* ops
//	hub.SetDialogHandler(sup)   // ui_response answers
//	hub.SetReplayer(sup)        // since.entryId replay through get_entries
//
// *Manager therefore implements Supervisor plus the hub's CommandHandler, DialogHandler
// and Replayer; nothing reaches a package-level singleton.
//
// Two deliberate deviations from a literal reading of §5.3, both documented where they
// happen:
//
//   - Config carries no runtime-dir field. The per-session PI_UI_BRIDGE_CONFIG documents
//     of §5.7 live in RuntimeDir(), which implements the documented PIUI_RUNTIME_DIR rule,
//     so the CLI and this package can never disagree about the path.
//   - The frozen ws.Hub has no method to emit a `request` frame. A hub that implements the
//     optional DialogSender interface is used directly; any other publisher receives an
//     event of type "request" whose payload is the complete frame (see Manager.sendDialog).
package sessions
