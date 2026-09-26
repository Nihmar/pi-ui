# REST API v1

**Status:** the conventions, `/health`, `/server` and the auth surface are
normative here and in `schemas/server.json`; every later endpoint (sessions,
files, git, terminal, search, tasks, MCP, updates, settings, audit) is added to
this document and to the schema in the same commit as its handler.
**Source of truth:** `schemas/server.json` for the DTOs and
`docs/spike-interfaces.md` §5.4 for the router seam. Where this document and the
schema disagree, the schema wins and this document is fixed in the same commit.

## Conventions

- Base path `/api/v1`. Method + path are unique; a wrong method on a known path
  is `405`, an unknown path is `404`, both with the JSON error envelope.
- Every answer carries `X-Piui-Protocol: 1` and a JSON body
  (`Content-Type: application/json`), including for errors.
- Errors are `{"error":{"code","message"}}` with the taxonomy of
  `schemas/core.json`. Clients branch on `code`, never on `message`.
- Timestamps are RFC3339 UTC with milliseconds; ids are opaque strings
  (`s_…` sessions, `d_…` devices) and are never parsed by a client.
- Request bodies are capped at 1 MiB; a larger body is `413 too_large`.
- The client never receives provider secrets: keys and headers stay server-side.

## Authentication

Every request except `/health` and `POST /auth/pair` needs a bearer token:

```
Authorization: Bearer <device token>
```

A token is minted by pairing, stored by the client in the OS keystore and
**only its hash** is kept server-side (argon2id). A device token is
`<deviceId>.<secret>`: the public device id makes verification O(1), the secret
is what the hash protects. Tokens rotate on refresh and die on revocation.

### Scopes

| Scope | Grants |
|---|---|
| `viewer` | read: sessions, entries, files, git status/log, search, logs |
| `operator` | viewer **plus** driving: prompt/steer/abort, bash, PTY, file write, git write (when enabled) |
| `admin` | operator **plus** server management: devices, settings, MCP, updates, drain, audit, packages |

A pairing with a code yields `operator`; only the admin-password branch yields
`admin`. `viewer` is granted deliberately (a read-only device) and is never a
downgrade of an operator device by accident: the scope is stored per device.

### Pairing flow

1. The server creates an **invite**: a one-time code (typed fallback, 6 digits)
   plus a high-entropy secret (QR only), with a 10-minute TTL. `pi-ui status`
   prints it and the admin page renders it as a QR.
2. The app scans the QR or the user types the code, confirms the server origin
   and the TLS fingerprint, then calls the endpoint below.
3. The server burns the invite and returns the device token exactly once.

```
POST /api/v1/auth/pair
```

```json
{
  "deviceName": "Alessandro's Pixel 9",
  "platform": "android",
  "code": "7K4M2Q",
  "secret": "…"            // present only when the QR was scanned
}
```

The admin-password branch uses the same endpoint:

```json
{
  "deviceName": "pi-ui admin",
  "password": "…"
}
```

Response `201`:

```json
{
  "deviceId": "d_4f2a9c118e73b052",
  "token": "d_4f2a9c118e73b052.…",
  "scope": "operator",
  "expiresAt": "2026-10-26T12:00:00.000Z",
  "server": {
    "version": "0.1.0",
    "piVersion": "0.87.1",
    "protocol": 1,
    "features": ["sessions", "replay"],
    "limits": {"maxSessions": 8, "pairingTtlSec": 600},
    "tls": {"fingerprintSha256": "4f2a…"}
  }
}
```

The QR invite is a deep link the client parses itself; it is not a server DTO:

```
piui://pair?v=1&url=http%3A%2F%2Fpi-ui.local%3A8787&code=7K4M2Q&fp=4f2a…
```

`code` and `secret` above are the short-lived invite; the QR never contains a
device token.

### Refresh and revocation

```
POST /api/v1/auth/refresh        (authenticated)
→ 200 {"token": "…", "expiresAt": "…"}
```

The device id and scope survive; the previous token stops working as soon as
the new one is written (rotation, not extension).

```
GET    /api/v1/auth/devices      (admin) → {"devices": [...]}
DELETE /api/v1/auth/devices/{id} (admin) → 204
```

Revocation is immediate and also closes that device's WebSockets. A device can
list itself: `current: true` marks the caller without exposing ids it cannot
see.

### Errors

| Code | Status | When |
|---|---|---|
| `unauthorized` | 401 | missing/unknown/expired/revoked token, bad or consumed pairing code, wrong password |
| `forbidden_scope` | 403 | the device's scope does not cover the endpoint |
| `rate_limited` | 429 | pairing or command rate limit, with `Retry-After` |
| `bad_request` | 400 | body not readable or missing a required field |
| `too_large` | 413 | body above the 1 MiB cap |

A failed pairing never says which part was wrong (code vs secret vs expiry):
the failure is one `unauthorized`, and the attempt is audited.

### Rate limits and audit

- Pairing: 5 attempts/min/IP, then a lockout. Refresh: 10/min/token.
- REST commands: 120/min/token (burst 30); `prompt` 30/min/session.
- Audit events: `auth.pair`, `auth.denied`, `device.revoke`.

## Server

### `GET /health`

No credential, no state: `{"status":"ok"}`. Used by the app before pairing and
by the deployment's liveness probe. Never leaks cwd, names or versions.

### `GET /server`

Scope `viewer` (or none before pairing, for the origin check). Returns
`SrvServerIdentity`: version, `piVersion`, protocol, `features[]`, `limits{}`
and `tls` when the server terminates TLS. The app uses it to negotiate
capabilities and to confirm a certificate fingerprint (TOFU/pin).
