# Community launch checklist

[Participation model](../../COMMUNITY.md) · [Contributor guide](../../CONTRIBUTING.md)

The participation model is adopted locally. This checklist separates repository preparation from live GitHub configuration. On 2026-10-03, the current GitHub CLI credentials could not resolve `aportcullis/portcullis`; no remote settings, categories, labels, issues, or posts were created.

## Remote setup

- [ ] Confirm the intended repository and maintainer access without changing its visibility implicitly.
- [ ] Enable private vulnerability reporting: repository Settings → Advanced Security → Private vulnerability reporting → Enable. GitHub requires a public repository and owner/admin access. After the separately authorized public transition, confirm the Security tab offers Report a vulnerability. Do not submit a fake vulnerability report.
- [ ] Enable Discussions and create Welcome (announcement), Q&A (question/answer), Ideas (open discussion), and Show and tell (open discussion).
- [ ] Publish and pin a Welcome post linking the quickstart, participation paths, and contributor guide. Invite introductions and workflow feedback.
- [ ] Create or verify `help wanted` and `good first issue` labels.
- [ ] Assign a reviewer and confirm scope for each starter draft below before publishing and labeling it.
- [ ] Publish a formal Code of Conduct with a reporting route that its responsible maintainers can operate.
- [ ] After verifying the channels, replace the temporary Issues fallback in README, COMMUNITY, and CONTRIBUTING with live Discussions links.

Publishing a post or issue is a separate external communication step. The local policy and drafts do not establish that it has happened.

## Starter issue drafts

| Draft | Scope | Proposed label after readiness review |
| --- | --- | --- |
| [Make the first-query instructions match the alpha](first-issues/01-first-query-guide.md) | Fix the missing required title and outdated SQLite roadmap statement | `help wanted`, `good first issue` |
| [Explain reversible SQL formatting](first-issues/02-sql-formatting-guide.md) | Add a short synthetic example and the save/submit boundary | `help wanted`, `good first issue` |
| [Explain result sorting, CSV, and expiry](first-issues/03-result-exploration-guide.md) | Add a small user walkthrough for the existing result controls | `help wanted`, `good first issue` |

These are proposed documentation tasks grounded in existing behavior. No issue number, owner assignment, or label is implied. Each draft provides references and completion criteria; a named reviewer is the remaining prerequisite for publication as a first-contribution issue.

## Ongoing maintenance

Triage questions, reproduction reports, and PRs regularly. Keep accepted tasks and beginner labels current. Explain review findings and check failures, then help first-time contributors find a related next task. Review the community model when activity warrants another channel or recurring meetings; do not promise response deadlines that maintainers cannot sustain.

## Platform references

- [GitHub discussion categories](https://docs.github.com/en/discussions/managing-discussions-for-your-community/managing-categories-for-discussions)
- [Kubernetes first-issue criteria](https://www.kubernetes.dev/docs/guide/help-wanted/)
- [GitHub private vulnerability reporting configuration](https://docs.github.com/en/code-security/how-tos/report-and-fix-vulnerabilities/configure-vulnerability-reporting/configure-for-a-repository)
