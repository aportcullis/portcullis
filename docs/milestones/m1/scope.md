# M1 · Gate

**In progress — first PostgreSQL alpha boundary.**

Give teams one complete path from connection registration through policy, request, distinct review, single-use execution, result exploration, and audit. Preserve exact result values and explicitly report uncertain outcomes without automatically rerunning SQL.

User and custom-role administration with one-time password setup links belongs to this milestone.
Administrators can create users, assign roles, disable/enable accounts and manage custom permission bundles without escalating their own authority.
Revalidate active approver eligibility before execution, serialize permission removal with approval/execution checks, revoke affected sessions and audit every administration mutation.

## Acceptance

- The PostgreSQL governance journey works from bootstrap through distinct review, one execution, exact results/CSV and audit.
- Users and Roles RPCs and permission-aware SPA pages support administration and one-time password setup.
- Cross-organization access, privilege escalation, self-administration and last-administrator removal are refused, including concurrent changes.
- Disabled or demoted approvers cannot supply a still-valid quorum; concurrent role-permission removal cannot bypass this rule.
- Release admission binds a tag to its release branch and publication promotes the verified image digest.
- Build version appears only in startup logs; Health responses do not expose it.
- Review findings, documentation and CHANGELOG are resolved, and both `make verify` and `make supply-chain` pass before release readiness.

Per [ADR-0053](../../adr/0053-user-and-role-administration.md), administration closes the multi-user gap in the first alpha.
See [progress](progress.md) for implementation status and [validation](validation.md) for recorded governance evidence.
