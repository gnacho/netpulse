# Docker

Docker is an **optional** install path for the server — the project's
position is "Docker is optional, never a requirement"
([external integrations](external-integrations.md)), and the
systemd/binary install remains the primary documented one. This page is for
people who run everything in containers.

The image ships the Go server with the embedded web UI **and** the embedded
agent binaries, built with the same flags and version stamps as the release
tarballs. The agents are payload only: the server serves them so you can
install agents from the UI — nothing agent-related runs inside the
container.

## What you get in the image

- Server + web UI, non-root user (uid/gid 1000), `/data` as the state volume.
- Agent binaries for all five architectures embedded and version-coherent
  with the server: after upgrading the server image, existing agents see
  `update available` and self-upgrade from the server.
- Healthcheck on `/api/health` (busybox `wget`, no extra tools).
- `linux/amd64` and `linux/arm64` images on GHCR.

## Quickstart: prebuilt image

```bash
docker run -d --name netpulse \
  -p 127.0.0.1:3000:3000 \
  -e AUTH_PASS=choose-one \
  -v netpulse-data:/data \
  --restart unless-stopped \
  ghcr.io/gnacho/netpulse:latest
```

Open `http://localhost:3000` on the docker host and log in with `AUTH_PASS`.
`AUTH_PASS` is required at boot (at least 10 characters — the server refuses
to start without it); to reach the panel from other machines on the LAN,
publish on the host interface instead of loopback
(`-p 3000:3000`, or `-p <lan-ip>:3000:3000`). On first boot the server
generates its SSH keypair into `/data/.ssh` and auto-detects the gateway,
same as the binary install.

## Quickstart: compose, from this checkout

```bash
docker compose up -d --build      # builds the local image netpulse:local
```

Compose reads an optional `.env` file next to `docker-compose.yml` and passes
it to the container — set `AUTH_PASS` there before the first start (the
server will not boot without it). The port is published on the docker
host's loopback by default. Useful variables for compose itself:

| Variable | Default | Purpose |
|---|---|---|
| `NETPULSE_PORT` | `3000` | Host port mapped to the container's 3000 |
| `NETPULSE_PUBLISH_ADDR` | `127.0.0.1` | Host interface the port is published on; `0.0.0.0` exposes it on all interfaces |
| `NETPULSE_IMAGE` | `netpulse:local` | Image to run; set it to a GHCR tag to skip the build |
| `TZ` | `UTC` | Container timezone |

## Configuration

The container uses the same environment as the binary install — see
[`server-go/.env.example`](../server-go/.env.example). The essentials:

- `AUTH_PASS` — required to protect the web session (hash is persisted in
  the DB after the first login).
- `DEMO_MODE=1` — sample dataset, handy for a first look.
- `NETPULSE_PUBLIC_URL` — set it when agents must reach the server under a
  different address than the one they discover (used by agent install and
  auto-reinstall).
- `NETPULSE_TLS_ENABLED=1` + `NETPULSE_TLS_PORT=3443` — enable the extra
  HTTPS listener and map the port in compose (`-p 3443:3443`).

Do not change `PORT` or `DATA_DIR` in the container: the healthcheck and the
volume layout assume the defaults; map a different host port with
`NETPULSE_PORT` instead. `NETPULSE_BIND` ships as `0.0.0.0` because a
container must listen on all interfaces for published ports to reach the
fresh-install loopback default; setting it back to `127.0.0.1` locks the
listener to the container's loopback (useful only with an in-container
proxy).

## Data and persistence

Everything stateful lives in the `netpulse-data` volume at `/data`: the
SQLite database, the auto-generated session secret, the server SSH keypair
and any TLS certificates. Keep the volume across recreations or the server
starts from scratch. Bind mounts work too (`-v /srv/netpulse:/data`); make
the host directory writable by uid 1000.

## Updates

Image deploys update by pulling a new image and recreating the container —
the data volume survives:

```bash
docker pull ghcr.io/gnacho/netpulse:latest
docker compose up -d             # with compose
# or, plain docker:
docker stop netpulse && docker rm netpulse && <your docker run line again>
```

The in-app updater is inert here by design: without the repo's
`deploy/update.sh` or the root systemd unit it reports available versions
but cannot apply them (`canApply: false`), so the fleet upgrade path is
image tags. The embedded agents are unaffected: they keep self-upgrading
from the running server, which stays version-coherent with whatever image
you run.

## Notes

- The collector (TCP latency sidecar) is not part of this image or the
  compose file; keep using its binary/systemd install for now.
- `armv7` images are not published (the release binaries cover it); if you
  need one, `docker build --platform linux/arm/v7 .` works from a checkout.
- Building locally takes a few minutes the first time: the frontend, five
  agent architectures and the server are all compiled inside the image
  (UPX included), so no toolchain is needed on the host.
