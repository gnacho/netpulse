# Local mirror of the .github/workflows/pr.yml checks.
# Keep this in sync with the workflow. Run `make check` before pushing.

SHELL := /bin/bash
AGENT_VERSION ?= 0.0.0-dev
GO ?= go
NPM ?= npm
UPX ?= upx
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
# before the server compiles. Arch names follow normalizeArch: armv7 is
# "arm" with GOARM=7 and MIPS is softfloat.
agents:
	@command -v "$(UPX)" >/dev/null || { echo "UPX is required to build agents (set UPX=/path/to/upx)" >&2; exit 1; }
	@mkdir -p $(AGENT_BIN)
	@# Go may retain an existing output with the same build ID, even after UPX.
	rm -f $(AGENT_BIN)/netpulse-agent-{amd64,arm64,arm,mipsle,mips}{,.version}
	cd agent && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w -X main.Version=$(AGENT_VERSION)" -o ../$(AGENT_BIN)/netpulse-agent-amd64 ./cmd/netpulse-agent
	cd agent && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w -X main.Version=$(AGENT_VERSION)" -o ../$(AGENT_BIN)/netpulse-agent-arm64 ./cmd/netpulse-agent
	cd agent && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 $(GO) build -trimpath -ldflags "-s -w -X main.Version=$(AGENT_VERSION)" -o ../$(AGENT_BIN)/netpulse-agent-arm ./cmd/netpulse-agent
	cd agent && CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat $(GO) build -trimpath -ldflags "-s -w -X main.Version=$(AGENT_VERSION)" -o ../$(AGENT_BIN)/netpulse-agent-mipsle ./cmd/netpulse-agent
	cd agent && CGO_ENABLED=0 GOOS=linux GOARCH=mips GOMIPS=softfloat $(GO) build -trimpath -ldflags "-s -w -X main.Version=$(AGENT_VERSION)" -o ../$(AGENT_BIN)/netpulse-agent-mips ./cmd/netpulse-agent
	@for binary in $(AGENT_BIN)/netpulse-agent-{amd64,arm64,arm,mipsle,mips}; do \
		"$(UPX)" --best --lzma "$$binary" && "$(UPX)" -t "$$binary" || exit 1; \
		printf '%s %s\n' "$(AGENT_VERSION)" "$$(sha256sum "$$binary" | cut -d' ' -f1)" > "$$binary.version" || exit 1; \
	done

server:
	cd server-go && $(GO) test ./...

agent:
	cd agent && $(GO) test ./...

# The collector is its own module and is not part of the PR workflow.
collector:
	cd collector && $(GO) test ./...

clean:
	rm -rf app/dist server-go/internal/staticspa/dist
