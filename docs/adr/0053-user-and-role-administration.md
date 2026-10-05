# ADR-0053: User and role administration with one-time password setup links

- **Status:** Accepted
- **Date:** 2026-10-04

## Context

PRD §3 and §8.3 promise that admins manage users and custom roles, that admin-created users receive a 24-hour one-time password setup link, and that Google sign-in links only to admin-created users (ADR-0007). ADR-0008 seeded `users.*` and `roles.*` permissions, but no API, use case or screen enforced them. Only the bootstrap admin could sign in, so distinct-reviewer quorum (PRD §4.3) and Google sign-in for anyone else were unreachable, and the alpha quickstart deferred user management to M4 while PRD §11 scheduled it nowhere.

The decision must keep authorization permission-based (ADR-0008), organization-scoped (ADR-0004), audited atomically (ADR-0009), and consistent with session rotation on privilege change (ADR-0006).

## Options

- **Configuration provisioning only.** Declarative users and roles reconciled at boot would avoid an admin UI, but the PRD requires interactive administration and setup links, and provenance ownership needs its own design.
- **Invitation email.** Out of MVP scope: PRD §8.3 displays the link once without email delivery, and outbound mail adds infrastructure.
- **Interactive Users and Roles services with displayed setup links (chosen).**

## Decision

### Users

- A `Users` Connect service provides List (`users.list`), Get (`users.get`), Create (`users.create`), IssueSetupLink (`users.update`), Disable and Enable (`users.disable`) and AssignRole (`users.update`).
- Users are listed through their membership in the caller's organization; a user without a membership in that organization is not found, so foreign identifiers cannot be probed or changed.
- A membership holds exactly one role, as the existing `organization_memberships (organization_id, user_id)` uniqueness already defines. AssignRole replaces that role; multiple roles per membership are deferred.
- Create takes a normalized email, a display name (the same rules as bootstrap) and an initial role, and returns the user plus a password setup token. Emails stay globally unique, so a taken email is refused as AlreadyExists to the administrator.

### Password setup links

- The token is 32 bytes from a CSPRNG, encoded base64url, and only its SHA-256 digest is stored, as for session tokens; a high-entropy random token does not need a slow password hash.
- A link is valid for 24 hours (`identity.PasswordSetupValidity`), and a partial unique index allows one open link per user. Issuing a new link revokes the previous open one in the same transaction.
- Links are issued only for active users that have no password yet. Administrator-initiated password reset of an existing password is deferred, because it would let a user manager take over a more privileged account.
- The SPA builds `/setup-password#token=<token>` from its own origin rather than a server-supplied host, and the token travels in the fragment so it never reaches server logs or a `Referer` header; the setup page also sets `no-referrer`.
- The public `Auth.CompletePasswordSetup` RPC shares the credential rate limits. It validates the password policy first, looks the token up, hashes the password, then in one transaction consumes the token with a conditional update (`consumed_at is null and revoked_at is null and expires_at > now()` on the database clock and the user still active), stores the password, revokes every session of the user and commits `USER_PASSWORD_SET`. A replayed, expired, revoked, unknown or disabled-user token returns one generic refusal. Completion does not sign the user in; they log in normally.
- Disabling a user revokes their open setup link.

### Roles

- A `Roles` Connect service provides List (`roles.list`), Get (`roles.get`), Create (`roles.create`), Update (`roles.update`), Delete (`roles.delete`) and ListPermissions (`roles.list`, the seeded catalog with descriptions for the role form).
- A custom role has a name of 1–64 Unicode code points without control or format characters, unique among the organization's live roles, and any subset of the loaded permission catalog. Unknown keys are refused.
- System roles are read-only: they cannot be renamed, changed or deleted.
- Update is a full replacement under optimistic concurrency: a new `roles.version` column must equal the version the caller read, otherwise Aborted.
- A removed permission's `role_permissions` row is soft-deleted (`deleted_at`) rather than erased, and re-adding the permission clears `deleted_at` on that row, so the `(role_id, permission_key)` key keeps one row per pair; every permission read, including approver eligibility, ignores deleted rows (`docs/conventions/data.md`).
- Delete is a soft delete (`deleted_at`) and is refused while any membership references the role, because a deleted role silently stops granting permissions to its members.

