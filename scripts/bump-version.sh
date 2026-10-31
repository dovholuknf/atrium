#!/usr/bin/env bash
# Move internal/cli/VERSION, the base number every untagged build reports.
#
#   scripts/bump-version.sh                the next patch: 0.0.3 becomes 0.0.4
#   scripts/bump-version.sh 0.1.0          exactly that
#   scripts/bump-version.sh --push BRANCH  after a release: move BRANCH on origin to the next patch of this checkout's
#                                          number, if BRANCH still holds this number. The release workflow calls this
#
# Without --push it writes the file, prints the new number and commits nothing.

set -euo pipefail
file="internal/cli/VERSION"
cd "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

next_patch() { echo "${1%.*}.$((${1##*.} + 1))"; }
cur="$(tr -d '[:space:]' <"$file")"

if [ "${1:-}" = "--push" ]; then
  branch="${2:?--push needs a branch}"
  next="$(next_patch "$cur")"
  git fetch -q origin "$branch"
  git checkout -q --detach FETCH_HEAD
  if [ "$(tr -d '[:space:]' <"$file")" != "$cur" ]; then
    echo "origin/$branch holds $(tr -d '[:space:]' <"$file"), not $cur. nothing to bump"
    exit 0
  fi
  printf '%s\n' "$next" >"$file"
  git -c user.name="atrium release" -c user.email="release@invalid" commit -q -m "Move VERSION to $next" -- "$file"
  git push -q origin "HEAD:refs/heads/$branch"
  echo "$next"
  exit 0
fi

next="${1:-$(next_patch "$cur")}"
if ! printf '%s' "$next" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "'$next' is not MAJOR.MINOR.PATCH" >&2
  exit 2
fi
printf '%s\n' "$next" >"$file"
echo "$next"
