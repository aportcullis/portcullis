# Recommended deployment architecture

Keep Portcullis, its metadata database and governed targets inside a private network. Remote users connect to the application's private HTTPS endpoint through **Cloudflare WARP or Tailscale**. Choose one remote-access path; they are alternatives, not successive layers.

This is production deployment guidance ([ADR-0042](../adr/0042-private-network-deployment.md)), not a provider integration or an enforced application setting. Box boundaries and arrows distinguish remote access from server-to-database traffic.

## Private-network topology

```text
                         ┌───────────────────────────────────────────┐
                         │ Users / Developers                        │
                         │ Managed device + browser                  │
                         └───────────────────────────────────────────┘
                                               │
                                               ▼
                         ┌───────────────────────────────────────────┐
                         │ Private remote access (choose one)        │
                         │ Cloudflare WARP OR Tailscale              │
                         │ Enrolled device + scoped access policy    │
                         └───────────────────────────────────────────┘
                                               │
┌─ Private network ───────────────────────────────────────────────────────────────────────────┐
│                                              ▼                                              │
│                        ┌───────────────────────────────────────────┐                        │
│                        │ Selected private ingress                  │                        │
│                        │ Connector / node / subnet router          │                        │
│                        │ App HTTPS only                            │                        │
│                        └───────────────────────────────────────────┘                        │
│                                              │                                              │
│                                              ▼                                              │
│                        ┌───────────────────────────────────────────┐                        │
│                        │ Private HTTPS reverse proxy               │                        │
│                        │ Internal DNS + trusted TLS :443           │                        │
│                        │ Allow remote group to app only            │                        │
│                        └───────────────────────────────────────────┘                        │
│                                              │                                              │
│                                              ▼                                              │
│                        ┌───────────────────────────────────────────┐                        │
│                        │ Portcullis / one instance                 │                        │
│                        │ Embedded UI + API + execution             │                        │
│                        └───────────────────────────────────────────┘                        │
│                                              │                                              │
│                      ┌───────────────────────┴───────────────────────┐                      │
│                      │                                               │                      │
│                      ▼                                               ▼                      │
│    ┌───────────────────────────────────┐           ┌───────────────────────────────────┐    │
│    │ Metadata PostgreSQL 18            │           │ Governed target databases         │    │
│    │ Restricted runtime DB account     │           │ Least-privilege target accounts   │    │
│    │ Requests / audit / result storage │           │ Verified database TLS             │    │
│    └───────────────────────────────────┘           └───────────────────────────────────┘    │
│                                                                                             │
│    Server-to-DB traffic stays inside the private network                                    │
│    Secrets + persistent master key / backups / monitoring                                   │
│    No direct developer route to database ports                                              │
└─────────────────────────────────────────────────────────────────────────────────────────────┘
```

The diagram shows one remote-access layer with a choice of provider. Deploy the selected provider’s private ingress: a `cloudflared` connector for Cloudflare WARP, or a Tailscale node/subnet router for Tailscale. Provider-specific setup is compared below.
Arrows show application traffic, not connection initiation: `cloudflared` initiates its tunnel outward, while Tailscale carries encrypted peer traffic directly or through a relay. The selected path reaches the private HTTPS proxy. Restrict its upstream application connection to the local host/private network, or encrypt and authenticate that hop when crossing a trust boundary.

The developer path stops at HTTPS. Portcullis separately connects to metadata and governed targets; users do not need direct DB network access to execute approved SQL in the web UI. PostgreSQL is the currently shipped target; see the [database feature matrix](../product/database-support.md) for MySQL implementation and qualification status.

## Choose the remote-access path

| Area | Cloudflare WARP | Tailscale |
|---|---|---|
| User device | Organization-enrolled Cloudflare One Client in the required WARP mode | Enrolled device in your tailnet |
| Private ingress | `cloudflared` connector with a private IP/CIDR or hostname route | Tailscale on the ingress host, or a subnet router for an existing private ingress |
| Access scope | Gateway/network policies allow the approved group/device to the app destination and HTTPS port; deny broader private access | Explicit grants allow the approved group/device to the ingress destination and HTTPS port; route approval alone is not permission |
| DNS | Private hostname resolution plus the required WARP routes/split-tunnel configuration | Tailnet host naming for a direct node, or private DNS/split DNS for a routed hostname |
| Exposure | Private-network routing without publishing a public application hostname | Tailnet/private routing; do not enable Funnel for this topology |

Consumer WARP does not grant access to an organization's private routes. Enroll devices in the organization's Zero Trust configuration and configure routing, DNS and network policy together. A configured Tunnel alone is not the user/device authorization boundary.
See [Cloudflare private networks](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/private-net/) and [connecting with cloudflared](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/private-net/cloudflared/).

