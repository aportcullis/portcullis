# Development guide

Product scope and acceptance criteria are maintained in the [English PRD](product/prd.en.md) and its [Korean translation](product/prd.ko.md). The public [roadmap](roadmap.md) describes milestone outcomes, dependencies, and status; this guide describes how to contribute.

## Development practices

Follow DDD and Clean Architecture with consumer-defined ports. Work in the domain → port → failing scenario → passing implementation → refactor → adapter wiring cycle. Tests verify observable use-case behavior. Do not claim a TDD sequence that was not actually observed.

Keep commits focused and small. Immediately before each commit, review the staged diff for correctness, security, dependency direction, and scenario coverage. Verify the staged snapshot so an unstaged dependency cannot hide a broken commit. Fix findings and review again before committing.

Run `make verify` for the complete build, vet, lint, frontend, Go, and browser E2E gate. A remaining failure means the milestone is not complete. See [AGENTS.md](../AGENTS.md), [code conventions](conventions/code.md), [frontend conventions](conventions/frontend.md), and [tooling](conventions/tooling.md) for the full requirements.

Record technical decisions in [ADRs](adr/README.md). If a decision changes the product contract, update both PRD translations before implementation.
