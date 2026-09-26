// Package ws is the WebSocket hub of the pi-ui server: the /ws/v1 endpoint, the
// event fan-out, replay and the seams sessions plugs into.
//
// # What it owns
//
// The hub is the only writer of a client socket. Everything a session produces
// arrives through Publish, which stamps a global sequence number and a timestamp,
// keeps the event for replay and hands it to the subscribers of its session. One
// subscriber is one connection with one bounded queue, and a queue that overflows
// is a disconnect with a server.error{code:"slow_consumer"} frame instead of
// growing memory — a client that cannot keep up is told, not starved of the
// reason.
//
// # Seams
//
// Hub (docs/spike-interfaces.md §5.2) is the frozen surface the rest of the
// process uses; Handler slots are injected with SetCommandHandler,
// SetDialogHandler and SetReplayer, never taken from a global. Requester is the
// outbound dialog seam (SendRequest): it is a separate interface so the frozen Hub
// signature stays untouched, and the value returned by New implements both.
//
//	hub := ws.New(ws.Options{ServerVersion: version, PiVersion: piVersion})
//	hub.SetCommandHandler(sessions)  // sessions.CommandHandler
//	hub.SetDialogHandler(sessions)   // sessions.DialogHandler
//	hub.SetReplayer(sessions)        // sessions.Replayer
//	mux.Handle("/ws/v1", hub)
//
// # Wire contract
//
// schemas/ws.json is the source of truth for every frame, and the hub validates
// each inbound frame against the embedded copy of it (gen.SchemaJSON) before any
// handler runs. docs/ws-protocol.md is the prose version: handshake, auth rules,
// every frame shape, replay semantics, event names, error codes, defaults.
//
// # Concurrency
//
// One goroutine reads the socket, one writes it, one runs the heartbeat, plus one
// per replay and one per handler call. Locks are taken in a single order
// (hub.mu → connection.mu → subscription.mu → connection.sendMu), publishers
// never block on a client, and no handler is called with a hub lock held.
package ws
