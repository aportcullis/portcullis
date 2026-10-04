# ADR-0052: Pre-session Host and origin boundary

- **Status:** Accepted
- **Date:** 2026-10-04

## Context

Portcullis is deployed on private networks (ADR-0042), and the local demo listens on `127.0.0.1:8080` over HTTP. The server accepted any `Host` header, and the pre-session RPCs `Auth.Bootstrap`, `Auth.Login` and `Auth.GetConfig` need no cookie or CSRF token (ADR-0006). A page on `attacker.example` whose DNS record is rebound to the Portcullis address therefore becomes same-origin with the server from the browser's point of view: it can read `GetConfig`, claim the first administrator on a fresh install, and run password guesses from inside the network perimeter. A cross-site form or fetch can also drive login CSRF, because pre-session requests carry no token.

Browsers derive `Host` from the URL they loaded, so a rebound page always presents its own domain. Validating `Host` against an explicit allowlist is the standard defense against DNS rebinding, as Django's `ALLOWED_HOSTS` does, and the OWASP CSRF cheat sheet recommends verifying `Origin` or Fetch Metadata (`Sec-Fetch-Site`) on unsafe requests, blocking when neither header is present. Go 1.25 ships `net/http.CrossOriginProtection`, which rejects unsafe cross-origin browser requests using `Sec-Fetch-Site`, falling back to comparing `Origin` with `Host`, and admits requests carrying neither header as same-origin or non-browser.

## Options

- **Trust the network boundary.** Rebinding turns any browser inside the boundary into the attacker's proxy, so the network alone cannot protect pre-session RPCs.
- **Tier-C runtime setting (ADR-0017).** The allowlist is evaluated on every request before authentication and before the settings store is reachable, and an administrator session could widen it from inside the browser it protects. It is therefore boot-only configuration, like trusted proxies.
- **Require `Origin` on every unsafe request.** Authenticated RPCs already require the HMAC double-submit CSRF token, and non-browser clients such as the load harness send no `Origin`. Requiring it there adds no protection and breaks those clients.
- **Allow any host when unset.** Unset configuration would leave the demo exposed to rebinding, which is the reported problem.

## Decision

### Public origins and Host admission

`PORTCULLIS_PUBLIC_ORIGINS` is a comma-separated list of absolute browser origins (`scheme://host[:port]`, http or https, no path, query, fragment, userinfo or wildcard). Load normalizes them (lowercase, default port elided) and refuses startup on a malformed entry without echoing the value. Every request except `GET /livez` and `GET /readyz` must carry a `Host` that names a configured origin, with or without its default port; otherwise the server answers `421 Misdirected Request` (RFC 9110 §15.5.20) before routing. Health probes stay exempt because kubelet and container probes address the pod IP and the endpoints expose no application data.

When the setting is unset, only loopback hosts are admitted: `localhost` and loopback IP literals (`127.0.0.0/8`, `[::1]`) on any port. This keeps the Compose demo and the browser E2E harness working and still defeats rebinding, because a rebound page cannot present a loopback name as its `Host`. Startup logs a warning naming the risk: a LAN, proxied or Kubernetes deployment must set the origin users open, and its reverse proxy must forward the original `Host`.

### Cross-origin browser writes

After Host admission, the server wraps the application in `http.CrossOriginProtection`, trusting the configured origins. Unsafe requests with `Sec-Fetch-Site` other than `same-origin`/`none`, or with an `Origin` whose host differs from `Host` (including `null`), receive `403`. A different port on the same loopback name is a different origin and is refused.

For the pre-session credential procedures `Auth.Bootstrap` and `Auth.Login`, an unsafe request carrying neither `Origin` nor `Sec-Fetch-Site` is also refused with `403`, following the OWASP recommendation to block when origin evidence is absent. Modern browsers attach these headers to every `fetch` POST, so the SPA is unaffected; scripted clients set `Origin` explicitly. `connectapi.BrowserOriginRequiredProcedures` names these procedures once, and the composition root passes them to the HTTP boundary. `Auth.GetConfig` is a read that the Host check already protects, and authenticated RPCs keep their CSRF token as the write defense.

## Consequences

Existing non-loopback deployments must set `PORTCULLIS_PUBLIC_ORIGINS` before upgrading, or every application request returns 421. The `.env.example` file and the recommended architecture document describe the setting and the proxy `Host` requirement. Raw API scenarios that call the credential procedures must send a same-origin `Origin`. Both PRD translations record the boundary in §8.3.

## Sources

- [OWASP CSRF Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html) (checked 2026-10-04): Fetch Metadata, `Origin` verification and blocking requests without origin evidence.
- [Go `net/http.CrossOriginProtection`](https://pkg.go.dev/net/http#CrossOriginProtection) (checked 2026-10-04).
- [Django `ALLOWED_HOSTS`](https://docs.djangoproject.com/en/stable/ref/settings/#allowed-hosts) (checked 2026-10-04): Host allowlisting against Host header attacks, with a loopback-only fallback for unconfigured local development.
- [MDN `Sec-Fetch-Site`](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Sec-Fetch-Site).
- [RFC 9110 §15.5.20, 421 Misdirected Request](https://www.rfc-editor.org/rfc/rfc9110#section-15.5.20).
