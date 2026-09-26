// Package store is the SQLite state database of the server.
//
// Everything the server must remember across restarts lives here: devices and the
// admin password first, then sessions, cursors, settings, uploads and the audit
// trail as their workstreams land. Nothing in this package talks HTTP or spawns a
// process; it opens one file, applies the migrations and hands typed stores to the
// services that need them.
//
// The schema is versioned with SQLite's PRAGMA user_version: a new table is a new
// entry in migrations, never an edit of an applied one. The file uses WAL and a
// busy timeout, and one connection at a time, which is all a single-process server
// needs and the least surprising behaviour for modernc's pure-Go driver.
package store
