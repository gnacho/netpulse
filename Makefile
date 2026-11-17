# Local mirror of the .github/workflows/pr.yml checks.
# Keep this in sync with the workflow. Run `make check` before pushing.

SHELL := /bin/bash
AGENT_VERSION ?= 0.0.0-dev
GO ?= go
NPM ?= npm
AGENT_BIN := server-go/internal/agentbin/agents

.PHONY: check frontend agents server agent collector clean

check: frontend agents server agent
	@echo "check: all steps passed"

frontend:
	cd app && $(NPM) ci --no-audit --no-fund
	cd app && $(NPM) run lint
	cd app && $(NPM) run build
	rm -rf server-go/internal/staticspa/dist
	cp -r app/dist server-go/internal/staticspa/dist

# The agent binaries are go:embed inputs of the server, so they must exist
# before the server compiles. scripts/build-agents.sh builds the five
# architectures (normalizeArch names: armv7 is "arm" with GOARM=7, MIPS is
# softfloat), packs them with UPX and verifies the result (#1890: an unpacked
# agent needs twice its size free to upgrade on a router). It needs upx in
# PATH; UPX_BIN=/path/to/upx points it elsewhere.
agents:
	GO=$(GO) scripts/build-agents.sh $(AGENT_VERSION) $(AGENT_BIN)

server:
	cd server-go && $(GO) test ./...

agent:
	cd agent && $(GO) test ./...

# The collector is its own module and is not part of the PR workflow.
collector:
	cd collector && $(GO) test ./...

clean:
	rm -rf app/dist server-go/internal/staticspa/dist

# --- Docker (optional install path, docs/docker.md) -------------------------
# Not part of `make check`: helpers to build and run the server image. The
# image builds the frontend and the agent binaries itself, so a plain
# `docker build` is self-contained and needs no toolchain on the host.
DOCKER_IMAGE ?= netpulse:local
VERSION ?= 0.0.0-docker
BUILD_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

.PHONY: docker-build docker-up docker-down

docker-build:
	docker build \
	  --build-arg VERSION=$(VERSION) \
	  --build-arg BUILD_COMMIT=$(BUILD_COMMIT) \
	  --build-arg AGENT_VERSION=$(shell tr -d '[:space:]' < agent/VERSION 2>/dev/null || echo 0.0.0-dev) \
	  -t $(DOCKER_IMAGE) .

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down
