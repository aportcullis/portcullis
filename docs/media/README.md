# README screenshots and walkthroughs

These assets show the actual embedded SPA and Go server with synthetic data in a disposable PostgreSQL instance. They are product documentation, not a substitute for the release gate. Logo assets and branding character illustrations are separate from these application captures.

## Assets

| File | Contents |
| --- | --- |
| `connections.png` | Two demo connections with development/production labels |
| `policy.png` | Default read-only policy and one distinct required reviewer |
| `review.png` | A distinct reviewer inspecting submitted SQL |
| `results.png` | Typed results with exact large integers and decimal amounts |
| `workflow.gif` | Request → distinct approval → single-use execution |
| `results.gif` | Page → sort → filter → prepare the whole-snapshot CSV |

The `production` label is illustrative: both targets connect to the disposable database. The reviewer is provisioned directly in that fixture because the user-management UI is a later milestone. The CSV sequence checks the generated content includes all 30 synthetic rows, despite filtering down to 10, and stops at the download link. It does not claim native file saving succeeded; see the [M1 validation record](../operations/m1-validation.md).

## Reproduce

Use Go, pnpm, Python 3 with pip, Docker and project-local Playwright Chromium. Obtain any required path-specific permission before accessing a Docker socket, tool cache or browser installation outside your workspace. Set `DOCKER_HOST` to the permitted socket and `DOCKER_CONFIG` to a project-local directory containing `config.json` with `{"auths":{}}`. The capture owns its container and removes it and its server on completion or error. Ports `15432` and `18080` must be free; do not run E2E concurrently.

From the project root, with frontend dependencies already installed:

```sh
export DOCKER_CONFIG="$PWD/.test-docker"
export PLAYWRIGHT_BROWSERS_PATH="$PWD/.test-docker/browsers"
pnpm -C web exec playwright install chromium
python3 -m pip --isolated install --no-cache-dir --target .test-docker/media-python Pillow==12.1.1
node docs/media/capture.mjs
PYTHONPATH="$PWD/.test-docker/media-python" python3 docs/media/make_gifs.py
```

`capture.mjs` builds the SPA and real binary, starts a fresh database, bootstraps a demo administrator, provisions a distinct reviewer, then operates the real browser UI. It asserts the request state transitions, single-use execution, approval button layout, exact result values, sorting, filtering and whole-snapshot CSV contents. It writes temporary frames into ignored `.test-docker/readme-media/`; only final PNGs and GIFs belong in Git. The synthetic password and randomly generated master key are confined to this throwaway installation.

The GIFs are deliberately paced step walkthroughs rather than continuous video. The only added visual element is a caption strip outside the unchanged application screenshot. Each captioned frame is encoded with a 128-color palette and a 1.6–2.8 second reading pause. Keep a static result screenshot and descriptive alt text in the README so understanding the feature does not depend on animation.

Capture API behavior follows the [Playwright screenshot documentation](https://playwright.dev/docs/api/class-page#page-screenshot); GIF encoding follows [Pillow's GIF documentation](https://pillow.readthedocs.io/en/stable/handbook/image-file-formats.html#gif). Inspect every final screenshot and the GIF sequence before committing refreshed media. Use only synthetic accounts, SQL and results; do not capture actual credentials or operational data.
