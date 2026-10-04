# ADR-0052: Pre-session Host, origin and setup-token boundary

- **Status:** Accepted
- **Date:** 2026-10-04

## Context

Portcullis is deployed on private networks (ADR-0042), and the local demo listens on `127.0.0.1:8080` over HTTP. The server accepted any `Host` header, and the pre-session RPCs `Auth.Bootstrap`, `Auth.Login` and `Auth.GetConfig` need no cookie or CSRF token (ADR-0006). A page on `attacker.example` whose DNS record is rebound to the Portcullis address therefore becomes same-origin with the server from the browser's point of view: it can read `GetConfig`, claim the first administrator on a fresh install, and run password guesses from inside the network perimeter. A cross-site form or fetch can also drive login CSRF, because pre-session requests carry no token.

Browsers derive `Host` from the URL they loaded, so a rebound page always presents its own domain. Validating `Host` against an explicit allowlist is the standard defense against DNS rebinding, as Django's `ALLOWED_HOSTS` does, and the OWASP CSRF cheat sheet recommends verifying `Origin` or Fetch Metadata (`Sec-Fetch-Site`) on unsafe requests, blocking when neither header is present. Go 1.25 ships `net/http.CrossOriginProtection`, which rejects unsafe cross-origin browser requests using `Sec-Fetch-Site`, falling back to comparing `Origin` with `Host`, and admits requests carrying neither header as same-origin or non-browser.

Even with the origin boundary, the first caller that reaches `/bootstrap` on a fresh install owns it: anyone on the network path, or an allowed origin opened by the wrong person, can claim the administrator before the operator does. Jenkins unlocks its setup wizard with a generated password stored under `$JENKINS_HOME/secrets/initialAdminPassword` and printed to the log, and GitLab writes a random initial root password to `/etc/gitlab/initial_root_password` for 24 hours. Both prove possession of the server's filesystem or console before granting the first administrator.

## Options

- **Trust the network boundary.** Rebinding turns any browser inside the boundary into the attacker's proxy, so the network alone cannot protect pre-session RPCs.
- **Tier-C runtime setting (ADR-0017).** The allowlist is evaluated on every request before authentication and before the settings store is reachable, and an administrator session could widen it from inside the browser it protects. It is therefore boot-only configuration, like trusted proxies.
- **Require `Origin` on every unsafe request.** Authenticated RPCs already require the HMAC double-submit CSRF token, and non-browser clients such as the load harness send no `Origin`. Requiring it there adds no protection and breaks those clients.
- **Allow any host when unset.** Unset configuration would leave the demo exposed to rebinding, which is the reported problem.
- **Process-local setup token.** A token held only in memory diverges between instances and would be lost on restart without a durable record of consumption. Storing only its SHA-256 hash in the metadata database lets every instance verify it and consume it atomically.
- **Require configuration-based bootstrap only.** `PORTCULLIS_BOOTSTRAP_ADMIN_*` remains supported, but removing the interactive form would break the Compose quickstart.

## Decision

### Public origins and Host admission

`PORTCULLIS_PUBLIC_ORIGINS` is a comma-separated list of absolute browser origins (`scheme://host[:port]`, http or https, no path, query, fragment, userinfo or wildcard). Load normalizes them (lowercase, default port elided) and refuses startup on a malformed entry without echoing the value. Every request except `GET /livez` and `GET /readyz` must carry a `Host` that names a configured origin, with or without its default port; otherwise the server answers `421 Misdirected Request` (RFC 9110 §15.5.20) before routing. Health probes stay exempt because kubelet and container probes address the pod IP and the endpoints expose no application data.

When the setting is unset, only loopback hosts are admitted: `localhost` and loopback IP literals (`127.0.0.0/8`, `[::1]`) on any port. This keeps the Compose demo and the browser E2E harness working and still defeats rebinding, because a rebound page cannot present a loopback name as its `Host`. Startup logs a warning naming the risk: a LAN, proxied or Kubernetes deployment must set the origin users open, and its reverse proxy must forward the original `Host`.

### Cross-origin browser writes

After Host admission, the server wraps the application in `http.CrossOriginProtection`, trusting the configured origins. Unsafe requests with `Sec-Fetch-Site` other than `same-origin`/`none`, or with an `Origin` whose host differs from `Host` (including `null`), receive `403`. A different port on the same loopback name is a different origin and is refused.

