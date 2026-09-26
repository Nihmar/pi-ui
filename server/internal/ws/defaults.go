package ws

import "time"

// Defaults New applies to a zero Options field. They match the numbers published
// in docs/ws-protocol.md and in the core schema's limits map, so a client that
// hard-codes no limit still agrees with the server.
const (
	defaultReplayEvents = 2000
	defaultReplayWindow = 15 * time.Minute
	defaultHeartbeat    = 30 * time.Second
	defaultWriteTimeout = 10 * time.Second
	defaultSendBuffer   = 512
)

// Protocol level constants that are not configurable in the spike.
const (
	// path is the only endpoint the hub serves. ServeHTTP rejects every other
	// path, so mounting it at a different route fails loudly at the first
	// request instead of opening an unexpected socket.
	path = "/ws/v1"

	// protocolVersion is the WS protocol version hello must negotiate and
	// welcome echoes (schemas/ws.json pins it with const 1).
	protocolVersion = 1

	// maxMissedPongs is how many consecutive unanswered heartbeat pings close the
	// connection. Three intervals tolerate one lost pong and one slow round trip
	// without letting a half-open connection linger.
	maxMissedPongs = 3

	// closeCodeUnauthorized is the close status (and HTTP status before the
	// upgrade) reported for a failed handshake: no token, wrong token,
	// non-loopback peer, or a Host/Origin the server does not serve. 4401 is in
	// the private-use range, so it cannot be confused with a protocol status.
	closeCodeUnauthorized = 4401

	// maxInboundFrameBytes caps one inbound frame. The largest legitimate frame is
	// a command carrying a pasted message, so a megabyte is generous; anything
	// bigger is a client bug or an attempt to make the server allocate.
	maxInboundFrameBytes = 1 << 20

	// closeHandshakeGrace is how long the closing handshake waits for the peer's
	// answer before the socket is dropped. A cooperative peer answers in
	// microseconds; the one this path exists for (a slow consumer, a missed
	// heartbeat) never answers at all, and the library's own five second wait is
	// far too long to inherit.
	closeHandshakeGrace = 250 * time.Millisecond

	// closeGrace bounds the closing handshake. A peer that is on the other side of a
	// slow consumer or a missed heartbeat is not going to answer a close frame, and
	// shutdown has a two second budget (docs/spike-interfaces.md §12), so waiting
	// longer than this for it would trade a clean close for a slow shutdown.
	closeGrace = time.Second
)

// handshakeTimeout bounds how long the server waits for the first frame. A client
// that upgrades and then says nothing must not hold a slot, and the deadline is
// short because hello is trivial to send. It is a variable rather than a constant
// so the tests can shorten it; New copies it into the hub, so a running connection
// never reads it again.
var handshakeTimeout = 10 * time.Second
