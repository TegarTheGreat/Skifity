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
	lint-web fmt check i18n smoke e2e clean deps tidy release install-hooks

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

lint-go: ## Vet the Go code, and run golangci-lint when it is installed
	go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint is not installed; ran go vet only"; \
	fi

lint-web: ## Lint and type-check the frontend
	npm --prefix web run lint
	npm --prefix web exec -- tsc -b

i18n: ## Fail if any translation is missing in any language
	npm --prefix web run check:i18n

fmt: ## Format Go and frontend code
	gofmt -w ./cmd ./internal
	npm --prefix web run format

check: lint test ## What CI runs

smoke: backend ## Run the panel smoke test against a freshly built binary
	./test/smoke/panel.sh

e2e: ## Run the Playwright user interface test
	npm --prefix web run test:e2e

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
