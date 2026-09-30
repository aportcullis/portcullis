# AGENTS.md — Portcullis

Single source of truth for any AI coding agent (Claude, GPT/Codex, Gemini, Cursor, Copilot).
`CLAUDE.md` and `GEMINI.md` symlink here.

## Project
Portcullis: self-hosted OSS **DevSecOps database governance and BI** — govern access and changes, audit execution, and turn queries/results into reusable analysis assets.
MVP establishes saved queries and result exploration; charts, dashboards, and agent integration follow the PRD roadmap.
Ships as one Go binary with the SolidJS SPA embedded.

**Stack:** Go (connect-go, pgx, sqlc, viper, slog, argon2) · SolidJS + Vite (tsgo) · PostgreSQL 18 · Docker · buf + sqlc codegen.

## Process — non-negotiable
1. Start from the **PRD** ([한국어](docs/product/prd.ko.md) · [English](docs/product/prd.en.md)) — it is the agreement; conform to it.
2. Record technical decisions as **ADRs** ([`docs/adr/`](docs/adr/)) — see 0001–0008 for the style.
3. **Web-verify** the standard approach before deciding (OWASP, Google IAM, library docs) — never from memory.
4. If an ADR shows the PRD is wrong/insufficient, **amend both PRD translations** (ADR-backed), then implement.
5. Aim for **clean architecture (ports & adapters)** and implement in the **DDD × TDD cycle**: domain model → port (consumer-defined interface) → **red scenario test** (TDD verifies *scenarios* — observable use-case behavior, not implementation) → green → refactor → wire the adapter in `cmd/portcullis`.
   See [code.md — Development order](docs/conventions/code.md).

## Definition of done
`go build ./...`, `go vet ./...`, `make lint` (0 issues), `make test`, and `make e2e` (browser e2e — Playwright against the real binary) all green; `make verify` runs the whole gate.

## Conventions — see `docs/conventions/`
- [code.md](docs/conventions/code.md) — layered DDD, file-split, tests/TDD, minimize-hardcoding.
- [data.md](docs/conventions/data.md) — soft-delete + no-cascade, indexes, migrations, RBAC permissions.
- [security.md](docs/conventions/security.md) — crypto, secrets, audit, redaction.
- [tooling.md](docs/conventions/tooling.md) — make commands, codegen, dependencies, commit style.
- [frontend.md](docs/conventions/frontend.md) — light FSD layers (app/pages/features/entities/shared), `@/` imports, vendored UI, pnpm only.

Function names describe the operation and subject; declaration comments give one short behavior summary for IDE hovers.
Use Go doc comments and TypeScript JSDoc, with detailed rationale in ADRs and only essential inline invariants.

## Map
[Documentation index](docs/README.md) · [Architecture](docs/ARCHITECTURE.md) · [ADRs](docs/adr/) · [Performance](docs/performance/README.md).
