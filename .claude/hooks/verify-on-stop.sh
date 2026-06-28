#!/usr/bin/env bash
# Stop hook: refuse to finish a turn while the Go code doesn't build or vet
# cleanly, so "done" can't be declared on broken code. Fast checks only (no
# Docker/tests) so it can run at every turn end; the full `make verify` gate
# (lint + tests) lives in the pre-push git hook and CI.
set -uo pipefail

# Loop guard: if we already blocked once this turn, let the stop through to
# avoid an infinite stop loop (CI remains the hard gate). See stop_hook_active
# in the Claude Code hooks reference.
input=$(cat)
if printf '%s' "$input" | grep -q '"stop_hook_active"[[:space:]]*:[[:space:]]*true'; then
  exit 0
fi

cd "${CLAUDE_PROJECT_DIR:-.}" || exit 0

if ! go build ./... || ! go vet ./...; then
  echo "Stop blocked: \`go build ./...\` and \`go vet ./...\` must pass before finishing. Fix the errors above, then run \`make verify\` for the full gate." >&2
  exit 2
fi
exit 0
