# ADR-0080: Shared TypeScript workspace and dependency catalog

- **Status:** Accepted
- **Date:** 2026-10-24
- **Partly supersedes:** ADR-0029 for separate lockfiles, per-project installation policy and audit scope; its supply-chain controls remain in force.

## Context

The frontend and load tools are independent pnpm projects with duplicate tool versions, lockfiles and installation policy. Both now use Zod, so security updates can leave consumers at different versions or require repeated policy edits. The owner requested one monorepo installation and shared dependency version management while preserving each package's responsibility.

## Options

Keep separate installations and synchronize duplicated declarations manually; move every dependency to the root and let packages rely on undeclared imports; or use native pnpm workspaces with explicit member dependencies, one lockfile and a shared version catalog. The second option obscures ownership. Additional build orchestration is unnecessary for the existing Make gates.

## Decision

Use one private root pnpm package and a workspace containing exactly web and tests/load. Pin pnpm once through the root packageManager field. Keep each member's scripts, source layout, dependency declarations and execution directory; do not introduce imports between frontend and load sources. Move the lockfile to the root and use frozen root installation for Make, CI and Docker.

Define exact versions for common direct dependencies in the default workspace catalog: Zod, TypeScript, Oxlint and Node types. Members reference the catalog for packages they actually use; unique dependencies retain their exact member pins. A shared catalog coordinates versions rather than declaring dependencies on behalf of consumers. Do not introduce a new task runner, hoisting policy or runtime shared state.

Centralize the existing seven-day age delay, narrowly enumerated pnpm age exceptions, no trust downgrade, blocked exotic transitive sources, strict reviewed build-script decisions, store integrity and exact saving at the root. Preserve the existing security overrides and explicit prohibited native installation scripts. No policy bypass or dependency promotion is part of the migration. Run one root audit over every lockfile importer with the previous web audit's full default severity, retaining production, development and optional dependency coverage.

Treat the root manifest, workspace/catalog, lockfile and both member manifests as installation inputs in Docker and CI. Keep the real frontend build and generated-client workflow working with the shared dependency layout. Retain existing Make entry points as aliases where useful and make workspace contract qualification part of the required gates.

## Consequences

One installation now prepares both TypeScript packages. Security policy and common version changes have one authoritative location, while package ownership stays explicit. A manifest or catalog change requires a coherent root lockfile update; a missing or mismatched lockfile must fail frozen installation. The migration does not alter product APIs, protobuf contracts, frontend layering, Go module management or release authority.

Qualify actual fresh installs and public package imports, existing member commands, Docker packaging and generation. Refuse manifest/catalog drift, absent lockfiles and unreviewed installation scripts with the expected errors, unchanged lock bytes and no script output. Use working fixture baselines so offline cache misses cannot masquerade as policy refusals. Record observed red, full gates, reviews and running-application QA in milestone evidence.

## Sources (verified 2026-10-24)

- [pnpm 10 workspace configuration](https://pnpm.io/10.x/pnpm-workspace_yaml).
- [pnpm 10 dependency catalogs](https://pnpm.io/10.x/catalogs).
- [pnpm 10 frozen and workspace installation](https://pnpm.io/10.x/cli/install).
- [pnpm 10 installation settings](https://pnpm.io/10.x/settings).
- [pnpm 10 dependency audit](https://pnpm.io/10.x/cli/audit).
