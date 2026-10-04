.PHONY: generate web web-install web-dev web-typecheck web-lint web-test web-audit e2e e2e-browser e2e-run e2e-sweep build release run devkey test test-race lint vuln audit verify hooks tidy clean
.PHONY: verify-prepare verify-static verify-go verify-browser
.PHONY: load-test load-typecheck load-bundle load-check load-server query-bench
.PHONY: keygen-check release-check changelog changelog-check release-notes image-check dockerfile-check ignore-check clean-check generate-check proto-breaking

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
	@drift="$$(gofmt -l $$(git ls-files '*.go'))"; \
		if [ -n "$$drift" ]; then echo "gofmt needed (run 'gofmt -w'):"; echo "$$drift"; exit 1; fi
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout=10m --enable=unparam --enable=misspell --enable=prealloc


devkey:
	@head -c 32 /dev/urandom | base64


generate: web-install
	bash .github/scripts/generate.sh

generate-check: web-install
	bash tests/release/generate-check.sh

proto-breaking:
	bash tests/release/proto-breaking.sh


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


e2e: web e2e-browser
	pnpm -C web e2e

e2e-browser:
	pnpm -C web exec playwright install --with-deps chromium

# Run the browser suite against an already built SPA, removing any harness containers a killed run left behind (ADR-0045).
e2e-run:
	@$(MAKE) --no-print-directory e2e-sweep
	pnpm -C web e2e; status=$$?; $(MAKE) --no-print-directory e2e-sweep; exit $$status

e2e-sweep:
	@docker ps -aq --filter label=portcullis.test=browser | xargs docker rm -f >/dev/null 2>&1 || true


build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/portcullis ./cmd/portcullis


release: web build


run:
	go run ./cmd/portcullis


GO_TEST_PACKAGES ?= ./...
# Database-backed tests fail instead of skipping when their containers cannot start, so a gate can never pass without them.
GO_TEST_ENV := PORTCULLIS_TEST_DATABASE_REQUIRED=1

test:
	$(GO_TEST_ENV) go test -p 1 $(GO_TEST_PACKAGES) -count=1 -shuffle=on


test-race:
	$(GO_TEST_ENV) go test -p 1 $(GO_TEST_PACKAGES) -count=1 -shuffle=on -race


vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...


audit: web-audit vuln


export RELEASE_TAG IMAGE_PLATFORMS

changelog:
	@bash .github/scripts/update-changelog.sh

release-notes:
	@bash .github/scripts/git-cliff.sh --current --strip all

image-check:
	bash tests/release/images.sh

changelog-check:
	bash tests/release/changelog.sh

dockerfile-check:
	bash tests/release/dockerfile-check.sh

ignore-check:
	bash tests/release/ignore-check.sh

clean-check:
	bash tests/release/clean-check.sh

release-check:
	bash tests/release/tags.sh

# Build the shared SPA, browser and load bundle once, then run the static, Go test and browser groups concurrently; each group runs its own steps in order and keeps its output in .test-docker/verify (ADR-0045).
VERIFY_LOG_DIR := .test-docker/verify

verify: verify-prepare
	@mkdir -p $(VERIFY_LOG_DIR)
	@$(MAKE) --no-print-directory -j3 verify-group-static verify-group-go verify-group-browser

verify-prepare: web e2e-browser load-bundle

verify-static:
	$(MAKE) -j1 release-check
	$(MAKE) -j1 dockerfile-check
	$(MAKE) -j1 ignore-check
	$(MAKE) -j1 clean-check
	$(MAKE) -j1 generate-check
	$(MAKE) -j1 proto-breaking
	$(MAKE) -j1 changelog-check
	go build ./...
	go vet ./...
	$(MAKE) -j1 lint
	$(MAKE) -j1 web-typecheck
	$(MAKE) -j1 web-lint
	$(MAKE) -j1 web-test
	$(MAKE) -j1 load-check

verify-go:
	$(MAKE) -j1 test

verify-browser:
	$(MAKE) -j1 e2e-run

# Print a group's log only when it fails, so concurrent groups never interleave on the terminal.
verify-group-%:
	@start=$$(date +%s); \
	if $(MAKE) --no-print-directory -j1 verify-$* > $(VERIFY_LOG_DIR)/$*.log 2>&1; then \
		echo "verify-$* passed in $$(( $$(date +%s) - start ))s"; \
	else \
		echo "verify-$* FAILED; full log in $(VERIFY_LOG_DIR)/$*.log" >&2; \
		tail -n 200 $(VERIFY_LOG_DIR)/$*.log >&2; \
		exit 1; \
	fi


hooks:
	go tool lefthook install

tidy:
	go mod tidy

# Remove build outputs only; generated Go/TypeScript sources are committed and regenerated with `make generate`.
clean:
	rm -rf bin
	find internal/platform/assets/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +

# Verify integrity and known vulnerabilities before delivery.
.PHONY: supply-chain load-audit install-policy-test
load-audit:
	pnpm -C tests/load audit --audit-level=high

install-policy-test:
	cd web && node --test tests/install-policy.test.mjs

supply-chain: web-audit load-audit vuln install-policy-test
	go mod verify
