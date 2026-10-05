# Portcullis documentation

English · [한국어](README.ko.md)

Product requirements are maintained in Korean and English; other documents adopt the same policy as translations are added.

## Documents

| Area | Documents | Language |
|---|---|---|
| Product requirements | [Korean PRD](product/prd.ko.md) · [English PRD](product/prd.en.md) | Korean / English |
| Database support | [Feature matrix and evidence boundaries](product/database-support.md) | English |
| Database research | [Container-backed candidates and engine experiments](product/database-candidates.md) | English |
| Architecture | [Code layers and query flow](ARCHITECTURE.md) | English |
| Container releases | [Tagged GHCR publication](operations/container-releases.md) | English |
| Deployment architecture | [Private-network topology with WARP or Tailscale](operations/recommended-architecture.md) | English |
| Decisions | [ADR index](adr/README.md) | English |
| Development | [Code](conventions/code.md) · [Data](conventions/data.md) · [Security](conventions/security.md) · [Tooling](conventions/tooling.md) · [Frontend](conventions/frontend.md) | English |
| Performance | [Scenarios and sizing](performance/README.md) · [Query measurements](performance/benchmarks/2026-09-30-query-workloads.md) | English |
| Operations | [PG alpha quickstart](operations/pg-alpha-quickstart.md) · [M1 validation](milestones/m1/validation.md) · [Docker browser E2E](operations/browser-e2e.md) · [Encryption key rotation](operations/key-rotation.md) | English |
| UX design | [Research evidence and evaluation protocol](design/ux-evidence.md) · [UI component catalog and reuse](../web/src/shared/ui/README.md) | English |
| Branding | [Logo assets and placement](branding.md) · [Product tour](media/product-tour.md) · [UI screenshots and GIFs](media/README.md) | English |
| Roadmap | [Milestones and product direction](milestones/README.md) | English |
| Community | [Participation paths](../COMMUNITY.md) · [Launch checklist and first-issue drafts](community/launch-checklist.md) | English |
| Contributors | [Contribution guidelines](../CONTRIBUTING.md) · [Development guide](development.md) | English |

## Language policy

- **One contract, two languages.**
  - Use `<name>.ko.md` and `<name>.en.md` for translated pairs in the same subject folder.
  - Root and directory landing pages retain `README.md` as the English entry point and use `README.ko.md` for Korean.
  - Keep each document body in one language, with a language link at the top.
  - Preserve section numbers, requirement scope, states, identifiers, limits, and source links across translations.
  - Update both files in the same change; a translation is a complete document, not a summary.
- **Versions come only from git tags.**
  - Documents carry no revision or status numbers; git history records their changes.
- **Resolve differences explicitly.**
  - The existing Korean PRD is the baseline for this initial translation.
  - If wording differs in meaning, resolve the requirement in Korean and update English before implementation; use an ADR for technical decisions.
- **Translate gradually.**
  - PRDs and the root/documentation landing pages have Korean and English versions; architecture, ADRs, conventions, and performance documents currently remain in English.
  - Add each translated pair to this index and link both versions to each other.
  - Preserve ADR numbers and existing filenames so decision references remain stable.
- **Keep prose easy to maintain.**
  - Keep related sentences in one paragraph and start a new line after the sentence where the topic changes.
  - Prefer ordinary development terms and preserve technical identifiers and evidence versions.
  - Use a short parent bullet for a rule and sub-bullets for its conditions, examples, and exceptions.
  - Prefer section references over line numbers and keep relative links valid when moving documents.

## File layout

```text
docs/
  README.md
  README.ko.md
  product/
    prd.ko.md
    prd.en.md
  ARCHITECTURE.md
  adr/
  conventions/
  milestones/
    README.md
    overview.svg
    m0/scope.md
    m1/scope.md
    m1/progress.md
    m1/validation.md
    m2/ … m7/
  performance/
    README.md
    benchmarks/
```

PRD files are repository documents; execution task lists, review findings and follow-up notes stay local and are never referenced from committed files.

Per [ADR-0054](adr/0054-release-branches-and-documentation-policy.md), accepted decisions change through a superseding ADR; requirements and section numbers stay synchronized across PRD translations.
