#!/usr/bin/env bash
# Make a release of atrium: a version commit on main, then a tag on it. The make-release workflow only calls this.
#
#   scripts/make-release.sh prepare 0.0.6             # prints the changelog it would write, changes nothing
#   scripts/make-release.sh prepare 0.0.6 --execute   # one commit on the current branch, prints its sha
#   scripts/make-release.sh tag 0.0.6 [--execute]     # tags main's head vX and pushes the tag
#
# The commit reaches main by a normal merge. tag refuses unless main's head has that VERSION, the tag is new and
# ci.yml concluded success on that exact sha: it never waits. prepare runs anywhere, tag needs GH_TOKEN (gh) and origin.
# Tags are never moved.
set -euo pipefail
cd "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

phase="${1:?phase: prepare or tag}"
ver="${2:?version MAJOR.MINOR.PATCH}"
ver="${ver#v}"
shift 2
exec_=0
[ "${1:-}" = "--execute" ] && exec_=1

printf '%s' "$ver" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || { echo "'$ver' is not MAJOR.MINOR.PATCH" >&2; exit 2; }
tag="v$ver"
repo="${GITHUB_REPOSITORY:-dovholuknf/atrium}"

last_tag() { git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || true; }

# The changelog: one line per first-parent commit since the last tag, merge noise removed. A draft to edit by hand.
changelog() {
  local last="$1" range=""
  [ -n "$last" ] && range="$last..HEAD"
  echo "# atrium $tag"
  echo
  echo "## Changes"
  echo
  git log --first-parent --no-merges --format='- %s' $range
  # A merged PR's commit has its title as the first line of the body: "- <title> (#N)".
  git log --first-parent --merges --format='%s%x1f%b%x1e' $range | awk -v RS='\036' -F'\037' '
    match($1, /pull request #[0-9]+/) {
      n = substr($1, RSTART + 14, RLENGTH - 14); split($2, b, "\n"); t = b[1]
      if (t == "") t = $1
      print "- " t " (#" n ")"
    }'
}

case "$phase" in
  prepare)
    if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then echo "tag $tag already exists" >&2; exit 1; fi
    last="$(last_tag)"
    if [ "$exec_" = 0 ]; then
      echo "last tag: ${last:-none}; would write internal/cli/VERSION=$ver and changelog/$tag.md:"
      changelog "$last"
      exit 0
    fi
    [ -z "$(git status --porcelain)" ] || { echo "working tree is dirty" >&2; exit 1; }
    changelog "$last" >"changelog/$tag.md"
    bash scripts/bump-version.sh "$ver" >/dev/null
    git add internal/cli/VERSION "changelog/$tag.md"
    git -c user.name="atrium release" -c user.email="release@invalid" commit -q -m "Version $ver and its changelog"
    git rev-parse HEAD
    ;;
  tag)
    git fetch -q origin main
    sha="$(git rev-parse origin/main)"
    if git ls-remote --exit-code --tags origin "refs/tags/$tag" >/dev/null 2>&1; then echo "tag $tag already exists" >&2; exit 1; fi
    [ "$(git show "$sha:internal/cli/VERSION" | tr -d '[:space:]')" = "$ver" ] || { echo "VERSION at main's head $sha is not $ver" >&2; exit 1; }
    ci="$(gh run list --repo "$repo" --workflow ci.yml --branch main --commit "$sha" --json status,conclusion --jq '[.[] | select(.status == "completed" and .conclusion == "success")] | length')"
    [ "${ci:-0}" -gt 0 ] || { echo "ci.yml has no successful run on main for $sha: not releasing" >&2; exit 1; }
    if [ "$exec_" = 0 ]; then echo "would tag $sha as $tag and push it"; exit 0; fi
    git -c user.name="atrium release" -c user.email="release@invalid" tag -a "$tag" "$sha" -m "$tag"
    git push -q origin "refs/tags/$tag"
    ;;
  *) echo "unknown phase $phase" >&2; exit 2 ;;
esac
