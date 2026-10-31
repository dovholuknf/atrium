#!/usr/bin/env bash
# What this checkout calls itself. The one place the rule lives, so no build path computes its own.
#
#   scripts/version.sh            the version: the tag on HEAD without its v, else <base>+<shorthash>
#   scripts/version.sh commit     the full commit, or empty
#   scripts/version.sh tree       clean or modified, over tracked files only
#
# <base> is internal/cli/VERSION. ATRIUM_COMMIT stands in for git, for a build in an export with no .git.

set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

commit="${ATRIUM_COMMIT:-$(git -C "$here" rev-parse HEAD 2>/dev/null || true)}"

case "${1:-version}" in
  commit) printf '%s\n' "$commit" ;;
  tree)
    if [ -n "$(git -C "$here" status --porcelain --untracked-files=no 2>/dev/null)" ]; then echo modified; else echo clean; fi
    ;;
  version)
    tag="$(git -C "$here" describe --tags --exact-match 2>/dev/null || true)"
    if [ -n "$tag" ]; then
      printf '%s\n' "${tag#v}"
    else
      base="$(tr -d '[:space:]' <"$here/internal/cli/VERSION")"
      if [ -n "$commit" ]; then printf '%s+%s\n' "$base" "${commit:0:7}"; else printf '%s\n' "$base"; fi
    fi
    ;;
  *)
    echo "usage: version.sh [version|commit|tree]" >&2
    exit 2
    ;;
esac
