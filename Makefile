# Skifity
#
# The short version: `make build` gives you one binary that is the panel, the
# CLI and the MCP server, with the user interface inside it.

SHELL := /bin/sh
BINARY := skifity
MODULE := skifity
BIN_DIR := bin
DIST := web/dist

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.Date=$(DATE)

# CGO is off everywhere: the SQLite driver is pure Go, and a static binary is
# the difference between "download and run" and "install a toolchain first".
export CGO_ENABLED := 0

.DEFAULT_GOAL := build
.PHONY: help build frontend backend dev dev-api test test-go test-race lint lint-go \
	lint-web fmt check i18n smoke e2e screenshots image audit clean deps tidy release install-hooks

help: ## Show this help
	@awk 'BEGIN { FS = ":.*##" } /^[a-zA-Z0-9_-]+:.*##/ { printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: frontend backend ## Build the frontend and the single binary

frontend: ## Build the user interface into web/dist
	npm --prefix web ci --no-audit --no-fund
	npm --prefix web run build

backend: ## Build the binary against whatever is in web/dist
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) ./cmd/$(BINARY)
	@echo "built $(BIN_DIR)/$(BINARY) $(VERSION) ($(COMMIT))"

dev: ## Run the panel with hot reload (needs two terminals: see dev-api)
	npm --prefix web run dev

dev-api: ## Run the panel in dev mode, proxying the UI to the Vite dev server
	SKIFITY_DEV_MODE=true \
	SKIFITY_LISTEN=127.0.0.1:8080 \
	SKIFITY_DATABASE_PATH=.dev/panel.db \
	SKIFITY_MASTER_KEY_PATH=.dev/master.key \
	SKIFITY_SETUP_TOKEN_PATH=.dev/setup-token \
	SKIFITY_LOG_FORMAT=text \
	SKIFITY_LOG_LEVEL=debug \
	go run ./cmd/$(BINARY) server

test: test-go ## Run the tests

test-go: ## Run the Go tests
	go test ./...

test-race: ## Run the Go tests with the race detector
	go test -race ./...

lint: lint-go lint-web i18n ## Run every linter

lint-go: ## Vet the Go code, and run golangci-lint when it can read this module
	go vet ./...
	@./scripts/lint-go.sh

lint-web: ## Lint and type-check the frontend
	npm --prefix web run lint
	# `npm run` in the package directory, not `npm exec`: exec keeps the
	# working directory it was called from, so tsc looked for a tsconfig.json
	# at the repository root and `make check` failed on a file that is not
	# supposed to exist.
	npm --prefix web run typecheck

i18n: ## Fail if any translation is missing in any language
	npm --prefix web run check:i18n

fmt: ## Format Go and frontend code
	gofmt -w ./cmd ./internal
	npm --prefix web run format

audit: ## Report known vulnerabilities in the dependencies
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	npm --prefix web audit --omit=dev --audit-level=high

# `check` is the gate, and it has to contain everything CI can fail on, or it
# is a gate with a hole in it. It had one: CI ran govulncheck and `check` did
# not, so twenty-three runs went red on a step nothing local ever executed —
# and because that step comes before them, the race detector, the smoke tests
# and the interface test never ran on CI at all.
check: lint test audit ## What CI runs

smoke: backend ## Run the smoke tests against a freshly built binary
	./test/smoke/panel.sh
	./test/smoke/installer.sh

e2e: backend ## Run the Playwright user interface test against the real binary
	npm --prefix web run test:e2e

screenshots: backend ## Recapture the screenshots in the README
	SKIFITY_SCREENSHOTS=1 npm --prefix web exec -- playwright test screenshots

image: ## Build the panel's container image
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg DATE=$(DATE) \
		-t ghcr.io/skifity/skifity:$(VERSION) .

deps: ## Download dependencies
	go mod download
	npm --prefix web ci --no-audit --no-fund

tidy: ## Tidy go.mod
	go mod tidy

clean: ## Remove build output
	rm -rf $(BIN_DIR) $(DIST)/assets $(DIST)/index.html $(DIST)/favicon.svg .dev

release: ## Build release binaries for every supported platform
	@mkdir -p $(BIN_DIR)/release
	@for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
		os=$${target%/*}; arch=$${target#*/}; \
		echo "building $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' \
			-o $(BIN_DIR)/release/$(BINARY)-$$os-$$arch ./cmd/$(BINARY) || exit 1; \
	done
	@ls -lh $(BIN_DIR)/release
