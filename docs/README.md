# Portcullis documentation

제품 요구사항은 한국어와 영어로 함께 관리하고, 나머지 문서는 번역을 추가할 때 같은 규칙을 적용한다.
Product requirements are maintained in Korean and English; other documents adopt the same policy as translations are added.

## Documents

| Area | Documents | Language |
|---|---|---|
| Product requirements | [한국어 PRD](product/prd.ko.md) · [English PRD](product/prd.en.md) | Korean / English |
| Architecture | [Layers and boundaries](ARCHITECTURE.md) | English |
| Decisions | [ADR index](adr/README.md) | English |
| Development | [Code](conventions/code.md) · [Data](conventions/data.md) · [Security](conventions/security.md) · [Tooling](conventions/tooling.md) · [Frontend](conventions/frontend.md) | English |
| Performance | [Scenarios and sizing](performance/README.md) · [Query measurements](performance/benchmarks/2026-09-30-query-workloads.md) | English |
| Operations | [PG alpha quickstart](operations/pg-alpha-quickstart.md) · [M1 validation](operations/m1-validation.md) · [Encryption key rotation](operations/key-rotation.md) | English |
| Branding | [Logo assets and placement](branding.md) · [UI screenshots and GIFs](media/README.md) | English |
| Contributors | [Development and roadmap](development.md) | English |

## Language policy

- **One contract, two languages.**
  - Use `<name>.ko.md` and `<name>.en.md` for translated pairs in the same subject folder.
  - Preserve section numbers, requirement scope, states, identifiers, limits, and source links across translations.
  - Update both files and their shared revision in the same change; a translation is a complete document, not a summary.
- **Resolve differences explicitly.**
  - The existing Korean PRD is the baseline for this initial translation.
  - If wording differs in meaning, resolve the requirement in Korean and update English before implementation; use an ADR for technical decisions.
- **Translate gradually.**
  - PRD is the first pair; architecture, ADRs, conventions, and performance documents currently remain in English.
  - Add each translated pair to this index and link both versions to each other.
  - Preserve ADR numbers and existing filenames so decision references remain stable.
- **Keep prose easy to maintain.**
  - Group prose into paragraphs by topic and keep comments concise; do not automatically break after every sentence.
  - Use a short parent bullet for a rule and sub-bullets for its conditions, examples, and exceptions.
  - Prefer section references over line numbers and keep relative links valid when moving documents.

## File layout

```text
docs/
  README.md
  product/
    prd.ko.md
    prd.en.md
  ARCHITECTURE.md
  adr/
  conventions/
  performance/
    README.md
    benchmarks/
```

PRD files are repository documents; private working notes such as the root `todo.md` remain gitignored.
