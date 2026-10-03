# Contributing to Portcullis

Portcullis is in development alpha. Documentation improvements, reproducible bug reports, scenario tests, and workflow feedback are useful starting points. Portcullis is licensed under [Apache 2.0](LICENSE).

## Choose your starting point

- **Try and give feedback:** follow the [quickstart](docs/operations/pg-alpha-quickstart.md), then share a use case or a confusing step.
- **Make a first contribution:** pick a prepared `good first issue`, or ask maintainers for a bounded documentation, reproduction, or UI task.
- **Develop a feature:** discuss the problem, scope, and acceptance criteria before implementing it.
- **Help regularly:** triage reports, review changes, improve documentation, and help new participants.

See [COMMUNITY.md](COMMUNITY.md) for channels, first-issue readiness, and continuing participation. Until Discussions is verified live, [GitHub issues](https://github.com/aportcullis/portcullis/issues) also accept questions and ideas.

## Questions and proposals

For bugs, include the database and application version or commit, reproduction steps, expected behavior, and observed behavior. Use synthetic SQL and data; remove credentials and private information from logs and screenshots.

For substantial changes, discuss the use case and proposed scope first. Ideas become implementation issues after maintainers agree on acceptance criteria. The [roadmap](docs/roadmap.md) describes planned outcomes; the [English PRD](docs/product/prd.en.md) and [Korean PRD](docs/product/prd.ko.md) define the shared product contract.

Comment on an existing issue before starting so a maintainer can confirm its scope and whether someone is already working on it. A first-contribution issue should identify a reviewer, related files, completion criteria, and verification steps. Ask for help if those details are missing.

## Contribution licensing

Unless explicitly stated otherwise, contributions intentionally submitted for inclusion are under Apache 2.0, as described in section 5 of [LICENSE](LICENSE). Existing third-party licenses and attribution notices must be preserved. A separate CLA or DCO process is not introduced by this change.

## Development and pull requests

Follow the [development guide](docs/development.md), [AGENTS.md](AGENTS.md), and the [code conventions](docs/conventions/code.md). Keep development details in those guides rather than duplicating them here.

1. Start with the relevant product requirements and existing architectural decisions.
2. Follow the domain → consumer-defined port → failing observable scenario → implementation → refactor → adapter cycle.
3. Keep commits small and focused. Review the staged diff immediately before each commit and run checks appropriate to the change.
4. Run `make verify` for the complete build, lint, unit/integration, and real-browser gate. Docker is required for the Testcontainers-backed tests. Record any failing checks explicitly.
5. Open a pull request describing the user-visible problem, resulting behavior, and validation. Keep generated files aligned with their sources when changing APIs or database queries.

See [tooling](docs/conventions/tooling.md) for commands and dependency requirements, and [frontend conventions](docs/conventions/frontend.md) for web changes. Technical scope decisions belong in [ADRs](docs/adr/README.md); changes to the product contract update both PRD translations.
