#!/usr/bin/env bash
# What scripts/cut-release.sh REFUSES, asserted rather than believed.
#
#   scripts/check-release.sh
#
# The valuable half of a release script is the half that says no, and that half
# is only ever exercised on the day it matters, which is the day you least want
# to find out it was wrong. So the refusals are tested here, against a throwaway
# git repository that holds nothing but the script itself.
#
# THE TEST ASSERTS THE EXIT CODE, not just a failure. `cut-release.sh` gives
# each refusal its own number for exactly this reason: a test that only checks
# for non-zero passes when the script refuses for the wrong reason, which is how
# a dirty-tree check quietly becomes a syntax error nobody noticed.
#
# `--preflight` is what makes this cheap. It runs the refusals and stops before
# anything is built, so the whole file takes about a second and needs neither a
# Go toolchain nor the network.

set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fail=0
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT

mkdir -p "$fixture/scripts"
cp "$here/scripts/cut-release.sh" "$fixture/scripts/cut-release.sh"

cd "$fixture"
git init -q -b main .
git config user.email "test@example.invalid"
git config user.name "release test"
echo "a repository with one file in it" >README
git add -A
git commit -qm "the commit under test"

# Every case runs the script and compares the code it exited with to the code
# the refusal is documented as. The output is kept and only shown when a case
# fails, because a passing suite that prints forty lines per case is a suite
# nobody reads the end of.
expect() {
  local want="$1" label="$2"
  shift 2
  local output got
  output="$(bash scripts/cut-release.sh "$@" 2>&1)"
  got=$?
  if [ "$got" = "$want" ]; then
    echo "ok: $label (exit $got)"
  else
    echo "FAILED: $label -- wanted exit $want, got $got" >&2
    printf '%s\n' "$output" | sed 's/^/    /' >&2
    fail=1
  fi
}

echo "=== the version string"
expect 0 "a tag-shaped version is accepted" v0.1.0 --preflight
expect 0 "a prerelease suffix is accepted" v0.1.0-rc1 --preflight
expect 2 "a version with no v is refused" 0.1.0 --preflight
expect 2 "a two-part version is refused" v1.2 --preflight
expect 2 "a branch name is not a version" main --preflight
expect 2 "an unknown option is refused" v0.1.0 --preflight --publish-now

echo
echo "=== a dirty working tree"
echo "not committed" >"scratch.txt"
expect 3 "an untracked file is dirty" v0.1.0 --preflight
rm -f scratch.txt
echo "edited" >>README
expect 3 "a modified tracked file is dirty" v0.1.0 --preflight
git checkout -q -- README
expect 0 "and clean again once it is put back" v0.1.0 --preflight

echo
echo "=== a tag that already exists"
git tag -a v0.1.0 -m "atrium v0.1.0"
expect 4 "a release will not move a tag somebody may have installed from" v0.1.0 --preflight
expect 0 "the next version is still fine" v0.1.1 --preflight
expect 0 "--from-tag accepts the tag it is told already exists" v0.1.0 --from-tag --preflight
expect 4 "--from-tag refuses a tag that is not there" v0.9.9 --from-tag --preflight

echo
echo "=== --from-tag against a tag that is not HEAD"
echo "later work" >>README
git commit -qam "a commit after the tag"
expect 4 "building HEAD while claiming to build the tag is refused" v0.1.0 --from-tag --preflight

echo
echo "=== inferring the version"
git tag -a v0.2.0 -m "atrium v0.2.0"
expect 0 "the tag on HEAD is used when no version is given" --preflight
git tag -d v0.2.0 >/dev/null
expect 2 "with no tag on HEAD and no argument, it asks rather than guesses" --preflight

echo
echo "=== a dry run leaves nothing behind"
# The point of the default being a dry run is that it can be run without
# deciding anything. A dry run that created the tag would be a decision.
before="$(git tag -l | tr '\n' ' ')"
bash scripts/cut-release.sh v0.3.0 --preflight >/dev/null 2>&1
after="$(git tag -l | tr '\n' ' ')"
if [ "$before" = "$after" ]; then
  echo "ok: no tag was created ($before)"
else
  echo "FAILED: the tag list changed from '$before' to '$after'" >&2
  fail=1
fi

echo
if [ "$fail" != "0" ]; then
  echo "the release refusals are not what they say they are. see above." >&2
  exit 1
fi
echo "every refusal did what it says it does."
