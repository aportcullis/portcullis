# M1 progress

**Status: In progress · scope reviewed 2026-10-16 · verification reviewed 2026-10-18.**
Scope and acceptance live in [scope.md](scope.md); dated test evidence lives in [validation.md](validation.md).

## Established implementation

- PostgreSQL connection/policy → immutable request → distinct approval → single-use execution → bounded encrypted results/CSV → audit is implemented.
- Historical `make verify` evidence includes real-browser native CSV saving/readback in the recorded Docker-hosted Chromium environment.
- User/role domain, application services and PostgreSQL administration persistence are present.
- Password setup token issuance and persistence are implemented.
- Result controls have aligned search, copy/export and paging controls; Text values wrap without dropping content at tested viewport widths in both themes.
- Result tables use subtle dashed column separators and show a sorting arrow only on the active column. Table headers cycle sorting without a separate ordering selector.

Per [ADR-0053](../../adr/0053-user-and-role-administration.md), user and role administration is M1 scope.
Per [administration use cases](../../../internal/app/administration/users.go) and [administration persistence](../../../internal/infra/postgres/administration_store.go), backend components exist without establishing a complete administration UI.

## Accepted authorization replacement

[ADR-0062](../../adr/0062-role-bindings-and-conditions.md) separates membership, roles, scoped bindings and conditions within M1. The current code still resolves one membership role into flat organization permissions; no binding schema, condition evaluator or resource-aware replacement is delivered by the documentation decision. The existing administration services and historical governance evidence therefore do not qualify the new boundary. Scope and both PRD translations now require grant-preserving migration, resource-aware list/action checks, conservative policy management and concurrent revocation/expiry scenarios.

[ADR-0063](../../adr/0063-condition-dsl-and-pure-evaluation.md) clarifies Role/Binding as data, Condition as a restricted CEL DSL, and evaluation as a pure function over checked programs and trusted immutable inputs. Preparation, attribute/time resolution, auditing and condition forms remain separate responsibilities; this clarification adds no runtime implementation or qualification evidence.

## Verification status

The recorded PostgreSQL governance tests passed in the documented Docker-browser environment.
The [2026-10-18 verification](validation.md#verification--2026-10-18) passed both required gates after the dependency security update, including all 38 real-binary browser scenarios. The accepted Role/Binding/Condition replacement remains unimplemented and is not qualified by those existing scenarios.
This evidence covers the tested journey and does not establish completion of the entire M1 scope or capacity qualification.
Per [scope.md](scope.md), release readiness requires all milestone completion criteria and both verification gates.
