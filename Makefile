.PHONY: generate web web-install web-dev web-typecheck web-audit e2e build release run devkey test test-race lint vuln audit verify hooks tidy clean

# Bootstrap: install the git hooks on the first make invocation in a clone, so
# any make command sets them up (git runs nothing on clone itself). Only the
# default hooks dir is auto-managed: if core.hooksPath is customized, lefthook's
# install needs --force, so we leave it alone and rely on an explicit `make
# hooks`. The path is worktree-aware (git rev-parse), both hooks are checked, and
# install errors surface on stderr but never fail make.
ifeq (,$(shell git config core.hooksPath 2>/dev/null))
GIT_HOOKS := $(shell git rev-parse --git-path hooks 2>/dev/null)
ifneq (,$(GIT_HOOKS))
ifeq (,$(and $(wildcard $(GIT_HOOKS)/pre-commit),$(wildcard $(GIT_HOOKS)/pre-push)))
$(shell go tool lefthook install >/dev/null)
endif
endif
endif

# Static analysis. Built from source with the project's Go toolchain so the
# linter's go/types matches the module's Go version (a prebuilt binary built
# with an older Go fails with "no go files to analyze").
GOLANGCI_LINT_VERSION := v2.12.2
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout=10m --enable=unparam --enable=misspell --enable=prealloc

# Print a fresh base64 master key for local development.
devkey:
	@head -c 32 /dev/urandom | base64

# Generate code: Connect (Go + TS) from proto, and type-safe queries from SQL.
generate:
	buf generate
	sqlc generate

# Install frontend dependencies.
web-install:
	pnpm -C web install

# Build the frontend into the embedded assets directory.
web: web-install
	pnpm -C web build

# Run the frontend dev server (proxies API calls to the Go backend).
web-dev: web-install
	pnpm -C web dev

# Type-check the frontend (tsgo: app + node config).
web-typecheck: web-install
	pnpm -C web typecheck

# Audit frontend dependencies for known vulnerabilities.
web-audit: web-install
	pnpm -C web audit

# Browser e2e (Playwright) against the real binary and a throwaway PostgreSQL
# (web/e2e/server.sh, booted by Playwright's webServer hook). The SPA is served
# from the Go embed, so it is built first. Needs Docker.
e2e: web
	pnpm -C web exec playwright install --with-deps chromium
	pnpm -C web e2e

# Build the optimized, static single binary.
# -s -w strip the symbol table and DWARF; -trimpath removes local paths;
# CGO_ENABLED=0 yields a fully static, portable binary.
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/portcullis ./cmd/portcullis

# Full release artifact: build the frontend, then embed it in the binary.
release: web build

# Run the server directly for development (unoptimized).
run:
	go run ./cmd/portcullis

# Run all tests. Packages run in parallel by default; parallel-marked tests run
# concurrently within a package. -shuffle=on surfaces hidden inter-test order deps.
test:
	go test ./... -count=1 -shuffle=on

# Same as test, with the race detector.
test-race:
	go test ./... -count=1 -shuffle=on -race

# Scan Go code and dependencies for known vulnerabilities.
# A security scanner is intentionally run at @latest for the newest checks.
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Full dependency security audit: frontend deps + Go vulnerabilities.
audit: web-audit vuln

# Definition-of-Done gate in one target: build, vet, lint, the full test
# suite, and the browser e2e. CI, the pre-push git hook, and contributors all
# call this, so "green" means the same thing everywhere. (Integration tests
# and e2e need Docker.)
verify:
	go build ./...
	go vet ./...
	$(MAKE) lint
	$(MAKE) test
	$(MAKE) e2e

# Install the git hooks (pre-commit gofmt, pre-push verify). lefthook is pinned
# as a go tool dependency in go.mod, so the installed hooks resolve it through
# `go tool lefthook` — no global install or PATH entry required.
hooks:
	go tool lefthook install

tidy:
	go mod tidy

clean:
	rm -rf bin web/src/gen
