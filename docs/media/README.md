# README screenshots and walkthroughs

These assets show the actual embedded SPA and Go server with synthetic data in a disposable PostgreSQL instance. They are product documentation, not a substitute for the release gate. Logo assets and branding character illustrations are separate from these application captures.

## Assets

| File | Contents |
| --- | --- |
| `bootstrap.png` | Branded first-administrator setup screen |
| `login.png` | Branded local sign-in screen |
| `connections.png` | Two demo connections with development/production labels |
| `policy.png` | Default read-only policy and one distinct required reviewer |
| `request.png` | Grouped request context and SQL, inline review guidance, local formatting and typed parameters |
| `requests.png` | Request list with the inline four-stage progress graph |
| `results-text.png` | Tab-separated Text view of one server-paged snapshot |
| `review.png` | A distinct reviewer inspecting submitted SQL |
| `results.png` | Recorded execution time and typed results with exact integers and decimals |
| `workflow.gif` | Request → distinct approval → single-use execution |
| `results.gif` | Page → sort → filter → prepare the whole-snapshot CSV |

The `production` label is illustrative: both targets connect to the disposable database. The reviewer is provisioned directly in that fixture because the user-management UI is not yet implemented and remains M1 work. The CSV sequence checks the generated content includes all 30 synthetic rows, despite filtering down to 10, and stops at the download link. It does not claim native file saving succeeded; see the [M1 validation record](../milestones/m1/validation.md).

## Reproduce

Use Go, pnpm, Python 3 with pip, Docker and project-local Playwright Chromium. Obtain any required path-specific permission before accessing a Docker socket, tool cache or browser installation outside your workspace. Set `DOCKER_HOST` to the permitted socket and `DOCKER_CONFIG` to a project-local directory containing `config.json` with `{"auths":{}}`. The capture owns its container and removes it and its server on completion or error. Ports `15432` and `18080` must be free; do not run E2E concurrently.

From the project root, with frontend dependencies already installed, set `DOCKER_CONFIG`, `DOCKER_HOST`, `PLAYWRIGHT_BROWSERS_PATH` and `PYTHONPATH` to the explicitly permitted test environment.
Install Playwright Chromium and Pillow 12.1.1 there, then run the tracked capture tools:

```sh
pnpm -C web exec playwright install chromium
node docs/media/capture.mjs
python3 docs/media/make_gifs.py
```

`capture.mjs` builds the SPA and real binary, starts a fresh database, bootstraps a demo administrator, provisions a distinct reviewer, then operates the real browser UI.
It checks request transitions, single-use execution, approval button layout, exact results, sorting, filtering and whole-snapshot CSV contents.
Temporary frames stay in the ignored working directory selected by the capture tool; only final PNGs and GIFs belong in Git.
The synthetic password and randomly generated master key are confined to this throwaway installation.

The GIFs are deliberately paced step walkthroughs rather than continuous video. The only added visual element is a caption strip outside the unchanged application screenshot. Each captioned frame is encoded with a 128-color palette and a 1.6–2.8 second reading pause. Full-page captures keep approval actions visible; GIF frames share a canvas sized to the tallest captured page. Keep a static result screenshot and descriptive alt text in the README so understanding the feature does not depend on animation.

Capture API behavior follows the [Playwright screenshot documentation](https://playwright.dev/docs/api/class-page#page-screenshot); GIF encoding follows [Pillow's GIF documentation](https://pillow.readthedocs.io/en/stable/handbook/image-file-formats.html#gif). Inspect every final screenshot and the GIF sequence before committing refreshed media. Use only synthetic accounts, SQL and results; do not capture actual credentials or operational data.
