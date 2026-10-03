# Portcullis

![Portcullis logo](docs/media/logo.png)

**Self-hosted, open-source database governance and result exploration.**

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![CI](https://github.com/aportcullis/portcullis/actions/workflows/ci.yml/badge.svg)](https://github.com/aportcullis/portcullis/actions/workflows/ci.yml)

[Getting started](#getting-started) · [Documentation](#documentation) · [Demo](#request-review-execute-once) · [Community](#community) · [Contributing](CONTRIBUTING.md)

Portcullis brings SQL requests, review, execution, and audit into one application. Teams define connection policies, review the exact SQL and parameters, and explore the results of an approved execution. A Go server embeds the web interface; PostgreSQL stores metadata.

**Project status:** development alpha with a verified PostgreSQL governance workflow. MySQL is the next target; the full MVP is still ahead. See the [support matrix](docs/product/database-support.md), [validation record](docs/operations/m1-validation.md), and [license](#license).

## Why Portcullis?

Database access decisions need context and evidence. Portcullis connects a request's purpose, approval inputs, execution outcome, and results so teams can review and investigate the same workflow in their own infrastructure.

### Features

- **Policy-controlled access:** per-connection Read / Write / DDL permissions, approval quorums, and time, row, and result-size limits.
- **Reviewable requests:** titles, explanatory bodies, SQL formatting, typed parameters, drafts, and frozen submissions with distinct reviewers.
- **Single-use execution:** revalidate approvals and policies before execution; report uncertain outcomes without automatically rerunning SQL.
- **Typed results:** preserve numeric precision, sort the complete snapshot, filter, inspect full cells, and export CSV without re-execution.
- **Authorization and evidence:** server-side permissions, encrypted credentials and payloads, expiring encrypted results, and append-only audit records.
- **Self-hosted operation:** Docker Compose, an embedded web UI, local authentication, and optional Google OIDC.

## Request, review, execute once

A requester submits SQL and its context. A distinct reviewer approves or rejects it. The requester executes an approved statement once and explores its result.

![A requester submits SQL, a distinct reviewer approves it, and the requester executes the statement once](docs/media/workflow.gif)

![Paging through results, sorting revenue, filtering a region, and preparing the complete snapshot as CSV](docs/media/results.gif)

These walkthroughs show the actual application with synthetic data. The reviewer is fixture-provisioned; user-management screens are planned. The CSV walkthrough ends at the prepared download link. Explore the [screenshots and product tour](docs/media/product-tour.md) for request composition, current sorting controls, connection policies, and authentication.

<details>
<summary>View request composition and result sorting screenshots</summary>

![Request composition with a title, explanatory body, SQL and typed parameters](docs/media/request.png)

![Result sorting controls with exact large integers and descending timestamps](docs/media/results.png)

</details>

## Getting started

To try Portcullis locally, install Git and Docker with Docker Compose:

```sh
git clone https://github.com/aportcullis/portcullis.git
cd portcullis
docker compose up --build
```

Open [localhost:8080](http://localhost:8080), create the first administrator, and follow the [PostgreSQL quickstart](docs/operations/pg-alpha-quickstart.md) to register a target and run your first request. The default policy requires one distinct reviewer; for a disposable single-user demo, set Read approvals to `0`.

Compose preserves metadata and master keys in volumes. Its sample credentials and disabled database TLS are for local demonstrations. See the quickstart for operating requirements, backup considerations, and execution limitations.

## Documentation

| Start here | Reference |
| --- | --- |
| Try the application | [PostgreSQL quickstart](docs/operations/pg-alpha-quickstart.md) · [Product tour](docs/media/product-tour.md) |
| Check compatibility | [Database features and version evidence](docs/product/database-support.md) |
| Understand operation | [Architecture](docs/ARCHITECTURE.md) · [Key rotation](docs/operations/key-rotation.md) · [Validation](docs/operations/m1-validation.md) |
| Explore the direction | [Roadmap](docs/roadmap.md) · [Product requirements](docs/product/prd.en.md) |
| Work on the project | [Contributing](CONTRIBUTING.md) · [Development guide](docs/development.md) |

The [documentation index](docs/README.md) contains the complete repository documentation. MySQL parity, SQL review/EXPLAIN, and saved queries with schema preview are the next milestones. Sensitive-data masking precedes later agent integration. Deployment and integration plans, including Helm/Kustomize, are tracked in the roadmap.

## Community

Use [GitHub issues](https://github.com/aportcullis/portcullis/issues) for questions, reproducible bug reports, feature proposals, and feedback from evaluating Portcullis. Include the database version, expected behavior, and minimal synthetic examples where relevant.

If you are trying Portcullis with your team, share the workflow you need and where the current experience falls short. These reports help prioritize the roadmap.

## Contributing

Contributions can start with documentation, bug reproduction, tests, or implementation. Read [CONTRIBUTING.md](CONTRIBUTING.md) for the workflow and links to development requirements. Discuss substantial changes in an issue before implementation so they can be aligned with the product scope.

## License

Portcullis is licensed under the [Apache License, Version 2.0](LICENSE). See [NOTICE](NOTICE) for project attribution. Third-party components retain their respective licenses.
