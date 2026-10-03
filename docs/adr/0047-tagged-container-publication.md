# ADR-0047: Publish tagged container releases to GHCR

- **Status:** Accepted
- **Date:** 2026-10-04

## Context

The repository builds one Go binary with its embedded SPA, but existing workflows do not publish images. A release must verify the tagged source rather than rely on an earlier branch check, and package write access must be confined to publication.

## Decision

Trigger on pushed `v*` tags and reject names other than `vMAJOR.MINOR.PATCH[-PRERELEASE]` with SemVer numeric-identifier rules and a Docker tag length limit. Build metadata is excluded to avoid aliases collapsing distinct releases. Reuse CI from the same tagged commit; publish only after its functional and supply-chain gates pass. Build the root Dockerfile as one multi-platform image index (`linux/amd64` and `linux/arm64`, ADR-0049) at `ghcr.io/${github.repository}`, using SHA-pinned Node 24 actions and the job's GITHUB_TOKEN with contents read/packages write. Other jobs have contents read only.

Publish full versions, stable major.minor aliases and full commit SHA tags. Disable implicit latest; prereleases must not promote stable aliases. Serialize runs of the same git ref without cancelling publication. Include OCI SBOM and maximum BuildKit provenance attestations. These do not establish cryptographic signing. No deployment or GitHub Release is created.

## Consequences

The workflow must be present at the tagged commit. Repository operators control release tags and package visibility; initial GHCR packages are private. Existing package access must permit this repository's token. Production users should pin image digests; version/minor/SHA tags can be overwritten on reruns or retagging, and concurrent different-version releases can move minor aliases backwards. Architecture packaging qualification follows ADR-0049; signed provenance is separate work. Hosted build/publication remains to be observed after an authorized tag push.

## Sources

- [GitHub reusable workflows](https://docs.github.com/en/actions/how-tos/reuse-automations/reuse-workflows): same-repository workflow references use the caller commit.
- [GHCR authentication and visibility](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry).
- [Docker metadata SemVer tags](https://github.com/docker/metadata-action#semver): prerelease versions do not promote minor aliases.
- [Docker SBOM and provenance attestations](https://docs.docker.com/build/ci/github-actions/attestations/).
