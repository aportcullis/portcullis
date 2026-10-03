# Contributing to Portcullis

Portcullis is in development alpha. Documentation improvements, reproducible bug reports, scenario tests, and workflow feedback are useful starting points. Project licensing and contribution terms remain [pending](README.md#license).

## Questions and proposals

Use [GitHub issues](https://github.com/aportcullis/portcullis/issues) for questions, bug reports, and feature proposals. For bugs, include the database and application version or commit, reproduction steps, expected behavior, and observed behavior. Use synthetic SQL and data; remove credentials and private information from logs and screenshots.

For substantial changes, discuss the use case and proposed scope before implementation. The [roadmap](docs/roadmap.md) describes planned outcomes; the [English PRD](docs/product/prd.en.md) and [Korean PRD](docs/product/prd.ko.md) define the shared product contract.

## Development and pull requests

Follow the [development guide](docs/development.md), [AGENTS.md](AGENTS.md), and the [code conventions](docs/conventions/code.md). Keep development details in those guides rather than duplicating them here.

1. Start with the relevant product requirements and existing architectural decisions.
2. Follow the domain → consumer-defined port → failing observable scenario → implementation → refactor → adapter cycle.
3. Keep commits small and focused. Review the staged diff immediately before each commit and run checks appropriate to the change.
4. Run `make verify` for the complete build, lint, unit/integration, and real-browser gate. Docker is required for the Testcontainers-backed tests. Record any failing checks explicitly.
5. Open a pull request describing the user-visible problem, resulting behavior, and validation. Keep generated files aligned with their sources when changing APIs or database queries.

See [tooling](docs/conventions/tooling.md) for commands and dependency requirements, and [frontend conventions](docs/conventions/frontend.md) for web changes. Technical scope decisions belong in [ADRs](docs/adr/README.md); changes to the product contract update both PRD translations.
