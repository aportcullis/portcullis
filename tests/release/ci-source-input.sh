#!/usr/bin/env bash
set -euo pipefail

project_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
fixture_root=$(mktemp -d "${TMPDIR:-$project_root}/portcullis-ci-source-input.XXXXXX")

# Remove only this invocation's temporary runner files.
cleanup_ci_source_fixture() {
  find "$fixture_root" -depth -delete
}

trap cleanup_ci_source_fixture EXIT
trap 'exit 130' INT TERM

# Execute the actual named step; refuse missing, duplicated or empty payloads.
if ! awk '
  /^      - name: Validate source SHA$/ {
    named_steps++
    selected_step = 1
    next
  }
  selected_step && /^        run: \|$/ {
    run_blocks++
    reading_block = 1
    next
  }
  reading_block {
    if ($0 ~ /^          /) {
      sub(/^          /, "")
      print
      if ($0 ~ /[^[:space:]]/) payload_lines++
      next
    }
    if ($0 ~ /^[[:space:]]*$/) {
      print ""
      next
    }
    reading_block = 0
    selected_step = 0
  }
  selected_step && $0 !~ /^        / && $0 !~ /^[[:space:]]*$/ {
    selected_step = 0
  }
  END {
    if (named_steps != 1 || run_blocks != 1 || payload_lines == 0) exit 1
  }
' "$project_root/.github/workflows/ci.yml" > "$fixture_root/step.sh"; then
  echo 'Expected exactly one nonempty Validate source SHA Bash block' >&2
  exit 1
fi

checks=0
failures=0

# Match the explicit Bash runner's failure handling and isolated output file.
run_ci_source_step() {
  local source_sha=$1
  : > "$fixture_root/output"
  if SOURCE_SHA="$source_sha" GITHUB_OUTPUT="$fixture_root/output" \
    bash --noprofile --norc -e -o pipefail "$fixture_root/step.sh" \
    > "$fixture_root/stdout" 2> "$fixture_root/stderr"; then
    step_status=0
  else
    step_status=$?
  fi
}

# Accepted values preserve their exact identity, including the empty default.
expect_ci_source_acceptance() {
  local description=$1 source_sha=$2
  run_ci_source_step "$source_sha"
  printf 'source_sha=%s\n' "$source_sha" > "$fixture_root/expected"
  checks=$((checks + 1))
  if [[ $step_status != 0 || -s $fixture_root/stdout || -s $fixture_root/stderr ]] || \
    ! cmp -s "$fixture_root/expected" "$fixture_root/output"; then
    echo "Valid CI verification source rejected or changed: $description" >&2
    failures=$((failures + 1))
  fi
}

# Refusal cannot expose partial job outputs or succeed with a mutable ref.
expect_ci_source_refusal() {
  local description=$1 source_sha=$2
  run_ci_source_step "$source_sha"
  checks=$((checks + 1))
  if [[ $step_status == 0 || -s $fixture_root/output || -s $fixture_root/stdout || ! -s $fixture_root/stderr ]]; then
    echo "Invalid CI verification source accepted or partially emitted: $description" >&2
    failures=$((failures + 1))
  fi
}

full_sha=0123456789abcdef0123456789abcdef01234567
long_sha=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
expect_ci_source_acceptance event-default ''
expect_ci_source_acceptance full-sha1 "$full_sha"
expect_ci_source_acceptance full-sha256 "$long_sha"
expect_ci_source_acceptance mixed-case-sha1 0123456789aBcDeF0123456789AbCdEf01234567

expect_ci_source_refusal branch-name main
expect_ci_source_refusal tag-reference refs/tags/v1.2.3
expect_ci_source_refusal shortened-sha "${full_sha%?}"
expect_ci_source_refusal oversized-sha "${long_sha}0"
expect_ci_source_refusal surrounding-whitespace " $full_sha "
expect_ci_source_refusal nonhex-sha "${full_sha%?}g"
expect_ci_source_refusal output-injection "$full_sha"$'\nsource_sha=main'
expect_ci_source_refusal newline-suffix "$full_sha"$'\n'

if [[ $failures != 0 ]]; then
  printf '%s of %s CI verification source scenarios failed\n' "$failures" "$checks" >&2
  exit 1
fi
cleanup_ci_source_fixture
[[ ! -e $fixture_root ]]
trap - EXIT
printf 'CI verification source scenarios and fixture cleanup passed (%s checks)\n' "$checks"
