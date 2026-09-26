// Package adversarial holds the black-box adversarial suite of the Phase 1 spike
// (docs/verification-report.md is its companion).
//
// The tests in this directory drive the real pipeline — the HTTP + WebSocket
// server, the sessions supervisor, the rpc bridge and the fake-pi harness — and
// they drive it through the exported seams only: the test files are an external
// test package (adversarial_test) on purpose, so an assertion can only observe
// what a client and a child process can observe. Nothing here reaches into
// internal state, no test talks to a model or to a network beyond loopback, and
// every test builds its own hub, supervisor, listener and children.
//
// The suite is organised by area, one file each:
//
//	framing_test.go     rpc-level framing against a real child process
//	pipeline_test.go    framing across the whole stdout → hub → WS path
//	ws_handshake_test.go handshake, auth, Host/Origin and frame validation
//	ws_stream_test.go   commands, dialogs, replay and the slow-consumer path
//	rest_test.go        REST auth, payload, limit and method abuse
//	lifecycle_test.go   SIGTERM reaping and crash classification
package adversarial
