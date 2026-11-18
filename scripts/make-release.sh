#!/usr/bin/env bash
# Make a release of atrium from main: bump, changelog, PR, wait, tag. The make-release workflow only calls this.
#
#   scripts/make-release.sh prepare 0.0.6             # prints the changelog it would write, changes nothing
#   scripts/make-release.sh prepare 0.0.6 --execute   # branch release/v0.0.6 with one commit, prints its sha
#   scripts/make-release.sh open-pr 0.0.6 --execute   # pushes the branch, opens the PR, starts ci.yml on it, auto-merge
#   scripts/make-release.sh wait 0.0.6                # waits for that PR to merge, prints the merge sha
#   scripts/make-release.sh tag 0.0.6 <sha> --execute # tags the merge sha vX and pushes the tag
#
# prepare and tag run anywhere; open-pr, wait and tag --execute need GH_TOKEN (gh). Tags are never moved.
set -euo pipefail
cd "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

phase="${1:?phase: prepare, open-pr, wait or tag}"
ver="${2:?version MAJOR.MINOR.PATCH}"
ver="${ver#v}"
shift 2
sha=""
[ "$phase" = tag ] && { sha="${1:?tag needs the merge sha}"; shift; }
exec_=0
[ "${1:-}" = "--execute" ] && exec_=1

printf '%s' "$ver" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || { echo "'$ver' is not MAJOR.MINOR.PATCH" >&2; exit 2; }
tag="v$ver"
branch="release/$tag"
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
    git switch -q -c "$branch"
    changelog "$last" >"changelog/$tag.md"
    bash scripts/bump-version.sh "$ver" >/dev/null
    git add internal/cli/VERSION "changelog/$tag.md"
    git -c user.name="atrium release" -c user.email="release@invalid" commit -q -m "Version $ver and its changelog"
    git rev-parse HEAD
    ;;
  open-pr)
    [ "$exec_" = 1 ] || { echo "open-pr needs --execute"; exit 0; }
    git push -q origin "$branch"
    gh pr create --repo "$repo" --base main --head "$branch" --title "Release $tag" \
      --body "Version $ver and its changelog. Made by the make-release workflow." || true
    # A PR opened by GITHUB_TOKEN does not start ci.yml: start it by hand on the head, so ci-ok reports on this sha.
    gh workflow run ci.yml --repo "$repo" --ref "$branch"
    gh pr merge "$branch" --repo "$repo" --auto --squash
    ;;
  wait)
    # Up to 3 hours, one look a minute, as a CI step and never a session.
    for _ in $(seq 1 180); do
      state="$(gh pr view "$branch" --repo "$repo" --json state --jq .state)"
      case "$state" in
        MERGED) gh pr view "$branch" --repo "$repo" --json mergeCommit --jq .mergeCommit.oid; exit 0 ;;
        CLOSED) echo "PR for $branch was closed without merging" >&2; exit 1 ;;
      esac
      sleep 60
    done
    echo "PR for $branch did not merge in 3 hours" >&2
    exit 1
    ;;
  tag)
    if git ls-remote --exit-code --tags origin "refs/tags/$tag" >/dev/null 2>&1; then echo "tag $tag already exists" >&2; exit 1; fi
    git fetch -q origin main "$sha" 2>/dev/null || git fetch -q origin main
    git merge-base --is-ancestor "$sha" origin/main || { echo "$sha is not on main" >&2; exit 1; }
    [ "$(git show "$sha:internal/cli/VERSION" | tr -d '[:space:]')" = "$ver" ] || { echo "VERSION at $sha is not $ver" >&2; exit 1; }
    if [ "$exec_" = 0 ]; then echo "would tag $sha as $tag and push it"; exit 0; fi
    git -c user.name="atrium release" -c user.email="release@invalid" tag -a "$tag" "$sha" -m "$tag"
    git push -q origin "refs/tags/$tag"
    ;;
  *) echo "unknown phase $phase" >&2; exit 2 ;;
esac
