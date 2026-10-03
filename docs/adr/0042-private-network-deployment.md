# ADR-0042: Private-network deployment recommendation

- **Status:** Accepted
- **Date:** 2026-10-03

## Context

The owner requests a separate recommended deployment document using box-and-arrow text diagrams. The recommendation must keep Portcullis inside a private network and let remote users connect through Cloudflare WARP or Tailscale. This is infrastructure guidance, not an automatic network restriction added to the application.

## Decision

Recommend a private HTTPS reverse proxy and one Portcullis instance, with private metadata and target databases. Remote access uses either organization-enrolled Cloudflare WARP plus a private-network cloudflared connector, or a Tailscale node/subnet router with explicit grants. Restrict the remote group to the application's HTTPS destination and deny direct metadata/target DB access. Retain Portcullis authentication, authorization, approval and audit controls independently of network membership.

Describe private DNS, browser-trusted TLS, source/proxy attribution and the host/network firewall as operator responsibilities. Keep public app publishing and Tailscale Funnel outside this recommended topology. Show the two connectivity alternatives in one diagram and separate network access from server-to-database traffic. Document currently shipped Compose/single-process behavior and planned Helm/Kustomize/CNPG deployment without implying application HA or tested provider integration.

## Consequences

The recommendation requires provider enrollment, route/access policies and reachable private DNS to be configured by operators. A private tunnel still uses external connectivity; private-only describes application and database exposure. Documentation changes do not alter the existing local-demo listener, Compose port publishing or authentication model. Both PRD translations record the production deployment recommendation.

## Sources

- [Cloudflare private networks](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/private-net/).
- [Tailscale subnet routers](https://tailscale.com/docs/features/subnet-routers).
- [Tailscale access policy guidance](https://tailscale.com/docs/reference/examples/acls).
