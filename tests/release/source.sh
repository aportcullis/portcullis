#!/usr/bin/env bash
set -euo pipefail

# Exercise real remote references without consulting host Git configuration or hooks.
project_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
fixture_root=$(mktemp -d "${TMPDIR:-$project_root}/portcullis-release-source.XXXXXX")

# Delete only this invocation's owned fixture without following symbolic links.
cleanup_source_fixture() {
  rm -rf "$fixture_root"
}

trap cleanup_source_fixture EXIT
trap 'exit 130' INT TERM
mkdir "$fixture_root/hooks" "$fixture_root/templates"
: > "$fixture_root/gitconfig"
: > "$fixture_root/attributes"
: > "$fixture_root/excludes"
unset GIT_DIR GIT_WORK_TREE GIT_COMMON_DIR GIT_INDEX_FILE GIT_OBJECT_DIRECTORY
unset GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_CONFIG GIT_CONFIG_PARAMETERS GIT_CONFIG_COUNT
unset GIT_NAMESPACE GIT_CEILING_DIRECTORIES
export GIT_CONFIG_NOSYSTEM=1 GIT_ATTR_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0
export GIT_CONFIG_GLOBAL="$fixture_root/gitconfig" GIT_TEMPLATE_DIR="$fixture_root/templates"
export GIT_AUTHOR_NAME='Release source fixture' GIT_AUTHOR_EMAIL='fixture@example.invalid'
export GIT_COMMITTER_NAME="$GIT_AUTHOR_NAME" GIT_COMMITTER_EMAIL="$GIT_AUTHOR_EMAIL"
git config --file "$GIT_CONFIG_GLOBAL" core.hooksPath "$fixture_root/hooks"
git config --file "$GIT_CONFIG_GLOBAL" core.attributesFile "$fixture_root/attributes"
git config --file "$GIT_CONFIG_GLOBAL" core.excludesFile "$fixture_root/excludes"
git config --file "$GIT_CONFIG_GLOBAL" commit.gpgsign false
git config --file "$GIT_CONFIG_GLOBAL" tag.gpgsign false
git config --file "$GIT_CONFIG_GLOBAL" init.defaultBranch main
validator="$project_root/.github/scripts/validate-release-source.sh"
verifier="$project_root/.github/scripts/verify-release-source.sh"
failures=0
checks=0

# Start each scenario from an independently working bare origin and detached checkout.
prepare_source_fixture() {
  scenario_root="$fixture_root/$1"
  fixture_tag=$2
  fixture_branch=$4
  fixture_origin="$scenario_root/origin.git"
  fixture_writer="$scenario_root/writer"
  fixture_work="$scenario_root/work"
  mkdir "$scenario_root"
  git init --quiet --bare "$fixture_origin"
  git init --quiet "$fixture_writer"
  git -C "$fixture_writer" commit --quiet --allow-empty -m 'feat: prepare release fixture'
  fixture_commit=$(git -C "$fixture_writer" rev-parse HEAD)
  git -C "$fixture_writer" branch "$fixture_branch"
  if [[ $3 == annotated ]]; then
    git -C "$fixture_writer" tag -a "$fixture_tag" -m 'Initial release annotation'
  else
    git -C "$fixture_writer" tag "$fixture_tag"
  fi
  fixture_tag_oid=$(git -C "$fixture_writer" rev-parse "refs/tags/$fixture_tag")
  git -C "$fixture_writer" remote add origin "$fixture_origin"
  git -C "$fixture_writer" push --quiet origin refs/heads/main "refs/heads/$fixture_branch" "refs/tags/$fixture_tag"
  git clone --quiet --no-checkout "$fixture_origin" "$fixture_work"
  git -C "$fixture_work" checkout --quiet --detach "$fixture_commit"
}

# Record observable output and prove helpers preserve checkout contents and identity.
run_source_helper() {
  local before_head before_status after_head after_status
  before_head=$(git -C "$fixture_work" rev-parse HEAD)
  before_status=$(git -C "$fixture_work" status --porcelain)
  if (cd "$fixture_work" && bash "$@") > "$scenario_root/stdout" 2> "$scenario_root/stderr"; then
    helper_status=0
  else
    helper_status=$?
  fi
  after_head=$(git -C "$fixture_work" rev-parse HEAD)
  after_status=$(git -C "$fixture_work" status --porcelain)
  if [[ $before_head != "$after_head" || $before_status != "$after_status" ]]; then
    echo 'Release helper changed the checkout' >&2
    exit 1
  fi
}

