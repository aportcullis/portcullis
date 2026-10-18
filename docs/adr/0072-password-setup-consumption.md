# ADR-0072: Initial password setup consumption

- **Status:** Accepted
- **Date:** 2026-10-18
- **Implements:** ADR-0053's retained password setup contract and ADR-0071's remaining account activation obligation.

## Context

Administration issues digest-only, expiring setup tokens for active passwordless members. Completing setup must not become an administrator password reset or a second authorization source.

## Decision

Expose public `Auth.CompletePasswordSetup` under the same Host, required browser-origin evidence and credential IP limits as login. The request contains only the opaque token and new password; return an empty no-store acknowledgment without account identity or session cookies. Apply the existing NFC and password policy, resolve an eligible digest, hash outside metadata locks, then revalidate authoritatively during consumption. Unknown, expired, consumed, replaced, revoked, disabled, nonmember and already-passworded targets share one refusal. Infrastructure failures and cancellation remain distinct operational outcomes.

Resolve only the configured organization. Acquire class-5 organization coordination, the account/membership lock and class-2 session coordination in that order. Observe database time after all waits. Conditionally consume the still-open unexpired digest, insert a password without overwriting an existing one, revoke every user session and commit `USER_PASSWORD_SET` at that instant in one transaction. Preserve Role, Binding, revision and OIDC data. Failed audit or credential writes roll back consumption. Concurrent claims admit exactly one; disabling or replacing the token serializes with consumption. A user must sign in normally afterwards.

The SPA builds `/setup-password#token=...` using its own origin and shows the link only in the issuance receipt. A dedicated public page captures and immediately removes the fragment with history replacement, sets no-referrer, and never puts the token in query parameters, storage, telemetry or success navigation. GET never consumes it. Require matching password confirmation; use normal login after success. Clear token/password state on completion, definitive refusal and route disposal. Fence late responses, prevent duplicate submissions and do not automatically retry a write with an uncertain outcome. An operational failure offers an explicit retry or normal login; it never claims success. No email delivery or reset of an existing password is introduced.

## Qualification

Require scenario evidence for normal grantless/bound/OIDC members, normalization, expiry/replay/revocation/replacement, disabled and nonmember targets, session invalidation, competing consumption, expiry after lock waits and atomic audit failure. Raw transport and real-binary browser checks cover public admission, origin refusal, no automatic session, generic token failures, fragment removal, confirmation and normal login. Repository gates and staged review precede commit; other M1 acceptance remains separate.

## Sources

- [OWASP Forgot Password Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Forgot_Password_Cheat_Sheet.html), verified 2026-10-18: single-use expiring random tokens, secure password storage, confirmation, no-referrer, rate limits and normal login after completion.
- [OWASP CSRF Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html): retained Host/origin admission from ADR-0052 protects pre-session credential writes.
