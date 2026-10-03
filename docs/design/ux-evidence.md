# UX and developer-experience evidence

Reviewed 2026-10-03. These sources inform hypotheses and acceptance tasks; they do not establish measured Portcullis usability improvements. Product requirements and authorization remain authoritative. Pixel colors, card spacing and sticky positioning are project design judgments, not findings proven by these papers.

## Evidence and decisions

| Source and method | Relevant finding | Portcullis design inference | Validation |
| --- | --- | --- | --- |
| Seckler et al., CHI 2014, [Designing Usable Web Forms](https://research.google/pubs/designing-usable-web-forms-empirical-evaluation-of-web-form-improvement-guidelines/), DOI 10.1145/2556288.2557265; controlled eye-tracking experiment, 65 participants | Applying a bundle of 20 form guidelines improved completion, submission attempts and satisfaction | Group context and SQL, explain required values before submission, retain drafts/errors, separate saving from submission | Real draft/save/format/undo scenarios; observe completion errors with participants before claiming an improvement |
| Purchase, GD 1997, [Which aesthetic has the greatest effect on human understanding?](https://link.springer.com/chapter/10.1007/3-540-63938-1_67), DOI 10.1007/3-540-63938-1_67; timed/error graph-comprehension experiments | Edge-crossing reduction mattered more than several other layout aesthetics in the tested graphs | Use a small fixed directed stage graph with crossing-free connectors; preserve ordering and label status in words as well as color | Identify current stage, remaining approvals and exception status; no layout movement on periodic refresh |
| Greiler, Storey and Noda, TSE 2022, [An Actionable Framework for Understanding and Improving Developer Experience](https://www.michaelagreiler.com/wp-content/uploads/2021/12/Framework-for-Understanding-and-Improving.pdf); author manuscript, 21 semi-structured developer interviews | DX depends on context; tooling/feedback and incremental improvements recur in the framework | Show progress inline, make review decisions discoverable, preserve page context, provide Table/Text and explicit visible-page copy; keep theme/frame/state code separate | Ask requester, reviewer and operator what is confusing; measure action discovery, feedback delay and successful task completion |

The form study evaluates a combined intervention rather than proving each guideline independently. The graph paper's publisher abstract was accessible; the full chapter was not. The DX study is qualitative, and its uploaded manuscript predates final publication. None measures SQL approval tools specifically. A universal productivity percentage or guaranteed usability gain is not justified.

## Interaction contract

A request title is a keyboard-operable disclosure button. It opens one inline stage graph beneath the row, exposes `aria-expanded`/`aria-controls`, and leaves row actions and direct detail navigation independent. Use only the authorized list summary. Show automatic approval separately, and do not invent timestamps, reviewer actions, a complete audit history or the last active stage for expired/cancelled requests. Unknown outcomes stay uncertain and never imply safe replay.

Request evidence and decision controls form distinct surfaces. A labeled decision panel remains visible on wide screens and stacks on narrow screens. Approval is primary; rejection and return navigation are separate. Permission checks and the effective server state decide which actions exist. The interface does not become a scheduler or DAG editor.

Table/Text use the same current result page, sort and filter without rerunning SQL or fetching another page. Copy includes headers and visible rows only, preserving exact wire values; spreadsheet formula-looking strings are escaped. Plain Text retains the original displayed values. Clipboard denial produces an inline message and leaves CSV available. Clear stale copy feedback on page, sort, filter, request or session changes.

## Evaluation protocol

Use synthetic data and independent requester/reviewer roles. Ask participants to compose and save a request, identify the connection, discover approval/rejection and return navigation, explain a pending/automatic/expired/unknown graph, then sort/filter a result and copy the visible page in both views. Record completion time, wrong-action attempts, navigation detours, state-interpretation errors and task satisfaction. Compare the same tasks against the previous interface; obtain a baseline before setting improvement targets. Do not send SQL, credentials or cell values into analytics to measure UX.

Automated tests establish behavior and regressions, not human ease of use. Gate the presentation on real-browser keyboard disclosure, current-location cues, reflow at 320 CSS pixels, visible actions, copy success/denial, identical view data and existing approval/execution scenarios. Check screenshots and both themes; local SQL/data-table scrolling is allowed under [W3C reflow guidance](https://www.w3.org/WAI/WCAG22/Understanding/reflow.html). Use [visible focus](https://www.w3.org/WAI/WCAG22/Understanding/focus-visible.html) and text status labels without claiming complete WCAG conformance.
