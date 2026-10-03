#!/usr/bin/env bash
set -euo pipefail

# Admit SemVer release tags that map unambiguously to a Docker version tag.
tag=${1-}
numeric='(0|[1-9][0-9]*)'
pattern="^v${numeric}\\.${numeric}\\.${numeric}(-[0-9A-Za-z-]+(\\.[0-9A-Za-z-]+)*)?$"
if [[ ! $tag =~ $pattern ]] || (( ${#tag} - 1 > 128 )); then
  echo 'Expected vMAJOR.MINOR.PATCH[-PRERELEASE], without build metadata, at most 128 characters after v' >&2
  exit 1
fi
if [[ $tag == *-* ]]; then
  IFS='.' read -r -a identifiers <<< "${tag#*-}"
  for identifier in "${identifiers[@]}"; do
    if [[ $identifier =~ ^[0-9]+$ && $identifier == 0?* ]]; then
      echo 'Numeric prerelease identifiers must not have leading zeroes' >&2
      exit 1
    fi
  done
fi