# Require exact structured admission data before changing any valid fixture.
expect_source_admission() {
  run_source_helper "$validator" "$fixture_tag" "$fixture_commit"
  printf 'source_sha=%s\ntag_oid=%s\nrelease_branch=%s\n' "$fixture_commit" "$fixture_tag_oid" "$fixture_branch" > "$scenario_root/expected"
  checks=$((checks + 1))
  if [[ $helper_status != 0 ]] || ! cmp -s "$scenario_root/expected" "$scenario_root/stdout"; then
    echo "Valid source admission failed: ${scenario_root##*/}" >&2
    cat "$scenario_root/stderr" >&2
    exit 1
  fi
}

# Check a successful final fence without treating diagnostics as machine output.
expect_final_source_fence() {
  run_source_helper "$verifier" "$fixture_tag" "$fixture_commit" "$fixture_tag_oid"
  checks=$((checks + 1))
  if [[ $helper_status != 0 || -s $scenario_root/stdout ]]; then
    echo "Valid final source fence failed: ${scenario_root##*/}" >&2
    cat "$scenario_root/stderr" >&2
    failures=$((failures + 1))
  fi
}

# Refusal must be an actual failed command with no partial success outputs.
expect_source_refusal() {
  local description=$1
  shift
  run_source_helper "$@"
  checks=$((checks + 1))
  if [[ $helper_status == 0 || -s $scenario_root/stdout || ! -s $scenario_root/stderr ]]; then
    echo "Unsafe release source accepted or partially emitted: $description" >&2
    failures=$((failures + 1))
  fi
}

for tag_style in lightweight annotated; do
  for tag_kind in stable prerelease; do
    if [[ $tag_kind == stable ]]; then
      tag=v1.2.3
    else
      tag=v1.2.4-rc.1
    fi
    prepare_source_fixture "$tag_style-$tag_kind" "$tag" "$tag_style" release/v1.2
    expect_source_admission
    expect_final_source_fence
  done
done

prepare_source_fixture advancing-branch v12.34.56-rc.1 annotated release/v12.34
expect_source_admission
git -C "$fixture_writer" commit --quiet --allow-empty -m 'feat: advance release branch'
git -C "$fixture_writer" branch --force "$fixture_branch" HEAD
git -C "$fixture_writer" push --quiet origin "refs/heads/$fixture_branch"
expect_final_source_fence
expect_source_refusal behind-tip "$validator" "$fixture_tag" "$fixture_commit"

for missing_branch in missing wrong-minor; do
  prepare_source_fixture "$missing_branch-branch" v1.2.3 annotated release/v1.2
  expect_source_admission
  if [[ $missing_branch == wrong-minor ]]; then
    git -C "$fixture_writer" push --quiet origin "$fixture_commit:refs/heads/release/v1.3"
  fi
  git -C "$fixture_writer" push --quiet origin ":refs/heads/$fixture_branch"
  expect_source_refusal "$missing_branch matching branch" "$validator" "$fixture_tag" "$fixture_commit"
done

prepare_source_fixture mismatched-event v1.2.3 lightweight release/v1.2
expect_source_admission
git -C "$fixture_writer" commit --quiet --allow-empty -m 'feat: unrelated event'
other_commit=$(git -C "$fixture_writer" rev-parse HEAD)
expect_source_refusal event-commit-mismatch "$validator" "$fixture_tag" "$other_commit"

prepare_source_fixture mismatched-checkout v1.2.3 annotated release/v1.2
expect_source_admission
git -C "$fixture_work" commit --quiet --allow-empty -m 'feat: unverified checkout'
expect_source_refusal checkout-mismatch "$validator" "$fixture_tag" "$fixture_commit"
expect_source_refusal final-checkout-mismatch "$verifier" "$fixture_tag" "$fixture_commit" "$fixture_tag_oid"

