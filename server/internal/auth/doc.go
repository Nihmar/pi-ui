// Package auth owns device identity: pairing invitations, device tokens, scopes,
// rotation and revocation (docs/api-v1.md, PLAN.md §4.2).
//
// The seam is deliberate:
//
//   - the REST handlers in internal/api decode and validate the wire DTOs and call
//     this package, which never imports HTTP;
//   - Store persists what must survive a restart (devices, the admin password
//     hash); MemoryStore ships first, the SQLite implementation lands behind the
//     same interface;
//   - clock and randomness are injected, so tests are deterministic and the argon2id
//     cost is cheap in tests and real in production.
//
// Security rules the package exists to keep:
//
//   - only hashes are stored (argon2id for the token secret and the admin
//     password); a database leak hands out no working credential;
//   - a token is <deviceId>.<secret>: the public id makes verification O(1) while
//     the secret is what the hash protects;
//   - pairing invitations are single-use and short-lived, and a failed attempt is
//     indistinguishable from any other failure to the client (one unauthorized);
//   - pairing and password attempts are rate-limited per caller key.
package auth
