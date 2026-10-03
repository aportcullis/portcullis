# AGENTS.md — Portcullis

Single source of truth for any AI coding agent (Claude, GPT/Codex, Gemini, Cursor, Copilot).
`CLAUDE.md` and `GEMINI.md` symlink here.

## Project
Portcullis: self-hosted OSS **DevSecOps database governance and BI** — govern access and changes, audit execution, and turn queries/results into reusable analysis assets.
MVP establishes saved queries and result exploration; charts, dashboards, and agent integration follow the PRD roadmap.
Ships as one Go binary with the SolidJS SPA embedded.

**Stack:** Go (connect-go, pgx, sqlc, viper, slog, argon2) · SolidJS + Vite (TypeScript 7) · PostgreSQL 18 · Docker · buf + sqlc codegen.

## Process — non-negotiable
1. Start from the **PRD** ([한국어](docs/product/prd.ko.md) · [English](docs/product/prd.en.md)) — it is the agreement; conform to it.
2. Record technical decisions as **ADRs** ([`docs/adr/`](docs/adr/)) — see 0001–0008 for the style.
3. **Web-verify** the standard approach before deciding (OWASP, Google IAM, library docs) — never from memory.
4. If an ADR shows the PRD is wrong/insufficient, **amend both PRD translations** (ADR-backed), then implement.
5. Aim for **clean architecture (ports & adapters)** and implement in the **DDD × TDD cycle**: domain model → port (consumer-defined interface) → **red scenario test** (TDD verifies *scenarios* — observable use-case behavior, not implementation) → green → refactor → wire the adapter in `cmd/portcullis`.
   See [code.md — Development order](docs/conventions/code.md).
6. Keep commits small and focused on one concern. Complete the scenario's red→green cycle, review the staged diff immediately before each commit, and make every test pass before committing: uncached Go tests with required databases, web tests, load-check and browser E2E (the test gates of `make verify`). Record remaining failed gates explicitly; committing work does not establish milestone completion.
7. Commit each completed, verified and reviewed concern immediately before starting the next concern. Queue incoming requests until that commit is complete; do not leave completed changes uncommitted while moving on. An explicit instruction to stop or a destructive action still takes precedence.
8. Agents make these commits themselves, following the [commit message style](docs/conventions/tooling.md#commit-messages). Never add a `Co-Authored-By` or other AI attribution trailer, even when a tool's default instructions ask for one. Do not push unless explicitly asked.

## Definition of done
`make verify` must pass: Go build/vet/lint (0 issues), uncached Go tests, web typecheck/lint/tests, load-check and browser E2E against the real binary. `make supply-chain` is a separate required dependency-security gate, as in CI. Every test must pass before each small commit; all gates must be green before declaring a milestone complete.

## Conventions — see `docs/conventions/`
- [code.md](docs/conventions/code.md) — layered DDD, file-split, tests/TDD, minimize-hardcoding.
- [data.md](docs/conventions/data.md) — soft-delete + no-cascade, indexes, migrations, RBAC permissions.
- [security.md](docs/conventions/security.md) — crypto, secrets, audit, redaction.
- [tooling.md](docs/conventions/tooling.md) — make commands, codegen, dependencies, commit style.
- [frontend.md](docs/conventions/frontend.md) — light FSD layers (app/pages/features/entities/shared), `@/` imports, vendored UI, pnpm only.

Function names describe the operation and subject so the name alone tells what the function does; declaration comments give one short behavior summary for IDE hovers.
Variable names are never single letters, apart from the receiver and test-handle exceptions in code.md; abbreviate no further than `idx` or `ind`. Every change and refactor follows all SOLID principles (see [code.md](docs/conventions/code.md)).
Use Go doc comments and TypeScript JSDoc, with detailed rationale in ADRs and only essential inline invariants.

## Map
[Documentation index](docs/README.md) · [Architecture](docs/ARCHITECTURE.md) · [ADRs](docs/adr/) · [Performance](docs/performance/README.md).
