# Release procedure

The [container release workflow](../../.github/workflows/container-release.yml) publishes the repository's single embedded-SPA server image to `ghcr.io/aportcullis/portcullis`. It runs on pushed `v*` tags, validates the name and matching release-branch source, and reuses the complete CI from the admitted commit. After those gates, it builds one attested candidate index, checks that exact digest on both native platforms and copies the unchanged index to release aliases.
The final promotion job checks out full Git history and records current-tag git-cliff release notes in its Actions summary before registry login. It does not deploy the application or create a GitHub Release.

## Prepare and publish

Complete the current milestone on `main`, including every scope acceptance criterion, `make verify` and `make supply-chain`.
Review the generated `CHANGELOG.md` from `make changelog`; before the first tag its heading stays `Unreleased`.
Commit verified release preparation, then cut the matching `release/vX.Y` branch from that main commit; the first release branch is `release/v0.1`.
Patch releases for that minor line use the same release branch, while the next milestone develops on owner-reviewed `feat/<feature>` branches from main.
Per [ADR-0054](../adr/0054-release-branches-and-documentation-policy.md), only the owner creates and pushes release tags; agents never tag or push.

Protect release branches and restrict release-tag creation, updates and deletion to the owner; never retarget a published version.
Use `vMAJOR.MINOR.PATCH`, optionally followed by a SemVer prerelease such as `-rc.1`.
Leading zeroes in numeric identifiers, build metadata (`+build`) and tags longer than 128 characters after `v` are rejected by the current syntax check.

When ready to publish, the owner checks out the matching release branch and creates and pushes the intended tag, for example:

```sh
git switch release/v0.1
git push origin release/v0.1
git tag -a v0.1.0 -m 'Portcullis v0.1.0'
git push origin v0.1.0
```

The tag is created on `release/v0.1`, and its push triggers publication; it is not part of local verification. Inspect the Container release Actions run for gate results and the publication job's digest summary.

## Release acceptance

Release acceptance requires matching release-branch admission and publication of the verified image digest.
The tag-derived version belongs in startup logs only, with no version field in Health responses.
Per [M1 scope](../milestones/m1/scope.md), these are release criteria; this procedure does not establish workflow acceptance.

Per [ADR-0084](../adr/0084-release-source-admission.md), source admission refreshes the exact tag and matching `release/vMAJOR.MINOR` branch from origin. The peeled tag commit must equal the remote branch tip, event commit and checked-out HEAD. Verification and publication check out that recorded SHA; the raw tag object is retained too. Missing refs, failed remote evidence and any mismatch refuse admission before registry credentials are used. Push the release branch before its tag so the matching remote ref exists.

Before the candidate build and again immediately before alias promotion, the tag must still exist with the same raw object and peeled commit; deleting, moving or reannotating even onto the same commit refuses that step. The branch may advance during an already-admitted run. A new admission for the older tag after that advancement fails the tip comparison; use the correct current release source instead of retargeting a published tag.

These checks do not prove a tag's creation branch or enforce owner identity. Protect release tags and branches through repository rules as described above. Source identity, startup version and verified-image promotion have separate qualification evidence; source admission alone does not establish the other release criteria.

Per [ADR-0085](../adr/0085-startup-build-identity.md), the release binary records its exact admitted Git tag, including the leading `v`, in one server-startup `INFO` event named `starting` with a `version` field. Ordinary untagged builds report `development`. Existing log-level filtering still applies; invalid logging configuration refuses before this event can be created. Health bodies/headers and the SPA do not expose the build version. Runtime environment changes cannot replace the identity compiled into the binary.

## Image references

| Git tag | Published image tags |
|---|---|
| `v0.1.0` | `0.1.0`, `0.1`, `sha-<full commit SHA>` |
| `v0.2.0-rc.1` | `0.2.0-rc.1`, `sha-<full commit SHA>` |

There is no implicit `latest` or major-only alias. Each tag selects `linux/amd64` (AMD/Intel x86-64) or `linux/arm64` (AArch64) through one image index. Apple Silicon uses the ARM64 Linux variant in Docker; native macOS/Windows executables and other CPU architectures are outside this container scope. A full-version alias with a different existing digest refuses promotion; an equal digest permits replay. Minor and commit aliases are mutable conveniences. Stable and prerelease builds of one commit can have different linked versions and image digests, so the commit alias is not artifact identity.
Pin the returned `ghcr.io/aportcullis/portcullis@sha256:...` digest in production.

The candidate-build and final-promotion jobs receive `packages: write`, authenticating with `GITHUB_TOKEN`; native candidate checks receive `packages: read`. For an existing package, grant the repository Actions access if needed. New GHCR packages start private: a package administrator must explicitly set visibility to public for anonymous OSS pulls. No personal access token is required by this workflow.

