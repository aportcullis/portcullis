#!/usr/bin/env bash
set -euo pipefail

mode="${1-}"
if [[ "$mode" != test && "$mode" != race ]]; then
  echo 'Expected test or race followed by Go package patterns.' >&2
  exit 2
fi
shift
if [[ "$#" -eq 0 ]]; then
  echo 'Expected at least one Go package pattern.' >&2
  exit 2
fi

trap 'exit 130' INT
trap 'exit 143' TERM

list_flags=(-f '{{.ImportPath}}')
test_flags=(-p 1)
if [[ "$mode" == race ]]; then
  list_flags+=(-race)
  test_flags+=(-race)
fi

package_paths="$(go list "${list_flags[@]}" -- "$@")"
if [[ -z "$package_paths" ]]; then
  echo 'Package selection contains no Go packages.' >&2
  exit 1
fi

exit_status=0
while IFS= read -r package_path; do
  if go test "${test_flags[@]}" "$package_path" -count=1 -shuffle=on; then
    continue
  else
    command_status="$?"
    case "$command_status" in
      130|143) exit "$command_status" ;;
      *) exit_status=1 ;;
    esac
  fi
done <<< "$package_paths"

exit "$exit_status"
