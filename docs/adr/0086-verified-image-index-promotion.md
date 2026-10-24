# ADR-0086: Verified image index promotion

- **Status:** Accepted
- **Date:** 2026-10-25
- **Refines:** ADR-0047, ADR-0049 and ADR-0084 for artifact verification and publication.

## Context

CI builds and executes disposable native images, but the release job builds a separate image when publishing. Equal source does not establish equal artifact identity, particularly when build attestations include changing metadata. M1 requires publication to promote the verified digest while retaining both supported platforms, SBOM/provenance and source admission.

## Decision

Keep source admission and the complete reusable CI, including functional, dependency-security, race and native image gates, ahead of any registry upload. Then build one Linux AMD64/ARM64 OCI candidate index from that admitted checkout with the exact linked release tag and existing SBOM/maximum provenance. Set OCI media types explicitly rather than depend on builder defaults. Upload it by digest without release aliases to the destination repository, and retain its exact repository and SHA-256 index digest as job outputs. Recheck the admitted Git tag before this candidate build.

Native AMD64 and ARM64 jobs authenticate with package-read permission, pull that same candidate by index digest and execute its selected platform. Qualify platform, non-root runtime, configuration refusal and exact linked startup identity without rebuilding. Only their joint success admits promotion. Candidate upload and final promotion receive package-write permission separately; the remaining jobs retain their narrower permissions.

Promotion validates the candidate index and existing full-version reference, then performs the fresh raw-tag-object/source fence immediately before registry writes. Copy exactly one index source from the same repository to the full-version, stable-minor and full-commit aliases. Do not rebuild, merge sources, filter platforms or add annotations during promotion. Resolve every written alias afterwards and require the original candidate digest, thereby retaining the index's runnable and attestation descriptors unchanged. No implicit latest or major-only alias is added.

An existing full-version alias with a different digest refuses promotion; an equal digest permits idempotent repair of the remaining aliases. Registry lookup, authentication and transport failures never mean that a version is absent. A whole-workflow rerun can produce a new attestation/index digest and therefore refuse an already-published version. Retry the original candidate's failed qualification/promotion where supported rather than retargeting that version. Minor and commit aliases remain mutable convenience references; stable and prerelease builds of one commit can differ per ADR-0085.

Use bounded structured OCI manifest responses and verified raw SHA-256 bytes for registry preflight. Confirm absence only from a valid 404 manifest-unknown response; CLI failure text is insufficient. Keep the credential path specific to GHCR: explicit workflow environment inputs, the exact HTTPS GHCR token realm and current-repository pull scope, no redirects or arbitrary credential forwarding. Unauthenticated loopback registries serve local fixtures. Never include token/response bodies in diagnostics.

Registry writes to multiple aliases are not atomic, and the full-version preflight is not a compare-and-swap lock. Keep same-release workflow concurrency, restrict external package writers and refuse known conflicts; postchecks detect failure without claiming an atomic rollback. Git tag rules and the final source fence retain their own limits per ADR-0084.

## Consequences

The registry must contain a candidate before independent native jobs can pull it and before its index can be copied in the same repository. This upload occurs after the original full CI gates but before the additional exact-candidate smoke checks. It may leave an unaliased candidate, including after a refused promotion. Existing package visibility applies: public package candidates can be downloaded by digest; private packages need granted read access. Operators own package access, visibility and retention. The workflow does not delete package versions or claim signed release attestation verification.

Use isolated owned registry/Git fixtures to observe behavioral reds, then qualify at least four success and four refusal cases, exact index/attestation preservation, same-digest replay, conflict refusal, unavailable evidence and source replacement. Execute the actual candidate and complete the repository's verification/security gates, running-application QA and code/architecture/operator-design/staged reviews before committing. Local registry evidence does not prove hosted scheduling, native runners, GHCR permissions or package visibility.

## Sources (verified 2026-10-25)

- [Docker image exporter and digest-only upload](https://docs.docker.com/build/exporters/image-registry/).
- [Docker single-source index copying](https://docs.docker.com/reference/cli/docker/buildx/imagetools/create/).
- [Docker index inspection](https://docs.docker.com/reference/cli/docker/buildx/imagetools/inspect/).
- [Docker attestation storage](https://docs.docker.com/build/metadata/attestations/attestation-storage/).
- [OCI registry lookup and error contracts](https://github.com/opencontainers/distribution-spec/blob/main/spec.md).
- [GHCR permissions and visibility](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry).
