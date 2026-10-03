# ADR-0035: Kubernetes and CloudNativePG immediately after MySQL parity

- Status: Accepted
- Date: 2026-10-03

## Context

The owner requested Kubernetes and CloudNativePG (CNPG) support immediately after MySQL. The previous deployment plan deferred Helm/CNPG to M6, while M2 moved straight from MySQL parity to SQL review. Earlier requests already selected both Helm and Kustomize. CNPG application guidance recommends stable Service DNS and exposes generated credentials for the database owner; the latter cannot replace Portcullis's restricted metadata runtime role.

## Decision

Keep milestone identifiers and the M3 MVP boundary. Sequence M2 as MySQL governance parity → Kubernetes/CNPG deployment acceptance → SQL review/basic EXPLAIN. Amend both PRDs, the public roadmap and support matrix together. This records future scope, not delivered support.

Provide a Helm chart and Kustomize base/overlays with equivalent application settings. Initially deploy one Portcullis replica with external or CNPG-managed metadata PostgreSQL 18. Connect governed CNPG PostgreSQL targets through primary read-write Service DNS, verified TLS and existing database-version qualifications. Keep the operator independently installed and managed by the operator's administrator; no operator lifecycle controller or new Portcullis CRD is required.

Use existing Secrets and mounted master keys, and separate an owner-role migration Job from the restricted application runtime role. Do not inject CNPG's generated database-owner or superuser credentials into runtime. Include probes, resource/security settings and least privilege without granting Kubernetes API discovery access by default.

Require installation, upgrade/restart, credential/certificate rotation, key preservation, backup/restore and failover scenarios on pinned, published Kubernetes/CNPG/tool versions before claiming support. Preserve execution leases and unknown outcomes across connection loss; never retry target SQL automatically. Document UNLOGGED result-cache loss as `result_unavailable`, without rerunning approved SQL. CNPG HA alone does not qualify Portcullis for multiple replicas.

## Consequences

Deployment becomes an M2/MVP acceptance gate and increases its scope. Select precise supported Kubernetes/CNPG versions and migration delivery details through follow-up implementation decisions and real-cluster evidence. CNPG automatic discovery remains M7. Terraform/OpenTofu remains M6 after API stability; Gateway-specific deployment additions, masking prerequisites and registered-agent MCP ordering remain unchanged.

## Primary sources (verified 2026-10-03)

- [CNPG application connections](https://cloudnative-pg.io/docs/1.28/applications/): Service DNS, primary endpoint and database-owner credential distinction.
- [Kubernetes Kustomize guidance](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/kustomization/): declarative bases and overlays.
- [Helm chart best practices](https://helm.sh/docs/chart_best_practices/): chart structure and configuration guidance.

Documentation versions are research references, not the qualified Portcullis support matrix.