Install a Tailscale node on the ingress host when feasible; use a [subnet router](https://tailscale.com/docs/features/subnet-routers) for private destinations that cannot run Tailscale. Advertise only the needed destination, approve its route and configure access independently.
Use grants for new access policies as recommended in [Tailscale's policy guidance](https://tailscale.com/docs/reference/examples/acls). Remove broad default access that would expose unrelated subnets or DB ports.

## Enforce the boundary

| Flow | Recommended rule |
|---|---|
| Approved remote users → private HTTPS proxy | Allow the chosen WARP/Tailscale path to the app destination on HTTPS |
| Public Internet → proxy, application listener or DB | Deny inbound app/DB exposure; retain provider-required connector egress and overlay connectivity |
| Proxy → Portcullis listener | Allow only the actual proxy/local network; do not publish the upstream app port publicly |
| Portcullis → metadata PostgreSQL | Allow the specific private endpoint with verified TLS and the restricted runtime role |
| Portcullis → governed target | Allow intended registered destinations with verified TLS and dedicated least-privilege credentials |
| Remote users → metadata or target DB ports | Deny through provider policy and host/VPC firewall; separate DBA access requires its own policy |
| Migration process → metadata PostgreSQL | Separate owner credential/process from runtime; allow controlled schema migrations only |

Keep application login, session/CSRF checks, RBAC, distinct review, single-use execution leases and auditing enabled. Network enrollment permits connectivity and does not log a user into Portcullis. Browser HTTPS still requires a trusted certificate and the correct application origin; an encrypted overlay does not remove those requirements.
Provider identity is not automatically mapped to an application account.

Set `PORTCULLIS_PUBLIC_ORIGINS` to the exact browser origin users open, such as `https://portcullis.internal.example`. Portcullis refuses requests whose `Host` names no configured origin with `421 Misdirected Request`, which defeats DNS rebinding from a page on another domain, and it refuses cross-origin browser writes (ADR-0052).
Unset, only loopback hosts are admitted; that suits the local demo but returns 421 behind a proxy or on a LAN address. The reverse proxy must forward the original `Host` header (for example, nginx `proxy_set_header Host $host`; Caddy forwards it by default). Health probes on `/livez` and `/readyz` are exempt so probes addressing a pod IP keep working.

Trust only actual reverse-proxy source CIDRs and sanitize forwarding headers at that proxy. A Tailscale subnet router uses SNAT by default, so the application may see the router instead of the original device IP. Preserve attribution through authenticated users and audit records, and validate the actual proxy/routing chain before relying on source-IP rate limits.
See [Tailscale SNAT behavior](https://tailscale.com/docs/features/subnet-routers#disable-snat) and [runtime configuration](../../internal/platform/config/config.go).

Cloudflare private-network `cloudflared` traffic likewise reaches internal services with the connector host's local source IP, as described in [the connector guide](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/private-net/cloudflared/). Do not assume either remote-access path preserves individual device IPs at the application.

## Deployment and operating scope

Start with one Portcullis instance on a private host or container network. The [Compose quickstart](pg-alpha-quickstart.md) is a local demonstration: its `127.0.0.1:8080:8080` port publishing now limits host access to IPv4 loopback. This is a safer local default, not complete production isolation.
Place the private HTTPS proxy in front and enforce host/network firewall controls; do not override the bind with a wildcard public address. Operators must also account for Docker routing and daemon configuration.

Preserve metadata storage, the master key and recoverable backups separately from application containers. Use managed or mounted runtime secrets, keep migration-owner credentials out of runtime and test restoration with the preserved key. Result storage currently uses PostgreSQL; this recommendation does not require or provide Valkey.

Helm/Kustomize and CNPG are planned in M2 ([ADR-0035](../adr/0035-kubernetes-cnpg-after-mysql.md)); they are not installed by this diagram. Their deployment should retain the same private ingress and restricted network paths. CNPG database HA does not establish application HA; network connectors do not share local execution cancellation state across application replicas.

Before relying on a deployment, verify login, review, execution and CSV export from an enrolled device. Check from an unenrolled device that app/DB access is denied; verify that the allowed group cannot bypass the proxy or reach DB ports. Test revoked device access and DNS/certificate behavior after connector or application restart.
Keep infrastructure access logs and business audit records separately attributable. Provider-specific deployment has not been integration-tested in this repository; operators must verify their policies and routes.

## Related documents

- [Source architecture and layer boundaries](../ARCHITECTURE.md).
- [Database support and acceptance evidence](../product/database-support.md).
- [PostgreSQL alpha quickstart](pg-alpha-quickstart.md).
- [Encryption key rotation](key-rotation.md).