BuildKit publishes OCI SBOM and maximum provenance attestations with the image. These are not signed release attestations; do not treat their presence as signature verification. Keep secrets out of Docker build arguments and context. Action SHAs and Docker base image digests remain pinned and tracked by Renovate.

## Candidate visibility and retries

Per [ADR-0086](../adr/0086-verified-image-index-promotion.md), the complete reusable CI, including functional/security, race and native image gates, passes before candidate upload. Upload uses a digest-only image exporter without release aliases. The additional native checks pull and execute that exact index digest, with no build or mutable-tag fallback. Promotion copies a single existing index source and checks every resulting alias against its digest, retaining the runnable images and attestation descriptors.

Candidate upload precedes those exact-candidate checks and uses the package's existing visibility. A public package can therefore expose a candidate by digest before its extra smoke checks pass. A failed check or refused promotion can leave an unaliased package version. Operators manage package access, visibility and retention; do not delete a digest merely because one run failed, since another reference can use the same content. Missing or inaccessible candidates fail the run rather than trigger a replacement build.

Replaying promotion of the same digest can repair missing convenience aliases. Rebuilding the same source in a whole-workflow rerun can change attestations and the index digest; a conflicting published full version then refuses instead of being replaced. Retry the original candidate's failed jobs when Actions retains their inputs, subject to the final source fence. Do not retarget the Git tag or overwrite a published full version to make a new candidate fit.

Full-version lookup distinguishes confirmed manifest absence from authentication, transport and registry failures. It is still a preflight rather than a registry compare-and-swap, and multiple alias writes are not atomic. Keep the same-release concurrency guard and restrict other package writers. Postchecks report partial failure without destructive rollback; a same-digest replay can finish convenience aliases. Different release versions can still move mutable minor aliases out of order, so pin production digests.

## Verification

`make release-check` exercises valid stable/prerelease names and refusal of malformed, unsafe, ambiguous and oversized names, executes the workflows' actual source-input and candidate-digest Bash blocks against valid and unsafe inputs, then uses isolated disposable Git origins/checkouts to qualify branch-tip/source agreement and the final tag fence. Cases include missing/moved/deleted refs, same-commit reannotation and allowed branch advancement during an admitted run. `make verify` includes it.

`make release-image-check` uses an owned loopback registry and Git fixtures to qualify actual index copying, attestation preservation, alias derivation/replay and refusal before conflicting or obsolete publication. Bounded registry-error cases ensure unavailable evidence does not mean an absent version. It requires Node 24, Docker and Buildx; `make verify` includes it. Its synthetic OCI protocol fixtures are separate from actual product-image smoke checks and hosted GHCR acceptance.

`make release-candidate-check` separately builds one actual multi-platform product candidate with SBOM/maximum provenance in an owned loopback registry, executes and promotes that same digest without rebuilding, and checks artifact preservation. It uses an isolated digest-pinned BuildKit container and verifies removal of its owned builder state without changing the default builder. Run it for release-pipeline changes before committing. Local execution of the other architecture needs existing emulation; it does not establish native hosted-runner acceptance.

`make changelog-check` tests real stable/prerelease history, grouping, breaking notes and current-tag boundaries in a disposable Git repository. `make verify` includes it.
The changelog history/writer fixture runs on disposable container tmpfs with a read-only source mount. Its test image extends the pinned generator with Git/Python from signed distribution repositories; scenario execution has networking disabled. Fixture and owned container/image cleanup must pass before success is reported.
The Docker generator is digest-pinned, reads only the mounted repository and runs without network or external template commands.

`make image-check` builds both real images, checks OS/architecture and non-root runtime, then verifies the actual entrypoint rejects deliberately invalid configuration and reports its compiled startup identity. An omitted `BUILD_VERSION` checks the Docker development default; an explicit valid release tag checks linker propagation. Local non-native execution needs existing emulation; set `IMAGE_PLATFORMS=linux/arm64` or `linux/amd64` to select a native variant. CI runs each variant on its native Ubuntu runner and release publication additionally checks the exact candidate digest on both native runners.
These are image/startup smoke checks; the full database/browser gate remains on AMD64.

Workflow validation uses actionlint. A local check does not prove registry permissions, hosted build success or package visibility: confirm these on the first authorized release run.

References: [GHCR permissions and visibility](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry), [Docker metadata](https://github.com/docker/metadata-action#semver), [OCI build attestations](https://docs.docker.com/build/ci/github-actions/attestations/), [ADR-0047](../adr/0047-tagged-container-publication.md).
