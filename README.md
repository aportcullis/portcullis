# Portcullis

![Portcullis logo](docs/media/logo.png)

**Govern database access. Keep the evidence. Make results useful.**

Portcullis is a self-hosted database governance tool that brings SQL requests, human review, controlled execution, and result exploration into one workflow. It ships as a Go binary with an embedded web interface, backed by PostgreSQL for metadata.

[Quickstart](#quickstart) · [See it in action](#request-review-execute-once) · [Database support](docs/product/database-support.md) · [Documentation](docs/README.md) · [Roadmap](docs/roadmap.md) · [Contribute](#contributing)

[![CI](https://github.com/aportcullis/portcullis/actions/workflows/ci.yml/badge.svg)](https://github.com/aportcullis/portcullis/actions/workflows/ci.yml)

**Development alpha:** the PostgreSQL governance workflow has passed its correctness gate. MySQL and saved queries are planned; the complete MVP is still ahead. See [validation evidence](docs/operations/m1-validation.md) for the tested environment. Capacity and soak qualification are tracked separately. The project's open-source license is [not yet selected](#license).

## Why Portcullis?

A database question often starts with a SQL snippet and ends with a copied result. The approval, exact inputs, execution outcome, and useful analysis can end up scattered across chat, tickets, and database clients.

Portcullis gives developers, reviewers, and operators a shared workflow: explain the request, review the submitted SQL and parameters, execute the approved statement once, and inspect the result alongside its execution history. The longer-term direction is to turn useful queries into reusable team assets while keeping the same governance rules.

| What your team needs | What Portcullis provides today |
| --- | --- |
| Context before approval | Request titles and explanatory bodies, SQL, typed parameters, and a dedicated review page |
| Explicit access rules | Per-connection Read / Write / DDL policies, distinct reviewers, approval quorums, and execution limits |
| Execution tied to the decision | Frozen submissions, approval and policy revalidation, and single-use execution without automatic SQL retries |
| Results that remain useful | Exact large integers and decimals, typed sorting, pagination, filtering, full-cell inspection, and CSV export |
| Evidence in your infrastructure | Encrypted credentials, request payloads and temporary results, server-side authorization, and append-only audit records |

## Request, review, execute once

Compose a request on its own page, including a title, optional explanation, SQL, and typed parameters. SQL formatting is automatic, reversible, and configurable. Save a draft or submit it to freeze the approval inputs. A distinct reviewer approves or rejects the request; the original requester executes an approved statement once.

![A requester submits SQL, a distinct reviewer approves it, and the requester executes the statement once](docs/media/workflow.gif)

![Request composition with a title, explanatory body, SQL and typed parameters](docs/media/request.png)

## Explore results

Choose a column and direction to sort the complete cached snapshot using its declared data type. Numeric precision and timezone-aware timestamp ordering are preserved; equal values keep their original order and NULL values stay last. Restore the original query order at any time. Sorting and filtering do not execute SQL again.

![Actual result sorting controls with exact large integers and descending timestamps](docs/media/results.png)

![Paging through results, sorting revenue, filtering a region, and preparing the complete snapshot as CSV](docs/media/results.gif)

CSV exports the complete snapshot in original query order, independently of the current page, sort, or filter. Results expire after 15 minutes and may be evicted earlier under quota pressure. The GIF stops at the prepared download link; actual file saving and readback are covered by the browser validation gate.

All screenshots and GIFs show the real application with synthetic data. Alex is the requester and Sam the reviewer; the demo reviewer is provisioned through a fixture because user-management screens are not yet available.

## Quickstart

**Prerequisites:** Git and Docker with Docker Compose.

```sh
git clone https://github.com/aportcullis/portcullis.git
cd portcullis
docker compose up --build
```

1. Open [localhost:8080](http://localhost:8080) and create the first administrator.
2. Register a PostgreSQL target and test the connection.
3. Review its execution policy, create a request, and submit it for approval.
4. Have a separately provisioned reviewer approve it, then execute it as the requester and explore the result.

For a disposable single-user demo, set Read approvals to `0`; automatic approval is still audited. The [PostgreSQL alpha quickstart](docs/operations/pg-alpha-quickstart.md) supplies local connection settings and a first query. Compose uses persistent database and key volumes; its sample credentials and disabled database TLS are intended for local demonstrations.

## Current scope

| Area | Status |
| --- | --- |
| PostgreSQL governance and result exploration | Available in the development alpha; see the [feature and version evidence](docs/product/database-support.md) |
| Authentication | First-administrator setup, email/password sessions, and configured Google OIDC |
| Deployment | Docker Compose; one serving process with PostgreSQL metadata |
| MySQL | Next committed database target; product adapter pending |
| SQL review facts and basic EXPLAIN | Planned after MySQL parity |
| Saved queries and schema dry-run preview | Planned; completes the MVP together with the preceding work |
| Sensitive-data masking, agent integration, Helm/Kustomize | Later milestones, with masking before agent access |

PostgreSQL target-version qualification and the metadata database baseline are separate. The [support matrix](docs/product/database-support.md) records their exact boundaries. SQLite is excluded from product scope.

The alpha accepts explicitly supported single-statement SQL forms. Cancellation is best effort, and an uncertain execution is reported as `outcome_unknown`; it is never automatically rerun. Operators own backups, master-key preservation, and deployment configuration. See the [operating guide](docs/operations/pg-alpha-quickstart.md) and [architecture](docs/ARCHITECTURE.md) before evaluating a deployment.

## More of the application

<details>
<summary>Connection management, execution policies, review, and sign-in</summary>

### Connections and policies

Register and test targets, label their environment, and archive connections. Policy settings control statement classes, review requirements, timeouts, row limits, and result sizes. Reads are enabled by default with one distinct reviewer; writes and DDL require explicit enablement.

![PostgreSQL connections labeled as development and production](docs/media/connections.png)

![Read, Write, and DDL permissions, approval counts, and execution limits](docs/media/policy.png)

### Review and authentication

Reviewers inspect the request context, SQL, target snapshot, and approval status before deciding. Requests and results support direct links, reload, and browser history. First-run setup creates the initial administrator; Google sign-in appears when configured.

![A distinct reviewer inspecting submitted SQL and choosing approval or rejection](docs/media/review.png)

![Portcullis-branded email and password sign-in screen](docs/media/login.png)

![Portcullis-branded first-administrator setup screen](docs/media/bootstrap.png)

The environment labels in these captures are illustrative; both targets use an isolated demo database.

</details>

## Contributing

Useful contributions include reproducible bug reports, documentation improvements, and feedback from trying the request → review → execution workflow. Share your use case and expected behavior in an [issue](https://github.com/aportcullis/portcullis/issues); use synthetic SQL and remove credentials and private data from examples.

For implementation changes, start with the [development guide](docs/development.md) and [product requirements](docs/product/prd.en.md). Discuss substantial scope changes before opening a pull request. The project uses scenario-based TDD, DDD, and Clean Architecture, with small reviewed commits and a complete `make verify` gate. Testcontainers and real-browser tests exercise the actual application.

The [roadmap](docs/roadmap.md) explains planned outcomes and prerequisites. It is a direction of travel, not a delivery-date commitment. Experience reports from teams evaluating database governance will help shape those priorities.

## Documentation

[Documentation index](docs/README.md) · [Quickstart](docs/operations/pg-alpha-quickstart.md) · [Database support](docs/product/database-support.md) · [Roadmap](docs/roadmap.md) · [Architecture](docs/ARCHITECTURE.md) · [Development](docs/development.md) · [Brand assets](docs/branding.md)

## License

Portcullis is being developed toward an open-source release. A project license has not yet been selected or added to this repository; licensing and contribution terms remain a [pre-publication decision](docs/product/prd.en.md#122-decision-status-2026-07-04-only-licensing-unresolved). This README does not grant a license.
