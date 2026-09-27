# The reference image: one pi-ui server with pi and node inside it.
#
# Two stages, because the server is a static Go binary and nothing of its build belongs in
# the runtime image. The runtime carries what a *session* needs: node (pi is a Node CLI)
# and nothing else that is not a shell utility.
#
# The server runs as a non-root user whose uid is meant to be the host user's
# (`--user $(id -u):$(id -g)` in the compose file): the sessions must read and write the
# bind-mounted projects with the permissions the host already has, which is the whole point
# of mounting them same-path.
FROM golang:1.27-bookworm AS build

WORKDIR /src
# The module first, so a source change does not refetch the dependencies.
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
# CGO off keeps the binary static: the runtime image has no libc to fall back on.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/pi-ui ./cmd/pi-ui

FROM node:22-bookworm-slim AS runtime

# ripgrep is a documented dependency of the search surface; tini reaps what the server
# spawns if it is ever killed hard.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ripgrep ca-certificates tini git \
    && rm -rf /var/lib/apt/lists/*
RUN npm install -g @earendil-works/pi-coding-agent@0.87.1

# The bridge extension ships with the server: the image mounts the repository's bridge
# directory read-only and points --bridge at it.
COPY --from=build /out/pi-ui /usr/local/bin/pi-ui

# The user the sessions and the server run as, with a home so pi finds ~/.pi/agent.
#
# The base image already has a `node` user at 1000:1000, which is exactly the id a single
# user desktop uses, so it is *renamed and renumbered* instead of duplicated — a second
# account with the same id is how a container ends up with two homes and one uid.
ARG UID=1000
ARG GID=1000
RUN groupmod --new-name piui --gid "${GID}" node \
    && usermod --login piui --uid "${UID}" --gid "${GID}" --home /home/piui --move-home node \
    && mkdir -p /state && chown piui:piui /state /home/piui

USER piui
WORKDIR /home/piui

# The state database (devices, settings, audit) lives on a volume; the workspaces and the
# pi configuration are bind mounts, because they are the host's.
ENV PIUI_STATE_DIR=/state \
    PIUI_RUNTIME_DIR=/tmp/pi-ui-runtime

EXPOSE 8787
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s \
    CMD pi-ui status --state-dir /state >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/usr/bin/tini", "--", "pi-ui"]
CMD ["serve", "--addr", "0.0.0.0:8787", "--root", "/home/piui/Projects", \
     "--session-dirs", "/home/piui/.pi/agent/sessions", \
     "--bridge", "/opt/pi-ui-bridge/pi-ui-bridge.ts"]
