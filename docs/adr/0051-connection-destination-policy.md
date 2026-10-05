# ADR-0051: Operator destination policy for connection dials

- **Status:** Accepted
- **Date:** 2026-10-04

## Context

Connection tests, connection create/update (which test before saving) and governed executions dial whatever host and port a connection names. Target validation (ADR-0014) checks only the shape of the host, so loopback, link-local cloud metadata (`169.254.169.254`), the metadata database and every RFC 1918 address were dialable.
The test result buckets (`unreachable`, `timeout`, `tls-failed`, `auth-failed`, `unknown-database`) differ per outcome, so a holder of `connections.test` could map the server's internal network and brute-force credentials of internal PostgreSQL servers it was never meant to reach. This is server-side request forgery (SSRF) through the database driver.

Portcullis exists to reach internal databases, so private ranges are its normal destinations (ADR-0042). A policy that refuses them by default would break the product; one that permits everything leaves the SSRF open.

## Options

- **Validate the host when the connection is saved.**
  - Refused: a hostname can resolve to a different address at dial time (DNS rebinding), and IP spellings vary, so a save-time check is a time-of-check/time-of-use gap.
- **Fixed deny list only.**
  - Refused as the sole control: OWASP calls deny lists bypass-prone and prefers allow lists.
- **Operator allow and deny CIDR lists enforced on resolved addresses at dial time, over a fixed always-refused baseline.**
  - Chosen.

## Decision

`connection.DestinationPolicy` (domain) decides whether one resolved address may be dialed. Addresses are canonicalized first: an IPv6 zone is dropped and an IPv4-mapped IPv6 address (`::ffff:a.b.c.d`) is unmapped, because CIDR prefixes contain neither form and would otherwise let those spellings bypass every rule. The rules are evaluated in order.

1. **Always refused**, whatever the operator configures:
   - `0.0.0.0/8` and `::/128` are unspecified; dialing `0.0.0.0` reaches the local host.
   - `169.254.0.0/16` and `fe80::/10` are link-local, including AWS/GCP/Azure/OCI metadata address `169.254.169.254`.
   - `224.0.0.0/4` and `ff00::/8` are multicast; `255.255.255.255/32` is broadcast.
   - `fd00:ec2::254/128` is AWS IPv6 metadata; `100.100.100.200/32` is Alibaba Cloud metadata.
2. **Operator deny list** (`PORTCULLIS_CONNECTION_DENIED_CIDRS`) refuses its ranges, including ranges inside the allow list.
3. **Operator allow list** (`PORTCULLIS_CONNECTION_ALLOWED_CIDRS`), when non-empty, permits only its ranges.
4. **Default** without an allow list: every remaining address except loopback (`127.0.0.0/8`, `::1`).

The default keeps private ranges reachable because they are where governed databases live, and refuses loopback because on a single host it exposes the metadata database and local services, and in Kubernetes it exposes the pod's sidecars.
A co-located database is enabled by naming loopback in the allow list, which also narrows every other destination; operators should set an allow list naming only their database subnets in production.
The metadata database is not refused automatically: single-cluster installations legitimately govern other databases on the same server, so an operator who keeps it separate denies its address explicitly.

Each list accepts at most 256 canonical CIDR entries. Entries without a prefix length, entries with host bits set, IPv4-mapped prefixes (they could never match an unmapped address) and zoned prefixes fail startup with an error naming the setting, matching the fail-fast handling of `trusted_proxies` (ADR-0010).

**Enforcement is at dial time on resolved addresses.** The PostgreSQL adapter sets pgconn's `LookupFunc` to resolve the host once, refuse the target unless every resolved address is permitted, and hand pgconn only those address literals, so the address that was checked is the address that is dialed and no second resolution can substitute another (DNS rebinding).
Its `DialFunc` re-checks the literal it is asked to dial, which also covers the out-of-band cancel request, and refuses anything that is not a permitted literal. Checking every address, not just the first, prevents a hostname that resolves to one allowed and one refused address from reaching the refused one through pgconn's multi-address fallback.
Connection tests, connection create/update and governed executions share this path.

**A refusal is one bucket.** Every refusal returns `destination-refused` without dialing, whichever rule or resolved address caused it, so a caller learns only that policy forbids the destination, never whether the address is reachable or what answers there.
For governed executions the request fails before any SQL is sent, and the circuit breaker records the attempt as not attempted rather than as target ill health.

## Consequences

- The browser E2E and load-test harnesses publish their Testcontainers targets on loopback, so they set the allow list to loopback plus private ranges; Go integration tests pass the same policy through `dbtest.TargetDestinationPolicy`.
- Local development that registers a target on `localhost` must set `PORTCULLIS_CONNECTION_ALLOWED_CIDRS`; `.env.example` documents it.
- Target validation rejects a host that is not a canonical IP literal but ends in a numeric label, such as `2130706433`, `0177.0.0.1`, `0x7f000001`, `127.1` or `169.254.169.254.`.
  No DNS hostname has an all-numeric top-level label, while inet_aton-style resolvers read these forms as IPv4, so rejecting them follows the OWASP advice to compare only the parsed address; dial-time checks would still refuse whatever such a spelling resolved to.
- Addresses that embed IPv4 in other IPv6 forms (NAT64 `64:ff9b::/96`, 6to4) are matched as IPv6; operators on such networks express the corresponding IPv6 ranges in their lists.

## Sources

- [OWASP SSRF Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html) — allow lists over deny lists, use the parsed address for comparison, DNS pinning, metadata endpoints and the private/loopback/link-local ranges.
- [pgconn Config](https://pkg.go.dev/github.com/jackc/pgx/v5/pgconn#Config) — `LookupFunc` and `DialFunc`; pgx v5 source shows every non-socket host, including IP literals, passing through `LookupFunc`.
- [AWS EC2 instance metadata endpoints](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/configuring-instance-metadata-service.html) — `169.254.169.254` and `fd00:ec2::254`.
- [Alibaba Cloud ECS metadata](https://www.alibabacloud.com/help/en/ecs/user-guide/view-instance-metadata/) — `100.100.100.200`.