prepare_source_fixture noncommit-tag v1.2.3 lightweight release/v1.2
expect_source_admission
tree_oid=$(git -C "$fixture_writer" rev-parse 'HEAD^{tree}')
git -C "$fixture_writer" tag --force "$fixture_tag" "$tree_oid" >/dev/null
git -C "$fixture_writer" push --quiet --force origin "refs/tags/$fixture_tag"
expect_source_refusal noncommit-tag "$validator" "$fixture_tag" "$fixture_commit"
expect_source_refusal final-noncommit-tag "$verifier" "$fixture_tag" "$fixture_commit" "$fixture_tag_oid"

prepare_source_fixture deleted-tag v1.2.3 annotated release/v1.2
expect_source_admission
expect_final_source_fence
git -C "$fixture_writer" push --quiet origin ":refs/tags/$fixture_tag"
git -C "$fixture_work" rev-parse --verify "refs/tags/$fixture_tag" >/dev/null
expect_source_refusal deleted-tag-with-cached-ref "$validator" "$fixture_tag" "$fixture_commit"
expect_source_refusal final-deleted-tag-with-cached-ref "$verifier" "$fixture_tag" "$fixture_commit" "$fixture_tag_oid"

prepare_source_fixture moved-tag v1.2.3 lightweight release/v1.2
expect_source_admission
git -C "$fixture_writer" commit --quiet --allow-empty -m 'feat: replace tag target'
git -C "$fixture_writer" tag --force "$fixture_tag" HEAD >/dev/null
git -C "$fixture_writer" push --quiet --force origin "refs/tags/$fixture_tag"
expect_source_refusal moved-tag "$validator" "$fixture_tag" "$fixture_commit"
expect_source_refusal final-moved-tag "$verifier" "$fixture_tag" "$fixture_commit" "$fixture_tag_oid"

prepare_source_fixture reannotated-tag v1.2.3 annotated release/v1.2
expect_source_admission
git -C "$fixture_writer" tag --force -a "$fixture_tag" "$fixture_commit" -m 'Replacement annotation for the same commit' >/dev/null
replacement_tag_oid=$(git -C "$fixture_writer" rev-parse "refs/tags/$fixture_tag")
[[ $replacement_tag_oid != "$fixture_tag_oid" ]]
[[ $(git -C "$fixture_writer" rev-parse "refs/tags/$fixture_tag^{commit}") == "$fixture_commit" ]]
git -C "$fixture_writer" push --quiet --force origin "refs/tags/$fixture_tag"
expect_source_refusal reannotation-of-same-commit "$verifier" "$fixture_tag" "$fixture_commit" "$fixture_tag_oid"

prepare_source_fixture unreachable-origin v1.2.3 annotated release/v1.2
expect_source_admission
git -C "$fixture_work" remote set-url origin "$scenario_root/absent-origin.git"
expect_source_refusal unreachable-origin "$validator" "$fixture_tag" "$fixture_commit"
expect_source_refusal final-unreachable-origin "$verifier" "$fixture_tag" "$fixture_commit" "$fixture_tag_oid"

prepare_source_fixture malformed-input v1.2.3 annotated release/v1.2
expect_source_admission
expect_source_refusal malformed-tag "$validator" 'v1.2.3;exit 0' "$fixture_commit"
expect_source_refusal abbreviated-event "$validator" "$fixture_tag" "${fixture_commit:0:12}"
expect_source_refusal extra-admission-argument "$validator" "$fixture_tag" "$fixture_commit" ignored
expect_source_refusal abbreviated-tag-object "$verifier" "$fixture_tag" "$fixture_commit" "${fixture_tag_oid:0:12}"
expect_source_refusal extra-fence-argument "$verifier" "$fixture_tag" "$fixture_commit" "$fixture_tag_oid" ignored

if [[ $failures != 0 ]]; then
  echo "Release source scenarios failed: $failures of $checks checks" >&2
  exit 1
fi
cleanup_source_fixture
[[ ! -e $fixture_root ]]
trap - EXIT
echo "Release source admission, immutable tag fences and fixture cleanup passed: $checks checks"
