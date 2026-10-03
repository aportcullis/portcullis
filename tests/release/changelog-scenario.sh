#!/usr/bin/env bash
set -euo pipefail

# Verify real Git-history and writer behavior entirely in container tmpfs.
[[ $PWD == /app ]] || { echo "Scenario requires container tmpfs workdir" >&2; exit 1; }
mkdir -p .test-docker
fixture=$(mktemp -d "$PWD/.test-docker/changelog.XXXXXX")
trap 'rm -rf "$fixture"' EXIT
cp /source/cliff.toml "$fixture/cliff.toml"
mkdir -p "$fixture/.github/scripts"
cp /source/.github/scripts/update-changelog.sh /source/.github/scripts/validate-release-tag.sh "$fixture/.github/scripts/"
# Use the installed real generator instead of nesting Docker inside the fixture.
cat > "$fixture/.github/scripts/git-cliff.sh" <<'GENERATOR'
#!/usr/bin/env bash
exec git-cliff --config cliff.toml --offline --no-exec "$@"
GENERATOR
git init -q "$fixture"
export GIT_AUTHOR_NAME='Changelog fixture' GIT_AUTHOR_EMAIL='fixture@example.invalid'
export GIT_COMMITTER_NAME="$GIT_AUTHOR_NAME" GIT_COMMITTER_EMAIL="$GIT_AUTHOR_EMAIL"
commit_fixture() {
  git -C "$fixture" -c core.hooksPath="$fixture/.git/hooks" commit -q --allow-empty "$@"
}
commit_fixture -m 'feat(requests): submit reviewed SQL'
commit_fixture -m 'fix(security): refuse unsafe SQL'
git -C "$fixture" tag v0.1.0
commit_fixture -m 'feat(results)!: replace wire format' -m 'BREAKING CHANGE: clients must regenerate bindings'
commit_fixture -m 'docs: explain private deployment'
commit_fixture -m 'Legacy history entry'
commit_fixture -m 'chore(changelog): refresh generated notes'
commit_fixture -m 'chore(changelog)!: migrate note format' -m 'BREAKING CHANGE: update note consumers'
(cd "$fixture" && bash .github/scripts/git-cliff.sh) > "$fixture/all.md"
(cd "$fixture" && bash .github/scripts/git-cliff.sh --unreleased --strip all) > "$fixture/unreleased.md"
(cd "$fixture" && RELEASE_TAG=v0.2.0-rc.1 bash .github/scripts/update-changelog.sh)
cp "$fixture/CHANGELOG.md" "$fixture/preview.md"
# A refused release name and generator failure must preserve existing notes.
if (cd "$fixture" && RELEASE_TAG=vbanana bash .github/scripts/update-changelog.sh) >/dev/null 2>&1; then
  echo "Invalid release preview accepted" >&2
  exit 1
fi
cmp "$fixture/preview.md" "$fixture/CHANGELOG.md"
cp "$fixture/cliff.toml" "$fixture/config.backup"
printf 'not valid TOML [\n' > "$fixture/cliff.toml"
if (cd "$fixture" && bash .github/scripts/update-changelog.sh) >/dev/null 2>&1; then
  echo "Invalid generator configuration accepted" >&2
  exit 1
fi
cmp "$fixture/preview.md" "$fixture/CHANGELOG.md"
if compgen -G "$fixture/CHANGELOG.md.*" >/dev/null; then
  echo 'Atomic writer left temporary files after failure' >&2
  exit 1
fi
mv "$fixture/config.backup" "$fixture/cliff.toml"
git -C "$fixture" tag v0.2.0-rc.1
(cd "$fixture" && bash .github/scripts/git-cliff.sh --current --strip all) > "$fixture/current.md"
python3 - "$fixture" <<'PYTEST'
from pathlib import Path
import sys
root = Path(sys.argv[1])
all_notes = (root / 'all.md').read_text()
unreleased = (root / 'unreleased.md').read_text()
current = (root / 'current.md').read_text()
for expected in ['## Unreleased', 'v0.1.0', '### Features', '### Security', 'submit reviewed SQL', 'refuse unsafe SQL', '**BREAKING**', 'clients must regenerate bindings', 'update note consumers', 'Legacy history entry', 'https://github.com/aportcullis/portcullis/commit/']:
    assert expected in all_notes, expected
assert 'refresh generated notes' not in all_notes
for notes in [unreleased, current]:
    assert 'replace wire format' in notes
    assert 'submit reviewed SQL' not in notes
    assert 'refuse unsafe SQL' not in notes
assert 'v0.2.0-rc.1' in (root / 'preview.md').read_text()
assert (root / 'CHANGELOG.md').stat().st_mode & 0o777 == 0o644
assert 'v0.2.0-rc.1' in current
assert 'Unreleased' not in current
PYTEST
rm -rf "$fixture"
[[ ! -e $fixture ]]
trap - EXIT
echo 'Changelog history, breaking-change, release-boundary and tmpfs cleanup scenarios passed'
