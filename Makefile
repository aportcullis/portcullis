.PHONY: generate web web-install web-dev web-typecheck web-lint web-test web-audit e2e build release run devkey test test-race lint vuln audit verify hooks tidy clean
.PHONY: load-test load-typecheck load-server query-bench

K6 ?= k6

load-typecheck:
	pnpm -C tests/load install --frozen-lockfile
	pnpm -C tests/load run typecheck

load-test: load-typecheck
	$(K6) run tests/load/governance.ts

load-server: web
	mkdir -p .test-docker/e2e tests/load/results
	go build -o .test-docker/e2e/portcullis ./cmd/portcullis
	go run ./tests/load/server

query-bench:
	go test ./internal/infra/pgdialect -run '^$$' -bench '^BenchmarkQueryWorkloads$$' -benchtime=20x -count=3 -benchmem

# Install missing default hooks; custom hook paths require `make hooks`.
ifeq (,$(shell git config core.hooksPath 2>/dev/null))
GIT_HOOKS := $(shell git rev-parse --git-path hooks 2>/dev/null)
ifneq (,$(GIT_HOOKS))
ifeq (,$(and $(wildcard $(GIT_HOOKS)/pre-commit),$(wildcard $(GIT_HOOKS)/pre-push)))
$(shell go tool lefthook install >/dev/null)
endif
endif
endif

# Build the linter with the project's Go toolchain to match its type checker.
GOLANGCI_LINT_VERSION := v2.12.2
lint:
	@drift="$$(gofmt -l internal cmd)"; \
		if [ -n "$$drift" ]; then echo "gofmt needed (run 'gofmt -w'):"; echo "$$drift"; exit 1; fi
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout=10m --enable=unparam --enable=misspell --enable=prealloc


devkey:
	@head -c 32 /dev/urandom | base64


generate:
	buf generate
	sqlc generate


web-install:
	pnpm -C web install


web: web-install
	pnpm -C web build


web-dev: web-install
	pnpm -C web dev


web-typecheck: web-install
	pnpm -C web typecheck


web-lint: web-install
	pnpm -C web lint


web-test: web-install
	pnpm -C web test


web-audit: web-install
	pnpm -C web audit


e2e: web
	pnpm -C web exec playwright install --with-deps chromium
	pnpm -C web e2e


build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/portcullis ./cmd/portcullis


release: web build


run:
	go run ./cmd/portcullis


test:
	go test ./... -count=1 -shuffle=on


test-race:
	go test ./... -count=1 -shuffle=on -race


vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...


audit: web-audit vuln


verify:
	go build ./...
	go vet ./...
	$(MAKE) lint
	$(MAKE) web-typecheck
	$(MAKE) web-lint
	$(MAKE) web-test
	$(MAKE) load-typecheck
	$(MAKE) test
	$(MAKE) e2e


hooks:
	go tool lefthook install

tidy:
	go mod tidy

clean:
	rm -rf bin web/src/gen
