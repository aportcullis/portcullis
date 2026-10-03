# Development and roadmap

Product scope and acceptance criteria are maintained in the [English PRD](product/prd.en.md) and its [Korean translation](product/prd.ko.md). This guide summarizes contributor practices, milestone status, and planned capabilities.

## Development practices

Follow DDD and Clean Architecture with consumer-defined ports. Work in the domain → port → failing scenario → passing implementation → refactor → adapter wiring cycle. Tests verify observable use-case behavior. Do not claim a TDD sequence that was not actually observed.

Keep commits focused and small. Immediately before each commit, review the staged diff for correctness, security, dependency direction, and scenario coverage. Verify the staged snapshot so an unstaged dependency cannot hide a broken commit. Fix findings and review again before committing.

Run `make verify` for the complete build, vet, lint, frontend, Go, and browser E2E gate. A remaining failure means the milestone is not complete. See [AGENTS.md](../AGENTS.md), [code conventions](conventions/code.md), [frontend conventions](conventions/frontend.md), and [tooling](conventions/tooling.md) for the full requirements.

## Current milestone

The PostgreSQL M1 implementation covers connection policies, immutable requests and distinct approvals, single-use execution, temporary encrypted result exploration, and audit evidence. Its release gate is incomplete because native CSV file saving has not passed in the browser environment. The [validation record](operations/m1-validation.md) distinguishes passing checks from the remaining gate.

## Planned capabilities

The PRD describes saved queries with favorites, sharing and parameters, MySQL/SQLite execution, charts and dashboards, and schema-change governance as subsequent work. These are planned capabilities, not features advertised as available in the current build. Consult the PRD for milestone boundaries and acceptance criteria; this summary does not replace it.

Record technical decisions in [ADRs](adr/README.md). If a decision changes the product contract, update both PRD translations before implementation.
