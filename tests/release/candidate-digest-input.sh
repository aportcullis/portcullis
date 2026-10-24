#!/usr/bin/env bash
set -euo pipefail

project_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
fixture_root=$(mktemp -d "${TMPDIR:-$project_root}/portcullis-candidate-digest-input.XXXXXX")

# Remove only the temporary files created by this invocation.
cleanup_candidate_digest_fixture() {
  find "$fixture_root" -depth -delete
}

trap cleanup_candidate_digest_fixture EXIT
trap 'exit 130' INT TERM

# Extract the actual workflow payload rather than duplicating its validation.
if ! awk '
  /^      - name: Validate candidate digest$/ {
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
' "$project_root/.github/workflows/container-release.yml" > "$fixture_root/step.sh"; then
  echo 'Expected exactly one nonempty Validate candidate digest Bash block' >&2
  exit 1
fi

checks=0
failures=0

# Run the extracted Bash step with an isolated workflow-output file.
run_candidate_digest_step() {
  local candidate_digest=$1
  : > "$fixture_root/output"
  if CANDIDATE_DIGEST="$candidate_digest" GITHUB_OUTPUT="$fixture_root/output" \
    bash --noprofile --norc -e -o pipefail "$fixture_root/step.sh" \
    > "$fixture_root/stdout" 2> "$fixture_root/stderr"; then
    step_status=0
  else
    step_status=$?
  fi
}

# Accepted digests retain every byte in the single fixed output field.
expect_candidate_digest_acceptance() {
  local description=$1 candidate_digest=$2
  run_candidate_digest_step "$candidate_digest"
  printf 'digest=%s\n' "$candidate_digest" > "$fixture_root/expected"
  checks=$((checks + 1))
  if [[ $step_status != 0 || -s $fixture_root/stdout || -s $fixture_root/stderr ]] || \
    ! cmp -s "$fixture_root/expected" "$fixture_root/output"; then
    echo "Valid candidate digest rejected or changed: $description" >&2
    failures=$((failures + 1))
  fi
}

# Rejected values cannot expose a partial or injected workflow output.
expect_candidate_digest_refusal() {
  local description=$1 candidate_digest=$2
  run_candidate_digest_step "$candidate_digest"
  checks=$((checks + 1))
  if [[ $step_status == 0 || -s $fixture_root/output || -s $fixture_root/stdout || ! -s $fixture_root/stderr ]]; then
    echo "Unsafe candidate digest accepted or partially emitted: $description" >&2
    failures=$((failures + 1))
  fi
}

digest_hex=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
candidate_digest="sha256:$digest_hex"
expect_candidate_digest_acceptance canonical-digest "$candidate_digest"
expect_candidate_digest_acceptance zero-prefixed-digest "sha256:0000000000000000000000000000000000000000000000000000000000000001"
expect_candidate_digest_acceptance all-numeric-digest "sha256:1234567890123456789012345678901234567890123456789012345678901234"
expect_candidate_digest_acceptance all-letter-digest "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

expect_candidate_digest_refusal missing-digest ''
expect_candidate_digest_refusal mutable-tag latest
expect_candidate_digest_refusal repository-reference "ghcr.io/example/portcullis@$candidate_digest"
expect_candidate_digest_refusal wrong-algorithm "sha512:$digest_hex"
expect_candidate_digest_refusal upper-case-digest "sha256:ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd"
expect_candidate_digest_refusal abbreviated-digest "sha256:${digest_hex%?}"
expect_candidate_digest_refusal oversized-digest "${candidate_digest}0"
expect_candidate_digest_refusal nonhex-digest "sha256:${digest_hex%?}g"
expect_candidate_digest_refusal surrounding-whitespace " $candidate_digest "
expect_candidate_digest_refusal output-injection "$candidate_digest"$'\ndigest=latest'
expect_candidate_digest_refusal newline-suffix "$candidate_digest"$'\n'

if [[ $failures != 0 ]]; then
  printf '%s of %s candidate digest scenarios failed\n' "$failures" "$checks" >&2
  exit 1
fi
cleanup_candidate_digest_fixture
[[ ! -e $fixture_root ]]
trap - EXIT
printf 'Candidate digest scenarios and fixture cleanup passed (%s checks)\n' "$checks"