For the pre-session credential procedures `Auth.Bootstrap` and `Auth.Login`, an unsafe request carrying neither `Origin` nor `Sec-Fetch-Site` is also refused with `403`, following the OWASP recommendation to block when origin evidence is absent. Modern browsers attach these headers to every `fetch` POST, so the SPA is unaffected; scripted clients set `Origin` explicitly. `connectapi.BrowserOriginRequiredProcedures` names these procedures once, and the composition root passes them to the HTTP boundary. `Auth.GetConfig` is a read that the Host check already protects, and authenticated RPCs keep their CSRF token as the write defense.

### First-run setup token

After the optional configuration-based bootstrap (ADR-0006), every start of an installation with zero users issues a setup token: 32 CSPRNG bytes encoded as base64url. Only its SHA-256 hash is stored in `setup_tokens`, with `created_at` and `expires_at` taken from one database-clock observation; the lifetime is pinned at 24 hours, following GitLab's initial-password window. Issuing runs under the bootstrap advisory lock, refuses with `ErrAlreadyBootstrapped` once a user exists, and soft-revokes the outstanding token (`deleted_at`) before inserting the new one, so a restart replaces a lost or leaked token and a partial unique index keeps at most one outstanding. Rows are never deleted; the runtime role inserts and updates them.

The raw token is handed over once. When `PORTCULLIS_SETUP_TOKEN_FILE` is set, it is written to that file with mode `0600` (narrowing an existing file) and the log names only the path; otherwise it is logged once at warning level with its lifetime, as Jenkins does. A delivery failure refuses startup, because an operator who cannot read the token could never bootstrap. An already bootstrapped installation issues and logs nothing.

`Auth.Bootstrap` carries `setup_token`. The service trims surrounding whitespace, refuses an empty or oversized token before hashing, and passes the hash to `BootstrapAdmin`, which, under the same advisory lock and after the zero-user check, consumes the outstanding unexpired token with one conditional `UPDATE` in the admin-creation transaction. Zero updated rows roll back the transaction with `ErrSetupTokenInvalid`, mapped to `PermissionDenied` with one message for missing, wrong, expired, rotated and consumed tokens; the attempt is recorded as a failed `AUTH_BOOTSTRAP` event. Once an administrator exists, any token yields `FailedPrecondition`, so concurrent claims with one token admit exactly one administrator. Configuration-based provisioning passes no token because the operator already controls the process environment. The SPA bootstrap form asks for the token and explains how to obtain a fresh one.

## Consequences

Existing non-loopback deployments must set `PORTCULLIS_PUBLIC_ORIGINS` before upgrading, or every application request returns 421. The `.env.example` file and the recommended architecture document describe the setting and the proxy `Host` requirement. Raw API scenarios that call the credential procedures must send a same-origin `Origin`.

Interactive bootstrap now needs access to the server log or token file. Logged tokens can reach log aggregation, so operators who ship logs off-host should configure the token file; a consumed, rotated or expired token is useless. Migration `0021_setup_tokens` adds the table, the quickstart and README explain where to find the token, and the browser E2E harness reads it from its token file. Both PRD translations record the boundary and the setup token in §8.3.

## Sources

- [OWASP CSRF Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html) (checked 2026-10-04): Fetch Metadata, `Origin` verification and blocking requests without origin evidence.
- [Go `net/http.CrossOriginProtection`](https://pkg.go.dev/net/http#CrossOriginProtection) (checked 2026-10-04).
- [Django `ALLOWED_HOSTS`](https://docs.djangoproject.com/en/stable/ref/settings/#allowed-hosts) (checked 2026-10-04): Host allowlisting against Host header attacks, with a loopback-only fallback for unconfigured local development.
- [MDN `Sec-Fetch-Site`](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Sec-Fetch-Site).
- [RFC 9110 §15.5.20, 421 Misdirected Request](https://www.rfc-editor.org/rfc/rfc9110#section-15.5.20).
- [Jenkins: unlocking Jenkins](https://www.jenkins.io/doc/book/installing/linux/#unlocking-jenkins) (checked 2026-10-04): generated initial admin password in a secrets file and the log.
- [GitLab Linux package installation](https://docs.gitlab.com/install/package/ubuntu/) (checked 2026-10-04): random initial root password file removed after 24 hours.
