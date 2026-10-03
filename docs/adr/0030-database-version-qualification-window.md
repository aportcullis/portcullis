# ADR-0030: Database compatibility maintenance window

- **Status:** Accepted (policy; additional version qualification pending)
- **Date:** 2026-10-03

## Context

The product owner initially proposed at most three release families per database, then explicitly clarified PostgreSQL coverage as 16/17/18/19 with compatibility maintenance rather than version-specific feature expansion. An unbounded minimum-version claim does not establish acceptance on every subsequent release. MySQL LTS and Innovation numbering cannot be treated as consecutive integers.

## Decision

Maintain the existing Portcullis feature contract on **PostgreSQL 16, 17, 18 and 19**. This explicit four-family window supersedes the earlier three-family proposal for PostgreSQL. Adding 19 does not retire 16. Coverage means preserving current behavior and fixing compatibility regressions, not implementing every engine feature, new SQL syntax or extension. Future widening requires a scope decision and recorded acceptance evidence. PostgreSQL/MySQL remain the committed engines; SQLite remains excluded.

PostgreSQL 16/17/18 are GA maintenance targets. PostgreSQL 19 Beta 4 is also in the compatibility window, with preview qualification until GA (currently planned for October 29, 2026). Record preview versus GA status honestly; beta success does not establish production readiness. Re-run the shared behavior/security gate against the final GA before claiming GA qualification. Use current patch releases and immutable test-image digests. Do not silently remove families on a newer release; retirements require an explicit scope decision, release notes and migration guidance.

For MySQL, qualification candidates are **8.4 LTS, 9.7 LTS and 26.7 Innovation**. Prioritize the two LTS families, then independently qualify the third. Version numbering follows upstream release series, not arithmetic major increments. MySQL 8.0 entered Sustaining Support on April 21, 2026 and is outside this target window. A newer Innovation series requires fresh release/availability and lifecycle checks before replacing 26.7. None of these MySQL candidates is currently a supported product adapter.

For each family, require real-engine Testcontainers scenarios plus real-binary browser E2E: registration/TLS, classification and bound parameters, policy and distinct approval, single-use execution, exact types, limits/results/CSV, timeout/cancellation and audit/isolation. Record actual engine version, patch/digest, test counts, failures and skips. Missing Docker or skipped database tests fail a qualification claim. New syntax stays fail-closed until parser/catalog and security scenarios cover it; server compatibility does not imply all syntax/extensions are accepted.

Metadata storage has a separate deployment contract: PostgreSQL 18 remains its current baseline. Managed-target qualification does not automatically certify metadata migrations/upgrades on all target families. Capacity evidence is also separate and must identify its actual baseline.

Go integration fixtures keep separate metadata and target pools. `Postgres`/`FreshPostgres` use the pinned PostgreSQL 18 metadata image; `TargetPostgres`/`FreshTargetPostgres` alone use the reviewed family catalog. External installations use separate `PORTCULLIS_TEST_DATABASE_URL` and `PORTCULLIS_TEST_TARGET_DATABASE_URL` settings. Qualification requires both the actual metadata baseline and requested target family checks, with unavailable databases failing when `PORTCULLIS_TEST_DATABASE_REQUIRED=1`. Reject unknown families rather than interpolating an unreviewed image. This fixes the PG16 `MAINTAIN` privilege regression without expanding the metadata deployment contract; that privilege exists in [PostgreSQL 17](https://www.postgresql.org/docs/17/ddl-priv.html) but not [16](https://www.postgresql.org/docs/16/ddl-priv.html).

Run target-family fixture/dialect scenarios and real-binary browser E2E in the compatibility matrix. Browser metadata remains PG18 and its target uses the selected family in an independent container. The ordinary CI gate runs shared unit/static/web/load/supply-chain checks once; repeating those independent checks in every target job adds no compatibility evidence. PostgreSQL 19 preview remains non-blocking until GA requalification.

This supersedes ADR-0001's open-ended PostgreSQL ≥14 / MySQL ≥8.0 support floor; it preserves its parser/driver choices and safety model. Amend both PRD translations and publish candidate versus verified status in the database feature matrix.

## Consequences

Expand the integration/CI image matrix and record each family's full gate before claiming support. Keep 19 preview separate from release-blocking GA jobs. Current evidence remains the recorded PostgreSQL 18 M1 environment; 16/17/19 and MySQL require qualification. A performance run on an older pinned patch is evidence for that patch only, not certification of the latest patch. Wider coverage may be introduced later with an explicit ADR/PRD update rather than becoming an automatic testing obligation. MySQL retains at most three initial candidate families; PostgreSQL has the explicit four-family exception.

## Primary sources (verified 2026-10-03)

- [PostgreSQL versioning and current supported patches](https://www.postgresql.org/support/versioning/).
- [PostgreSQL 19 release schedule](https://wiki.postgresql.org/wiki/PostgreSQL_19_Open_Items).
- [MySQL release tracks](https://dev.mysql.com/doc/refman/9.7/en/mysql-releases.html).
- [MySQL 26.7 releases](https://dev.mysql.com/doc/relnotes/mysql/26.7/en/).
- [MySQL downloads](https://dev.mysql.com/downloads/mysql/).
- [MySQL 8.0 Sustaining Support notice](https://www.mysql.com/support/eol-notice.html).
