.PHONY: generate web web-install web-dev web-typecheck web-lint web-test web-audit e2e build release run devkey test test-race lint vuln audit verify hooks tidy clean
.PHONY: load-test load-typecheck load-bundle load-check load-server query-bench
.PHONY: keygen-check release-check

# Use only a disposable tmpfs; never run key-generation tests on the demo volume.
keygen-check:
	@image="$$(sed -n 's/^    image: \(alpine:[^ ]*\)$$/\1/p' compose.yaml)"; \
		test -n "$$image" && docker run --rm \
		--mount "type=bind,source=$(CURDIR)/deploy/keygen,target=/keygen,readonly" \
		--tmpfs /secrets:rw,mode=0700 "$$image" sh /keygen/test-master-key.sh

K6 ?= k6
LOAD_FIXTURES ?= $(CURDIR)/tests/load/fixtures.local.json

load-typecheck:
	pnpm -C tests/load install --frozen-lockfile
	pnpm -C tests/load run typecheck

load-bundle: load-typecheck
	pnpm -C tests/load bundle

load-check: load-bundle
	pnpm -C tests/load lint
	pnpm -C tests/load test

load-test: load-check
	$(K6) run -e LOAD_FIXTURES="$(LOAD_FIXTURES)" tests/load/dist/governance.js

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
GOLANGCI_LINT_VERSION := v2.14.0
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
	pnpm -C web install --frozen-lockfile


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
	go test -p 1 ./... -count=1 -shuffle=on


test-race:
	go test -p 1 ./... -count=1 -shuffle=on -race


vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...


audit: web-audit vuln


release-check:
	bash tests/release/tags.sh

verify:
	$(MAKE) release-check
	go build ./...
	go vet ./...
	$(MAKE) lint
	$(MAKE) web-typecheck
	$(MAKE) web-lint
	$(MAKE) web-test
	$(MAKE) load-check
	$(MAKE) test
	$(MAKE) e2e


hooks:
	go tool lefthook install

tidy:
	go mod tidy

clean:
	rm -rf bin web/src/gen

# Verify integrity and known vulnerabilities before delivery.
.PHONY: supply-chain load-audit install-policy-test
load-audit:
	pnpm -C tests/load audit --audit-level=high

install-policy-test:
	cd web && node --test tests/install-policy.test.mjs

supply-chain: web-audit load-audit vuln install-policy-test
	go mod verify
