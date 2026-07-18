# ADR-0007: Social login via Google (OIDC)

- **Status:** Accepted (amended 2026-07-04: PKCE method, redirect target, and the pending
  cookie's exact TTL pinned)
- **Date:** 2026-06-28 (amended 2026-07-04)

## Context
The MVP must support **Google social login** in addition to local email/password (ADR-0006). This
moves Google OAuth from post-MVP into the MVP, so the PRD is amended (§2.2, §2.3, §4.6, §5.1, §6,
§8.3). Broader OIDC/LDAP/SAML/SCIM and group-role sync stay post-MVP — only Google sign-in lands now.

Standards verified 2026-06-28 (Google, OpenID Connect, OWASP):
- Use the **Authorization Code flow with PKCE**; OIDC adds an ID token with verified identity.
- **`state`** (binds the callback to the user's session, prevents CSRF) and **`nonce`** (prevents
  ID-token replay) are both mandatory; `coreos/go-oidc` verifies the ID token signature/iss/aud/exp
  but **nonce validation is the caller's responsibility**.
- The stable identity is the **`sub`** (subject), unique per issuer and never reassigned; email can
  change, so link by `(issuer, sub)` and only trust email when **`email_verified`** is true.

## Decision

### Flow & libraries
- `golang.org/x/oauth2` for the OAuth2 exchange; `github.com/coreos/go-oidc/v3/oidc` for provider
  discovery and ID-token verification. Scopes exactly `openid email profile` — nothing else, and
  **no `access_type=offline`**: sign-in only needs the ID token once; a refresh token would be
  standing credential we never use (amended 2026-07-04 — least privilege).
- **PKCE method: `S256`** (RFC 7636 — `plain` is compatibility-only and prohibited here); use
  `oauth2.GenerateVerifier()` + `oauth2.S256ChallengeOption`.
- Two plain HTTP routes (the redirect flow does not fit Connect RPC):
  - `GET /auth/google/start` → generate `state`, `nonce`, PKCE `verifier`; 302 to Google's auth URL.
  - `GET /auth/google/callback` → validate `state`, exchange `code` (with PKCE verifier), verify the
    ID token (signature via JWKS, `iss`, `aud`=client id, `exp`) **and the `nonce`**, then sign in.

### Pending-auth state
- `state`, `nonce`, and the PKCE `code_verifier` are carried across the redirect in a **short-lived
  AEAD-encrypted cookie** (`crypto.Keyring` envelope, AAD record type `oidc_pending` — ADR-0003),
  `__Host-` prefixed, TTL **10 min** exactly (cookie `Max-Age` and an encrypted-payload expiry
  both enforced — the cookie attribute alone is client-controlled) — stateless, no table.

### Account model — link, never auto-create
- Identity link stored in **`oidc_identities(user_id, issuer, subject, email, created_at)`**,
  unique `(issuer, subject)`.
- Callback resolution: (1) `(issuer, sub)` already linked → that user; (2) else, if `email_verified`
  and the email matches an **existing admin-created user**, link `(issuer, sub)` to it on first
  login; (3) else **reject** — no public signup (PRD §8.3). Disabled users are rejected.
- On success, create a session via ADR-0006 (same cookie/CSRF machinery) and redirect to the app.
  For a first link, the identity insert, session rotation, and successful login audit event commit atomically
  (ADR-0009); `identity_linked=true` in that event is the self-contained link trail.

### Frontend integration — no client SDK
- The flow is **entirely server-side**. The SPA does **not** load the Google Identity Services JS
  SDK and never handles tokens; it only renders a plain "Continue with Google" link to
  `GET /auth/google/start`.
- The backend owns the redirect dance and, on success, sets the session cookie and 302-redirects
  back to the SPA at the fixed path **`/`** (never a client-supplied return URL — no open-redirect
  surface; the SPA routes from its own state after `Me`). Failures redirect to
  **`/login?error=oidc`** with no detail (specifics go to the server log only). So the frontend
  stays dumb and UI changes are trivial — the button is just an anchor; sign-in state is read
  from the session (the `Me` RPC).

### Config (Google login is optional)
- `PORTCULLIS_GOOGLE_CLIENT_ID`, `PORTCULLIS_GOOGLE_CLIENT_SECRET`, `PORTCULLIS_GOOGLE_REDIRECT_URL`.
  When unset, Google login is disabled and password login still works.
- **Redirect URL validation (amended 2026-07-13):** boot refuses a `google_redirect_url` that
  Google would reject at registration or that this server could never answer — the rules mirror
  Google's redirect-URI validation: **HTTPS required** (plain HTTP only for
  localhost/loopback), **no query, fragment, or userinfo**, **no raw non-loopback IP host**,
  and the **path must be exactly `/auth/google/callback`** (the only mounted callback route;
  `config.GoogleCallbackPath`, pinned to `connectapi.OIDCCallbackPattern` by a transport test).
  Previously only "absolute http(s) URL" was checked, so a URL that could never complete a
  login still booted.

## Consequences
- New deps: `golang.org/x/oauth2`, `github.com/coreos/go-oidc/v3`. New table `oidc_identities`
  (migration). New HTTP routes mounted alongside the Connect handlers. Session issuance reuses ADR-0006.
- "Link to existing, no auto-create" keeps the no-public-signup guarantee; admins still provision
  users, and Google is an authentication method, not a sign-up path. Multi-provider/LDAP/SAML remain
  out of scope.

## Sources (checked 2026-06-28; amendment items 2026-07-04)
- coreos/go-oidc: https://github.com/coreos/go-oidc
- Google OAuth 2.0 for web server apps: https://developers.google.com/identity/protocols/oauth2/web-server
- OpenID Connect Core (sub, nonce): https://openid.net/specs/openid-connect-core-1_0.html
- RFC 7636 (S256 mandatory-to-implement; plain discouraged): https://datatracker.ietf.org/doc/html/rfc7636
