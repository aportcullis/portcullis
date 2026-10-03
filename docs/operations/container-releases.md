# Container releases

The [container release workflow](../../.github/workflows/container-release.yml) publishes the repository's single embedded-SPA server image to `ghcr.io/aportcullis/portcullis`. It runs on pushed `v*` tags, validates the name, and reuses CI from the tagged commit. Publication requires all `make verify` and `make supply-chain` gates to pass. The publication job checks out full Git history and records current-tag git-cliff release notes in its Actions summary before registry login. It does not deploy the application or create a GitHub Release.

## Prepare and publish

Run `make changelog RELEASE_TAG=v0.1.0` for the intended version, review and commit the generated `CHANGELOG.md`, then merge release changes before tagging. `RELEASE_TAG` labels the preview without creating a Git tag; omit it for an Unreleased snapshot. Generation failure preserves the existing file. git-cliff reads existing Conventional Commits; incompatible changes should include `!` and a `BREAKING CHANGE:` migration explanation. Set repository rules to restrict release-tag creation, updates and deletion to release maintainers; do not retarget released versions. Use `vMAJOR.MINOR.PATCH`, optionally followed by a SemVer prerelease such as `-rc.1`. Leading zeroes in numeric identifiers, build metadata (`+build`) and version tags longer than 128 characters after `v` are rejected.

When ready to publish, a maintainer creates and pushes the intended release tag, for example:

```sh
git tag -a v0.1.0 -m 'Portcullis v0.1.0'
git push origin v0.1.0
```

This example triggers publication; it is not part of local verification. Inspect the Container release Actions run for gate results and the publication job's digest summary.

## Image references

| Git tag | Published image tags |
|---|---|
| `v0.1.0` | `0.1.0`, `0.1`, `sha-<full commit SHA>` |
| `v0.2.0-rc.1` | `0.2.0-rc.1`, `sha-<full commit SHA>` |

There is no implicit `latest` or major-only alias. Each tag selects `linux/amd64` (AMD/Intel x86-64) or `linux/arm64` (AArch64) through one image index. Apple Silicon uses the ARM64 Linux variant in Docker; native macOS/Windows executables and other CPU architectures are outside this container scope. Minor aliases are mutable; reruns and retagging can overwrite other tags too. Different-version releases can complete out of order and move a minor alias backwards. Pin the returned `ghcr.io/aportcullis/portcullis@sha256:...` digest in production.

The publication job alone receives `packages: write`, authenticating with `GITHUB_TOKEN`. For an existing package, grant the repository Actions access if needed. New GHCR packages start private: a package administrator must explicitly set visibility to public for anonymous OSS pulls. No personal access token is required by this workflow.

BuildKit publishes OCI SBOM and maximum provenance attestations with the image. These are not signed release attestations; do not treat their presence as signature verification. Keep secrets out of Docker build arguments and context. Action SHAs and Docker base image digests remain pinned and tracked by Renovate.

## Verification

`make release-check` exercises valid stable/prerelease tags and refusal of malformed, unsafe, ambiguous and oversized names; `make verify` includes it. `make changelog-check` tests real stable/prerelease history, grouping, breaking notes and current-tag boundaries in a disposable Git repository. `make verify` includes both scenario targets. The Docker generator is digest-pinned, reads only the mounted repository and runs without network or external template commands.

`make image-check` builds both real images, checks OS/architecture and non-root runtime, then verifies the actual entrypoint rejects deliberately invalid configuration. Local non-native execution needs existing emulation; set `IMAGE_PLATFORMS=linux/arm64` or `linux/amd64` to select a native variant. CI runs each variant on its native Ubuntu runner and release publication requires both jobs. These are image/startup smoke checks; the full database/browser gate remains on AMD64.

Workflow validation uses actionlint. A local check does not prove registry permissions, hosted build success or package visibility: confirm these on the first authorized release run.

References: [GHCR permissions and visibility](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry), [Docker metadata](https://github.com/docker/metadata-action#semver), [OCI build attestations](https://docs.docker.com/build/ci/github-actions/attestations/), [ADR-0047](../adr/0047-tagged-container-publication.md).
