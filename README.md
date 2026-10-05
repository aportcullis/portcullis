# Portcullis

English · [한국어](README.ko.md)

![Portcullis logo](docs/media/logo.png)

**Self-hosted database governance. Request, review, execute once, and explore results.**

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![CI](https://github.com/aportcullis/portcullis/actions/workflows/ci.yml/badge.svg)](https://github.com/aportcullis/portcullis/actions/workflows/ci.yml)

[Quickstart](#getting-started) · [Demo](#see-it-in-action) · [Docs](#documentation) · [Changelog](CHANGELOG.md) · [Community](#community) · [Contribute](#contributing)

Portcullis manages SQL requests, approvals, execution and audit on your own infrastructure. Review the SQL and parameters before execution, then explore the results in the same application. The web UI ships inside one Go binary; PostgreSQL stores metadata.

**Development alpha:** the PostgreSQL workflow has been tested. User and role administration and release preparation are still in progress. See [database support](docs/product/database-support.md) and [validation results](docs/milestones/m1/validation.md) for what has been verified. Later milestones complete the MVP.

## What you can do

- **Set approval policies:** choose allowed SQL types, required approvals and execution limits for each connection.
- **Request and review:** submit SQL with a description and typed parameters. Reviewers see the exact submitted content.
- **Execute once:** check approvals and policies again before execution. SQL is never automatically retried when the outcome is uncertain.
- **Explore results:** sort, filter, inspect cells, switch Table/Text views, copy a page or export CSV without rerunning SQL.

<details>
<summary>Security and self-hosting details</summary>

The server checks every permission. Credentials, request data and cached results are encrypted. Cached results expire; audit records can only be appended. Connection policies limit execution time, row count and result size.

Run with Docker Compose and the built-in web UI. Local login is included; Google OIDC is optional. See the [architecture](docs/ARCHITECTURE.md) and [operations guide](docs/operations/pg-alpha-quickstart.md) for details.

</details>

## Getting started

Install Git and Docker with Docker Compose, then run:

```sh
git clone https://github.com/aportcullis/portcullis.git
cd portcullis
docker compose up --build
```

1. Open [localhost:8080](http://localhost:8080).
2. Copy the setup token from `docker compose logs portcullis` and create the first administrator.
3. Follow the [PostgreSQL quickstart](docs/operations/pg-alpha-quickstart.md) to add a connection and submit a request.

If you configure the initial administrator email and password in advance, the account is created at startup. Sign in directly; no setup token is issued. See [.env.example](.env.example) for the settings.

For production, keep Portcullis on a private network and connect through Cloudflare WARP or Tailscale. See the [recommended deployment](docs/operations/recommended-architecture.md).

<details>
<summary>Local demo settings and persistence</summary>

By default, another user must approve a request. For a disposable single-user demo, set **Read approvals** to `0`.

Compose stores metadata and master keys in volumes. Sample credentials and disabled database TLS are for local demos only. The quickstart covers backups, operating requirements and execution limits.

</details>

## See it in action

These recordings show the actual application using sample data.

### Request, review and execute

Submit SQL, get approval from another user and execute once.

![SQL submission, approval by another user and one-time execution](docs/media/workflow.gif)

<details>
<summary>View request and review screenshots</summary>

Add a description, SQL and typed parameters to a request.

![Request composition](docs/media/request.png)

Review the submitted SQL and approve or reject it.

![Approval and rejection controls](docs/media/review.png)

Expand a request to follow its progress. Missing history and uncertain outcomes are shown explicitly.

![Request progress from draft to execution](docs/media/requests.png)

</details>

### Explore results

Sort, filter, copy a page or export CSV from the cached result.

![Result paging, sorting, filtering and CSV preparation](docs/media/results.gif)

<details>
<summary>View Table and Text screenshots</summary>

Sorting covers the entire cached result and preserves numeric precision. Table/Text views and clipboard copy use the current filtered page. CSV includes the full result in the original query order; the recording ends at the download link.

![Paged results with sorting and copy controls](docs/media/results.png)

Text view wraps long values. The execution summary shows affected rows and server duration, including DB connection, execution, result collection and storage.

![Text result view](docs/media/results-text.png)

</details>

<details>
<summary>View connection and policy screenshots</summary>

Register a database connection, then set allowed SQL types, required approvals and execution limits.

![Database connections](docs/media/connections.png)

![Connection approval policy and execution limits](docs/media/policy.png)

</details>

See the [product tour](docs/media/product-tour.md) for more details.

## Documentation

| I want to… | Start here |
| --- | --- |
| Try Portcullis | [PostgreSQL quickstart](docs/operations/pg-alpha-quickstart.md) |
| Check database support | [Features and version evidence](docs/product/database-support.md) |
| Understand the system | [Architecture](docs/ARCHITECTURE.md) · [Operations validation](docs/milestones/m1/validation.md) |
| Explore what's next | [Roadmap](docs/milestones/README.md) · [Product requirements](docs/product/prd.en.md) |
| Develop or customize the UI | [Development guide](docs/development.md) · [UI customization](docs/conventions/frontend.md#changing-the-ui) · [Component catalog](web/src/shared/ui/README.md) |

See the [documentation index](docs/README.md) for all guides. Product requirements and landing pages are available in Korean and English; other guides are currently in English.

## Community

Report bugs, ask questions or share feedback through [GitHub issues](https://github.com/aportcullis/portcullis/issues). See [COMMUNITY.md](COMMUNITY.md) for ways to participate.

Report suspected vulnerabilities privately by following [SECURITY.md](SECURITY.md).

## Contributing

Help with documentation, bug reproduction, tests or small improvements. Start with [CONTRIBUTING.md](CONTRIBUTING.md) and discuss substantial changes in an issue before implementation.

## License

[Apache License 2.0](LICENSE). See [NOTICE](NOTICE) for attribution; third-party components retain their own licenses.
