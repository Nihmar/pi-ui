# Security model

What this deployment trusts, what it never reveals, and where each rule lives in the code.
It is the document the plan's Phase 0 asked for, written after the capabilities exist
rather than before, so every sentence can point at a test.

## What pi-ui is

A **server companion**: one Go process that orchestrates a `pi --mode rpc` child per
session on a host, and a Flutter client that drives it. The server is the only thing that
ever holds a provider key or a device token hash; the client is a remote control.

Two boundaries follow from that:

- **pi is orchestrated, never forked.** The server speaks JSONL to the child and forwards
  `pi.*` payloads verbatim. It adds capabilities pi does not have (files, git, search, PTY,
  tasks, MCP configuration, updates) as services of its own, each one confined to the
  workspaces the operator allows.
- **The client never sees a secret.** Provider keys, tokens and provider headers stay
  server-side; a device token is stored in the OS keystore and the server keeps only its
  argon2id hash.

## Trust, from the outside in

| Layer | Rule | Where |
|---|---|---|
| Network | `--allow-ips` refuses a peer outside the list with `403 forbidden_scope` **before** any handler; the refusal is audited | `internal/cli/allowips.go` |
| Transport | `--tls-cert`/`--tls-key` terminate TLS 1.2+; `GET /server` reports the leaf's SHA-256, which the app pins at pairing | `internal/tls`, `internal/api` |
| Host header / Origin | the WebSocket handshake accepts only the addresses and origins the operator allows | `--allow-hosts`, `--allow-origins` (`internal/ws`) |
| Identity | a device token is `<deviceId>.<secret>`; verification is O(1) on the id and constant-time on the hash; revocation closes the device's sockets immediately | `internal/auth`, `internal/ws` |
| Scope | `viewer` reads, `operator` drives sessions and the host, `admin` manages the server itself; every request is authorized against the stored scope | `internal/api`, `internal/ws` |
| Policy | `feature_disabled` is the server refusing by configuration (`git.write` off), which is not the same as a scope refusing | `internal/settings` |
| Path | every host path is resolved (symlinks included) and confined to a `--root`; a path outside is `path_escape`, audited as `path.escape.blocked` | `internal/fs` |
| Process | a PTY or a task gets its own process group and is signalled as a group, so a child does not outlive it | `internal/terminal`, `internal/tasks` |
| Session | `--isolate <image>` wraps each child in `docker run` with every path mounted same-path: what a session can reach is what was mounted, not the host's whole filesystem | `internal/sessions/isolation.go` |
| Rate | REST, refresh, WebSocket connects and prompts each have a budget; a refusal is `429` with `Retry-After` and an audit entry | `internal/ratelimit` |
| Content | logs never carry tokens or conversation content; the client only ever receives nicknames for providers | `internal/obs`, every handler |

## The pairing flow

1. `pi-ui pair` mints a one-time invitation: a six-digit code (typed) plus a high-entropy
   secret (QR only), ten minutes of TTL.
2. The app confirms the server origin and, when TLS is self-signed, the fingerprint
   (`pi-ui tls fingerprint` prints the same value).
3. `POST /auth/pair` burns the invitation and returns the device token exactly once. A
   wrong code, a consumed one and an expired one are one `unauthorized`: the failure never
   says which part was wrong.
4. The token goes into the OS keystore; the profile (URL, device id, scope, pinned
   fingerprint) goes into plain preferences. A token refresh rotates the secret and keeps
   the device id.

## What a compromised client can do

| It holds | It can | It cannot |
|---|---|---|
| A `viewer` token | read sessions, files, git state, search results | drive anything, change a setting, run a command |
| An `operator` token | prompt, steer, abort, run tasks and PTYs, write files inside the roots, and mutate git **when `git.write` is on** | manage devices, MCP, updates, settings, drain, audit |
| An `admin` token | everything above plus the server's own policy | read a provider key: none is ever stored in a form a client can fetch |
| The server's filesystem access | whatever the workspaces and the host permissions allow | leave a `--root` (a symlink is resolved before the check) |

A token that leaks is bounded by two things: the sliding expiry and revocation, which takes
effect immediately — a revoked device's WebSockets are closed by the server, not by the
client's cooperation.

## What is deliberately not defended

- **A hostile host.** The server runs with the operator's privileges and so do the children
  and the bridge extension; a user who can start a server can already run code there.
  `--isolate <image>` runs each session in its own container instead (every path mounted
  same-path, the working directory first, the pi configuration and the bridge reachable, the
  container disposable), which bounds a session to what it was mounted. The network mode is
  `host` by default so a session can reach a model server on the machine; `bridge` and `none`
  are one flag away. The server itself still runs with the operator's privileges, and a
  container spawned *by* a containerised server cannot reuse its mounts — `deploy/README.md`
  says which deployment isolates and which does not.
- **A hostile `pi` or provider.** The server forwards what the child says, including a
  malformed line (`pi.unknown`), and never interprets it as instructions to itself.
- **Traffic analysis on plain HTTP.** Without TLS, everything but the credentials is
  readable on the network: a deployment on a network it does not trust terminates TLS or
  sits behind a VPN/Tailscale (the deployment notes cover the matrix).

## Testing

Every row of the tables above has a test next to it — `internal/fs` for confinement
(symlinks included), `internal/cli` for TLS, the allowlist and the audit of a refusal,
`internal/auth` for pairing and rotation, `internal/ws` for scopes and revocation,
`test/adversarial` for the protocol-level attacks (framing, slow consumers, refused
handshakes), and `test/e2e` for the real-pi paths.
