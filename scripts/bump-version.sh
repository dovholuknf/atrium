#!/usr/bin/env bash
# Move internal/cli/VERSION, the base number every untagged build reports.
#
#   scripts/bump-version.sh           the next patch: 0.0.3 becomes 0.0.4
#   scripts/bump-version.sh 0.1.0     exactly that
#
# Writes the file and prints the new number. It commits nothing, so the release workflow that calls it decides how the
# change lands.

set -euo pipefail
file="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/internal/cli/VERSION"

next="${1:-}"
if [ -z "$next" ]; then
  cur="$(tr -d '[:space:]' <"$file")"
  next="${cur%.*}.$((${cur##*.} + 1))"
fi
if ! printf '%s' "$next" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "'$next' is not MAJOR.MINOR.PATCH" >&2
  exit 2
fi
printf '%s\n' "$next" >"$file"
echo "$next"
