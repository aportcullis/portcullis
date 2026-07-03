# AGENTS.md — Portcullis

Single source of truth for any AI coding agent (Claude, GPT/Codex, Gemini, Cursor, Copilot).
`CLAUDE.md` and `GEMINI.md` symlink here.

## Project
Portcullis: self-hosted OSS **database access governance** — control/audit human DB access and turn
approved queries into reusable team assets. Ships as one Go binary with the SolidJS SPA embedded.

**Stack:** Go (connect-go, pgx, sqlc, viper, slog, argon2) · SolidJS + Vite (tsgo) · PostgreSQL 18 ·
Docker · buf + sqlc codegen.

## Process — non-negotiable
1. Start from the **PRD** (the product requirements) — it is the agreement; conform to it.
2. Record technical decisions as **ADRs** ([`docs/adr/`](docs/adr/)) — see 0001–0008 for the style.
3. **Web-verify** the standard approach before deciding (OWASP, Google IAM, library docs) — never
   from memory.
4. If an ADR shows the PRD is wrong/insufficient, **amend the PRD** (ADR-backed), then implement.
5. Aim for **clean architecture (ports & adapters)** and implement in the **DDD × TDD cycle**:
   domain model → port (consumer-defined interface) → **red scenario test** (TDD verifies
   *scenarios* — observable use-case behavior, not implementation) → green → refactor → wire the
   adapter in `cmd/portcullis`. See [code.md — Development order](docs/conventions/code.md).

## Definition of done
`go build ./...`, `go vet ./...`, `make lint` (0 issues), and `make test` all green.

## Conventions — see `docs/conventions/`
- [code.md](docs/conventions/code.md) — layered DDD, file-split, tests/TDD, minimize-hardcoding.
- [data.md](docs/conventions/data.md) — soft-delete + no-cascade, indexes, migrations, RBAC permissions.
- [security.md](docs/conventions/security.md) — crypto, secrets, audit, redaction.
- [tooling.md](docs/conventions/tooling.md) — make commands, codegen, dependencies, commit style.

## Map
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — layers · [`docs/adr/`](docs/adr/) — decisions.
