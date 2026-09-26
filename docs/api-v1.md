# REST API v1

**Status:** the conventions, `/health`, `/server` and the auth surface are normative
here and in `schemas/server.json`, alongside the filesystem, git, search, settings,
tasks and drain surfaces documented below. The DTOs of those later endpoints are Go
types (`internal/fs`, `internal/git`, …) that the schema still has to absorb: until they
do, this document is their contract and a change here is a change to the wire surface.
Where a Go type and this document disagree, the document wins and the doc is fixed in
the same commit; every later endpoint (sessions,
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
The WebSocket handshake uses the same header, the same tokens and the same
scopes (`docs/ws-protocol.md`, "Device tokens and scopes").

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

### Operate it from the CLI

The device records, the admin password and the pending invitations live in
`$PIUI_STATE_DIR/state.db` (default `$XDG_STATE_HOME/pi-ui/state.db`), one SQLite
file the running server and the one-shot commands share:

```bash
pi-ui auth set-password                 # store the admin password verifier
pi-ui pair --url http://<host>:8787     # mint an invitation (code + QR)
pi-ui status                            # devices, password, pending invitations
```

A server started with nothing configured mints one invitation itself and logs its
code. `serve --token <token>` selects the static-token mode instead.

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
| `device_limit` | 409 | the maximum of paired devices is reached; revoke one first |
| `rate_limited` | 429 | pairing or command rate limit, with `Retry-After` |
| `feature_disabled` | 403 | an administrative setting turned the capability off (`git.write`) |
| `path_escape` | 403 | a path or directory outside every workspace |
| `unavailable` | 503 | the server is draining (or shutting down): it takes no new session |
| `bad_request` | 400 | body not readable or missing a required field |
| `too_large` | 413 | body above the 1 MiB cap |

A failed pairing never says which part was wrong (code vs secret vs expiry):
the failure is one `unauthorized`, and the attempt is audited.

### Rate limits

Token buckets: a burst the client may spend at once, then the sustained rate. A
refused request is `429 rate_limited` with `Retry-After` (seconds) and one
`rate.limited` entry in the audit trail.

| Budget | Default | Key | Flag |
|---|---|---|---|
| REST requests | 120/min | device, or peer host without a token | `--rate-rest` |
| Token refresh | 10/min | device | `--rate-refresh` |
| WebSocket connects | 10/min | token (hashed), or peer host | `--rate-ws` |
| Prompts | 30/min | session | `--rate-prompt` |

Pairing keeps its own budget in the auth service (5 attempts/min/IP with a
lockout) and answers `401 unauthorized`, never leaking which part failed. A
budget of `0` disables it. The burst is a quarter of the rate (at least one), so
`120/min` allows 30 requests back to back on a cold bucket.

## Audit

```
GET /api/v1/audit?since=<RFC3339>&action=<name>&deviceId=<id>&sessionId=<id>&limit=<n>
```

Scope `admin`. Returns `SrvAuditResponse`: the newest entries first, and
`truncated:true` when more rows matched than the page returned (`limit` defaults to
100, at most 1000). An unknown query parameter is ignored, so a newer client never
breaks an older server.

| Field | Meaning |
|---|---|
| `id`, `at` | row id and when the server observed the action |
| `action` | dotted name (`auth.pair`, `auth.denied`, `device.revoke`, `session.create`, `session.stop`, `session.prompt`, `session.steer`, `session.follow_up`, `session.abort`, `session.rename`, …). The vocabulary grows without a schema change; a client renders an unknown action generically. |
| `outcome` | `ok` (it happened), `denied` (a policy decision: the credential or the scope refused it before anything ran) or `error` (attempted and failed) |
| `actorDeviceId`, `actorName`, `actorScope` | who acted; the name is copied at the time, so a rename does not rewrite history |
| `sessionId`, `target` | what it acted on |
| `remoteAddr` | peer host, never a forwarded header |
| `details` | structured context (`op`, `method`, `path`, `reason`, counts). Never tokens, provider secrets or conversation content. |

The trail is append-only in the state database and is pruned by retention (30 days by
default, opportunistically on write, at most hourly). A failing audit store is logged;
it never turns the action it describes into an error, and a row nobody can decode is
returned with empty `details` instead of failing the page.

`auth.denied` is recorded for a refused credential, for a missing scope on REST and on
the WebSocket handshake, and for a frame above the connection's scope.

## Server

### `GET /health`

No credential, no state: `{"status":"ok"}`. Used by the app before pairing and
by the deployment's liveness probe. Never leaks cwd, names or versions.

### `GET /server`

Scope `viewer`. A loopback peer without a token is the **operator** until the
server is configured (no device paired, no admin password), which is the
bootstrap state the plan describes; once an identity exists it is only a viewer,
and a token is what unlocks operator and admin from anywhere. Returns
`SrvServerIdentity`: version, `piVersion`, protocol, `features[]`, `limits{}`
and `tls` when the server terminates TLS. The app uses it to negotiate
capabilities and to confirm a certificate fingerprint (TOFU/pin).

## Files and workspaces

The server exposes the host filesystem **only inside the workspaces the operator
configured** (`serve --root /path/to/projects`, repeatable). Confinement happens in
`internal/fs` on the *resolved* path, so a symlink inside a root that points outside
it is refused like any other escape (`path_escape`, HTTP 403, audited as
`path.escape.blocked`). The client never receives bytes above the read cap; a larger
file is a `too_large` and belongs to the download endpoint when that lands.

| Method | Path | Scope | Notes |
|---|---|---|---|
| GET | `/api/v1/workspaces` | viewer | `{"roots":[{"id","path"}]}`: the allowed roots, first one is the default |
| GET | `/api/v1/fs/list?path=` | viewer | `{"entries":[…]}` — directories first, then names, case-insensitive |
| GET | `/api/v1/fs/stat?path=` | viewer | one `FsEntry` |
| GET | `/api/v1/files/read?path=&maxBytes=` | viewer | `{"entry","text"}` for UTF-8, `{"entry","base64"}` otherwise |
| PUT | `/api/v1/files/write` | operator | `{path, text\|base64, expectedSha256?}` → the written `FsEntry` |
| DELETE | `/api/v1/files/delete` | operator | `{path, recursive?}` → `{"removed":path}` |

`FsEntry` is `{name, path, rel, rootId, isDir, size, mode, modTime, sha256?}`: absolute
host path, path relative to the root that contains it, and the content hash (files up
to the read cap) that a conditional write sends back.

A conditional write — `expectedSha256` set to the hash the client read — is refused
with `bad_request` when the file changed in between, and it never creates a file the
client believed existed. A mutation is audited (`file.write`, `file.delete`) with the
path as its target; a refused escape is audited as `path.escape.blocked` with
`outcome: denied`.

A server started without `--root` answers `501 unsupported` on every one of these
endpoints instead of exposing the whole filesystem by accident. A root that does not
exist, or is not a directory, is refused at startup: a workspace is a promise the
server makes to its clients, not something discovered later.

## Git

Reads are `viewer`; the two mutations are `operator` **and** need the `git.write`
setting to be on (off by default: writing a repository is a deliberate decision, and a
server with no state directory is not gated). Both are audited as `git.write` (a refused directory is `path.escape.blocked`). Every `dir` is resolved
through the same workspace confinement as the filesystem endpoints, so git can only
run inside `--root` directories; the commands are `git -C <dir>`, never the server's
own working directory, and a hook that hangs is a `timeout` instead of a held
request.

| Method | Path | Scope | Notes |
|---|---|---|---|
| GET | `/api/v1/git/status?dir=` | viewer | `{repo, branch, detached, ahead, behind, clean, changes:[{path,status,staged,unstaged}]}` |
| GET | `/api/v1/git/log?dir=&limit=` | viewer | `{commits:[{hash,short,subject,author,at}]}`, newest first, 30 by default |
| GET | `/api/v1/git/diff?dir=&path=&staged=` | viewer | `{diff,staged}` — unified diff, capped at 1 MiB |
| POST | `/api/v1/git/stage` | operator | `{dir, paths:[…]}` |
| POST | `/api/v1/git/commit` | operator | `{dir, message}` → the new commit |

A git failure keeps git's own words: `not a git repository`, `nothing to commit` and
the like come back as a `bad_request` whose message is the stderr, because that is
the part a user can act on. Without a workspace configured these endpoints answer
`501 unsupported`.

## Search

`viewer`, like every other read. Two halves, selected with `scope` (default both):
`files` runs ripgrep inside a workspace directory, `messages` scans the pi session
JSONL files the server was pointed at with `--session-dirs`. The session files are
allowed on their own — they live outside the workspaces by design — and a query with
no workspace configured is a `501 unsupported` rather than an empty answer.

```
GET /api/v1/search?q=<text>&scope=files,messages&cwd=<dir>&limit=50&case=sensitive
```

```json
{"hits":[
  {"kind":"file","path":"/srv/projects/app/main.go","rel":"main.go","rootId":"app",
   "line":3,"column":6,"text":"func helloWorld() {}"},
  {"kind":"message","path":"/home/u/.pi/agent/sessions/…jsonl","text":"…the RpcBridge…",
   "sessionId":"01a0dccc","role":"user","at":"2026-09-26T12:00:00.000Z"}
]}
```

A query shorter than two characters is a `bad_request` (a one-letter search would
walk every tree for nothing), a directory outside the workspaces is a `path_escape`,
and ripgrep finding nothing is an empty `hits` array, not an error. A host without
ripgrep answers `unsupported` when the file half is asked for.

## Settings

The server's own policy, in one catalogue (`internal/settings`). Any device reads it;
only an admin changes it, and every change is audited as `settings.update`. A change
publishes `server.settings.changed` on the WebSocket, so a settings screen notices an
edit made from another device. A key this server does not know is a `bad_request`
rather than a value stored under a name nothing reads.

| Method | Path | Scope | Notes |
|---|---|---|---|
| GET | `/api/v1/settings` | viewer | `{values, defaults, known:[{key,kind,default,description}]}` |
| PATCH | `/api/v1/settings` | admin | a partial map, `{"git.write":true,"terminal.maxSessions":8}` → `{applied, values}` |
| DELETE | `/api/v1/settings/{key}` | admin | back to the default → `{reset, values}` |

Keys:

| Key | Kind | Default | Effect |
|---|---|---|---|
| `git.write` | bool | `false` | allows `POST /git/stage` and `/git/commit`; off answers `feature_disabled` (403) |
| `terminal.maxSessions` | int 1–32 | `4` | PTY terminals one client may hold open |
| `session.idleTimeout` | duration string | `"1h"` | wraps up an idle session; `"0"` disables the watchdog |
| `session.wrapUpPrompt` | string | `""` | what an idle session is asked before it stops |
| `audit.retentionDays` | int 1–3650 | `30` | how long the audit trail is kept |

A value is validated before it is written (type, range, duration syntax) and a
`PATCH` carrying one invalid entry changes nothing at all. A server started with
`--token` instead of a state directory answers `501 unsupported` here: it has nowhere
to remember a setting, and its scope rules alone decide what a token may do.

## Tasks

Background commands on the host — a build, a test run, a dev server — started and
stopped with `operator`, read with `viewer`, and audited as `task.start` / `task.stop`.
A task is not a session: it has no model and no conversation. Like every other host
capability it goes through the workspace confinement, so a directory outside the roots
is a `path_escape` and no process starts.

| Method | Path | Scope | Notes |
|---|---|---|---|
| GET | `/api/v1/tasks` | viewer | `{tasks:[…]}`, newest first |
| GET | `/api/v1/tasks/{id}` | viewer | `{task, output}` — the kept output of the task |
| POST | `/api/v1/tasks` | operator | `{name?, command, args?[], dir}` → the task (201) |
| POST | `/api/v1/tasks/{id}/stop` | operator | SIGTERM to the process group, SIGKILL after a grace period |

A `Task` is `{id, name, command, args, dir, pid?, status, exitCode?, startedAt, endedAt?,
truncated, owner?}`. `status` is `running`, `exited`, `failed` (a non-zero exit) or
`stopped` (somebody asked). The output is a ring of the last 256 KiB with
`truncated: true` once bytes were dropped, because the end of a build is what someone
reads. `command` and `args` are kept apart and executed directly, never through a
shell. The concurrency limit is 4 and a stopped task frees its slot; a change publishes
`server.tasks.changed` on the WebSocket.

## Drain

An operational pause of the front door, not of the children: an admin stops the server
taking **new** sessions while everything already running stays running. It is what makes
a `systemctl restart` (or a deploy) a staged operation instead of a killing spree.

| Method | Path | Scope | Notes |
|---|---|---|---|
| POST | `/api/v1/drain/start` | admin | `{draining:true, running:<n>}` — `POST /sessions` answers `503 unavailable` until it is resumed |
| POST | `/api/v1/drain/resume` | admin | `{draining:false}` |

`GET /api/v1/server` reports the state in `limits` (`draining`, `drainingSince`) so a
client can explain a refused create instead of showing a bare error. Both operations are
audited as `drain.start` / `drain.resume` with the number of sessions running at the
time; a drain never stops or evicts a session.
