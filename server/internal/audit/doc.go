// Package audit is the append-only trail of what happened to the server.
//
// The contract is deliberately small and one-way:
//
//   - transports (REST, WebSocket) describe an [Event] and hand it to a
//     [Recorder]; the recorder never blocks a request on a slow disk for longer than
//     one insert, and a failing store is logged instead of breaking the action it was
//     describing;
//   - the trail is sensitive: it names devices, sessions, paths and outcomes, so it is
//     readable only with the admin scope and pruned after a retention window;
//   - it never carries provider secrets, tokens or conversation content, and lengths
//     are bounded so a hostile value cannot bloat the file.
//
// The vocabulary lives here as typed constants, but the wire accepts any dotted name:
// a new audited action is a new constant plus a call site, never a schema change.
package audit