### Safeguards

- **No privilege escalation.** The actor must hold every permission of a role it creates, of both the old and the new permission set of a role it updates, of a role it assigns, and of the target user's current role when it assigns, disables or enables that user. A user manager therefore cannot grant, strip or lock out permissions it does not hold.
- **No self-administration.** An actor cannot disable, enable or reassign itself.
- **Last administrator.** ADR-0008 defines an administrator as an active member whose role holds both `users.update` and `users.disable`. Every disable, role assignment and role update runs inside a transaction holding a per-organization advisory lock, applies the change, then counts administrators and rolls back with FailedPrecondition when none remain. The lock serializes concurrent administrators so two cannot remove each other.
- The request-time permission check only admits the call. Inside the organization-locked transaction the store re-reads the actor's active status and current role permissions and re-runs the escalation guard against them, so an actor disabled or demoted by a concurrent administrator cannot complete a change it no longer holds the authority for.
- Every administration transaction that also touches a user's credentials takes the organization lock before any user row lock, so administration and password setup completion never wait on each other in opposite orders.

### Session revocation and audit

- Disable, Enable, AssignRole and any role Update revoke every active session of each affected user in the same transaction (ADR-0006, OWASP Session Management). The SPA's permission snapshot therefore never outlives a privilege change.
- New audit actions commit with their mutation: `USER_CREATED`, `USER_SETUP_LINK_ISSUED`, `USER_PASSWORD_SET`, `USER_DISABLED`, `USER_ENABLED`, `USER_ROLE_ASSIGNED`, `ROLE_CREATED`, `ROLE_UPDATED` and `ROLE_DELETED`, with target types `user` and `role`.
- Metadata carries role identifiers, added and removed permission keys and revoked-session counts, never tokens, digests, passwords or emails.
- Refusals for escalation, self-administration and last-administrator removal are recorded best-effort as `FAILED` events of the attempted action, like failed logins.

### Roadmap

PRD §11 places this slice in M1 (amended 2026-10-05; it was first scheduled at the start of M2).
It closes the multi-user gap that distinct-reviewer quorum and Google sign-in depend on, so the first releasable alpha includes it.
The SPA gains an Administration section with Users and Roles pages shown only to holders of `users.list` or `roles.list`.

## Consequences

- Migration 0022 adds `password_setup_tokens`, `roles.version` and `role_permissions.deleted_at`. The token table is runtime-writable current state (consume and revoke are updates); the audit log remains the evidence. Runtime DELETE stays revoked by the default privileges, including on `role_permissions`; `ROLE_UPDATED` metadata records the added and removed keys.
- All administration mutations, including user creation and role deletion, hold the per-organization advisory lock, so a role cannot be deleted while a concurrent assignment references it. Writes are stamped with the instant observed after the lock (ADR-0009).
- `identity.EnforcedPermissions` gains the `users.*` and `roles.*` keys, so startup refuses a catalog that lacks them.
- Revalidating unexecuted approvals from users who were disabled or lost approval permission (PRD §8.3) is a separate execution-path change and is not part of this slice.
- Deferred: multiple roles per membership, administrator-initiated password reset, email delivery, and configuration provisioning.

## Primary sources (verified 2026-10-04)

- [OWASP Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html): least privilege, deny by default, and access checks on every request for the specific object.
- [OWASP Forgot Password Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Forgot_Password_Cheat_Sheet.html): URL tokens are CSPRNG-generated, stored securely, single use and expiring, links avoid the Host header, pages set `no-referrer`, and users sign in normally afterwards.
- [OWASP Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html): session identifiers are renewed after any privilege level change, and administrative session controls are protected and logged.
