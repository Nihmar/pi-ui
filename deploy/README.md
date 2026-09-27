# Deployment

Three ways to run a server, in decreasing order of convenience: Docker Compose (the
reference), the static binary under systemd, and a bare `./pi-ui serve` for a spike.

The matrix that decides between them is **who can reach the port**:

| Network | What to do | Why |
|---|---|---|
| Loopback (`127.0.0.1:8787`) | nothing else | the default; a loopback peer without a token is the operator until a device is paired |
| LAN (`192.168.x.x`) | terminate TLS (`--tls-cert/--tls-key`) and pin the fingerprint in the app | plain HTTP on a LAN is readable by everything on it; a device token alone is not enough against a hostile neighbour |
| VPN or Tailscale | plain HTTP over the tunnel is acceptable | the tunnel already authenticates and encrypts; there is nothing to pin |
| Public HTTPS | a reverse proxy (Caddy, nginx + certbot) terminating TLS, `--allow-ips` allowing only the proxy | a public port is scanned within minutes; the proxy is the peer the server sees, so it is the one to allow |

In every case the app is the only client that needs the port: it pairs once, stores the
device token in the OS keystore, and pins the certificate fingerprint it was shown
(`docs/security.md`).

## Docker Compose (reference)

```bash
# From the repository root.
HOST_UID=$(id -u) HOST_GID=$(id -g) docker compose -f deploy/docker-compose.yml up -d --build
docker compose -f deploy/docker-compose.yml logs -f pi-ui
```

What the compose file does, and why each part is not optional:

- **The projects are mounted same-path** (`${HOME}/Projects:${HOME}/Projects`). A session's
  working directory is its identity: if `/home/you/Projects/app` were mounted at
  `/projects/app`, every path pi reported and every path a client typed would differ, and
  the filesystem surface would be about a tree nobody asked for. Mount the host path at the
  same path.
- **`~/.pi/agent` is mounted read-write**, so sessions, credentials and settings are the
  host's own: a session started here is the one `pi` in a terminal would resume.
- **The state database is a volume** (`/state`): devices, settings and the audit trail
  belong to the deployment, not to a container run.
- **The port is bound to `127.0.0.1`**, not to `0.0.0.0`: publishing a plain-HTTP server to
  a network is a decision, and it should be made in front of the compose file.
- **The container runs as your uid** (`--user`), because the bind mounts are your files.

Health: `docker compose exec pi-ui pi-ui status` prints the devices, the admin password
state and the pending invitations.

## systemd (static binary)

```bash
CGO_ENABLED=0 go build -o pi-ui ./cmd/pi-ui          # in server/
sudo install -m 0755 pi-ui /usr/local/bin/pi-ui
sudo install -m 0644 deploy/systemd/pi-ui.service /etc/systemd/system/pi-ui@.service
sudo install -Dm 0644 bridge/pi-ui-bridge.ts /usr/local/share/pi-ui/pi-ui-bridge.ts
sudo systemctl enable --now pi-ui@$USER
journalctl -u pi-ui@$USER -f
```

The unit is a template (`pi-ui@<user>`) so the server runs as the user who owns the
projects. It hardens the process (`ProtectSystem=strict`, `PrivateTmp=yes`,
`NoNewPrivileges=yes`) while still allowing a PTY and a background task to work, and it
keeps the state in `%S/pi-ui` (`/var/lib/pi-ui` for a system user).

## TLS

Self-signed, for a LAN or a test:

```bash
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -days 825 -nodes \
  -keyout cert.key -out cert.pem -subj "/CN=pi-ui.local" -addext "subjectAltName=DNS:pi-ui.local,IP:192.168.1.20"
./pi-ui serve --root ~/Projects --tls-cert cert.pem --tls-key cert.key
./pi-ui tls fingerprint --cert cert.pem    # what the app shows when it asks to pin
```

The app compares that fingerprint with what the server reports in `GET /server` (and in
every pairing answer). Pin it once and a certificate that changes later is a hard failure,
not a prompt — which is the point of pinning a self-signed certificate at all.

Public HTTPS, behind a proxy (Caddy is the shortest path):

```
pi-ui.example.com {
    reverse_proxy 127.0.0.1:8787
}
```

The proxy terminates TLS, so the server reports no `tls` block and the app pins the proxy's
certificate, which is the one it saw. Allow only the proxy: `--allow-ips 127.0.0.1`.

## Backups

The state database is the only thing that must be kept: `$PIUI_STATE_DIR/state.db` (devices,
settings, audit). Conversations are pi's own session JSONL under `~/.pi/agent/sessions`, and
the workspaces are the host's — the server never owns either.
