# syntax=docker/dockerfile:1

# NetPulse server image: the web UI (app/) and the agent binaries are
# embedded into the Go binary the same way the release builds do it
# (.goreleaser.yaml + scripts/build-agents.sh). The agents are only payload:
# the server serves them for install, nothing agent-related runs here.
#
# Build args mirror the release ldflags; defaults produce a coherent
# dev-stamped image (version 0.0.0-docker, agents stamped 0.0.0-docker):
#
#   docker build -t netpulse .
#   docker build \
#     --build-arg VERSION=2.51.0 \
#     --build-arg BUILD_COMMIT=abc1234 \
#     --build-arg AGENT_VERSION=3.0.45 \
#     -t netpulse:2.51.0 .
#
# CI passes the real values on tag builds (.github/workflows/docker.yml).

ARG NODE_VERSION=24
ARG GO_VERSION=1.26

# ---- Frontend: app/ -> app/dist --------------------------------------------
FROM node:${NODE_VERSION}-alpine AS frontend
WORKDIR /src/app
COPY app/package.json app/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY app/ .
# npm run build = tsc -b && vite build (same as the workflows).
RUN npm run build

# ---- Agent binaries for the embed (scripts/build-agents.sh) ----------------
# Pure-Go cross compilation, so the same five binaries are embedded in both
# the amd64 and arm64 image builds. UPX is a build dependency only.
FROM golang:${GO_VERSION} AS agents
ARG AGENT_VERSION=0.0.0-docker
RUN apt-get update \
 && apt-get install -y --no-install-recommends upx-ucl \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY agent/ agent/
COPY scripts/build-agents.sh scripts/
# Relative output dir: build-agents.sh resolves it from the repo root.
RUN scripts/build-agents.sh "$AGENT_VERSION" out/agents

# ---- Server: frontend + agents embedded, release ldflags --------------------
FROM golang:${GO_VERSION} AS server
ARG AGENT_VERSION=0.0.0-docker
ARG VERSION=0.0.0-docker
ARG BUILD_COMMIT=unknown
WORKDIR /src
COPY agent/ agent/
COPY server-go/ server-go/
COPY --from=frontend /src/app/dist server-go/internal/staticspa/dist
COPY --from=agents /src/out/agents/ server-go/internal/agentbin/agents/
WORKDIR server-go
# Same ldflags as the netpulse build in .goreleaser.yaml.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w \
        -X github.com/gnacho/netpulse/server-go/internal/updater.BuildCommit=${BUILD_COMMIT} \
        -X github.com/gnacho/netpulse/server-go/internal/agentbin.EmbeddedAgentVersion=${AGENT_VERSION} \
        -X github.com/gnacho/netpulse/server-go/internal/httpapi.Version=${VERSION}" \
      -o /out/netpulse ./cmd/netpulse

# ---- Runtime ----------------------------------------------------------------
FROM alpine:3
LABEL org.opencontainers.image.title="NetPulse" \
      org.opencontainers.image.description="Self-hosted network monitoring: Go server with embedded web UI and agent binaries" \
      org.opencontainers.image.source="https://github.com/gnacho/netpulse"
# ca-certificates for outbound HTTPS (integrations, update checks), tzdata so
# TZ works, busybox wget for the healthcheck. openssh-client is needed: the
# gateway autodetect and per-router inventory call the ssh binary, and host
# key verification calls ssh-keyscan (the polling pool itself is pure Go,
# x/crypto). The server is self-contained otherwise; the updater cannot
# apply inside a container (no deploy/update.sh, no root unit) by design.
RUN apk add --no-cache ca-certificates tzdata openssh-client \
 && addgroup -g 1000 netpulse \
 && adduser -u 1000 -G netpulse -D -h /data -s /sbin/nologin netpulse \
 && mkdir -p /data \
 && chown netpulse:netpulse /data
ENV PORT=3000 \
    DATA_DIR=/data \
    # Fresh installs bind 127.0.0.1 by default; a container must listen on
    # all interfaces for published ports to work. Override back to 127.0.0.1
    # (docker run -e NETPULSE_BIND=127.0.0.1) if you front it with a proxy in
    # the same network namespace.
    NETPULSE_BIND=0.0.0.0
# Everything stateful (SQLite DB, session secret, generated SSH key, TLS
# certs) lives in /data — keep it on a volume or the server regenerates
# state on every recreate.
VOLUME /data
EXPOSE 3000
COPY --from=server /out/netpulse /usr/local/bin/netpulse
USER netpulse
WORKDIR /data
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:${PORT}/api/health" >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/netpulse"]
