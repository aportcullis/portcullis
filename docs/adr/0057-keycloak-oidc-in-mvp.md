# ADR-0057: Keycloak OIDC sign-in in the MVP

- **Status:** Accepted
- **Date:** 2026-10-05
- **Supersedes:** [ADR-0007](0007-social-login-google-oidc.md) provider-scope restriction; its Google flow and linking rules remain binding.

## Context

The product owner requests Keycloak integration in M2 or M3 for self-hosted teams using company SSO. M2 already covers MySQL parity, Kubernetes/CNPG and SQL review; M3 completes the team-facing MVP.
Per [Keycloak OIDC documentation](https://www.keycloak.org/securing-apps/oidc-layers), Keycloak exposes realm discovery and standard authorization-code endpoints.
Per [Keycloak integration guidance](https://www.keycloak.org/securing-apps/overview), applications can use standard protocol libraries rather than a Keycloak-specific adapter.

## Options

- Add Keycloak during M2, expanding the database/deployment milestone.
- Add bounded OIDC sign-in during M3 while retaining local application authorization.
- Leave all non-Google providers after MVP, retaining the previous scope restriction.

## Decision

Place optional Keycloak sign-in in M3. Support one operator-configured realm as the first additional provider through the existing OIDC library family, rather than embedding Keycloak or introducing its administration API into Portcullis.
Use server-side Authorization Code with PKCE S256, mandatory state/nonce and validated token signature, issuer, audience and expiry. Provider configuration is operator-controlled; discovery and callback validation must not accept arbitrary user-supplied endpoints.

Identity resolution uses the configured `(issuer, subject)` pair. An administrator explicitly binds this pair to an existing Portcullis user; email equality alone never creates a link or user. Preserve ADR-0007's complete Google-specific flow, pending-cookie protections and authoritative-email linking rules.
Keycloak authenticates identity; Portcullis remains authoritative for account status, organization membership, roles, approvals and execution. Do not infer application privileges from realm/client roles or group claims.

Successful sign-in issues ADR-0006's existing HttpOnly server session with its CSRF, expiry and local revocation rules. Provider tokens and client secrets stay server-side and out of browser storage and logs. No offline credentials are required for this login-only slice; do not retain refresh tokens.
Local logout revokes the Portcullis session. Global IdP logout, back-channel logout, automatic upstream-disable propagation, LDAP/SAML/SCIM, additional providers and group-role synchronization require later scope and decisions. Do not claim that an upstream logout or account change immediately terminates an existing local session.
Local administrator access remains available for recovery during provider failure. Keep Google and local login optionality and deny failed SSO without downgrading validation or granting access.

## Consequences

The provider-scope restriction in ADR-0007 is replaced; all its Google-specific security contracts remain applicable through this decision. Neither acceptance of this ADR nor existing Google support establishes implemented Keycloak compatibility.
Per [M3 scope](../milestones/m3/scope.md), acceptance requires a pinned real Keycloak fixture and successful, denied, replay, token-validation, account-linking, local-revocation and provider-failure scenarios alongside existing authentication regressions.
Keycloak deployment remains external. Realm/client configuration, identity binding and the operational local-session boundary need documentation before the feature is advertised as supported.
