#!/usr/bin/env bash
# Cut a release of atrium. One command, and no decisions left inside it.
#
#   scripts/cut-release.sh v0.1.0             # says everything it would do, changes nothing
#   scripts/cut-release.sh v0.1.0 --execute   # tags, pushes, publishes
#
# THE DRY RUN IS THE DEFAULT, and that is the whole design of this file. The
# three scripts underneath it already worked. What did not exist was a single
# thing to run that answers, without anybody thinking about it at the time:
# what version is this, does the binary agree, which artefacts, what are their
# hashes, where does the manifest go, and what would make this release wrong.
#
# WHAT IT REFUSES, and why each one is here rather than in a checklist:
#
#   A DIRTY WORKING TREE.       A release script that will happily publish
#                               uncommitted work is the one that eventually
#                               does, and the artefact that results cannot be
#                               rebuilt by anybody, including its author.
#   A TAG THAT ALREADY EXISTS.  A tag is the only name a release has. Moving one
#                               that somebody has already installed from is the
#                               packaging failure with no remedy.
#   A BINARY THAT LIES.         The version is stamped by the linker and is
#                               `dev` when it is not. A release that reports
#                               itself as `dev` is one no package manager will
#                               ever offer an upgrade over, and the only moment
#                               it is cheap to notice is before publishing.
#   A BUILD THAT IS NOT         The commit being tagged is exported to a clean
#   REPRODUCIBLE FROM THE       directory and built again there. If the two
#   COMMIT.                     binaries differ, something outside the commit
#                               reached the compiler, so the tag would name a
#                               source tree that does not produce the artefact.
#   A CHECKSUM THAT DOES NOT    Delegated to publish-release.sh, which re-hashes
#   MATCH.                      every asset against the file that claims to know.
#
# None of the first four can be waived by a flag. `--skip-ci` is the one thing
# that can, because CI is a gate that also runs elsewhere and the other four
# are facts about the artefact in front of you.
#
# WHAT IT WILL NEVER DO: create an account, sign anything, or edit the scoop
# bucket. Those need credentials that belong to a person. It writes the exact
# manifest the bucket wants and tells you where to put it, which is as far as
# a script can go on somebody's behalf.
#
# ALL THE LOGIC IS HERE. `.github/workflows/release.yml` checks out the code and
# calls this, passing the tag as an argument and the token in the environment.
# That is the convention every script in this directory follows, and the reason
# is that a release path which only exists inside a YAML file can only ever be
# debugged by cutting a release.
#
# EXIT CODES, so a caller and a test can tell the refusals apart:
#
#   2  bad usage, or a version string that is not a tag shape
#   3  the working tree is dirty
#   4  the tag already exists, or under --from-tag does not exist or is not HEAD
#   5  the built binary does not agree with the version being released
#   6  the build is not reproducible from the commit being tagged
#   7  the checksums do not match the artefacts
#   8  scripts/ci.sh failed
#   1  anything else

set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$here"

execute=no
from_tag=no
preflight_only=no
skip_ci=no
notes_file=""
version=""

usage() {
  cat <<'EOF'
usage: scripts/cut-release.sh <version> [options]

  <version>       the tag, with its v. v0.1.0, v1.2.3-rc1.

  --execute       do the things that change something: create the tag, push it,
                  and publish. Without it nothing outside build.claude/ is
                  touched and nothing leaves this machine.
  --dry-run       the default. Accepted so a script can say which one it means.
  --from-tag      the tag already exists and points at HEAD. Do not create it,
                  do not push it, just build and publish it. This is how the
                  workflow runs, because pushing the tag is what started it.
  --preflight     run only the refusals, print the verdict, and stop. Cheap
                  enough to run before you have decided anything.
  --skip-ci       do not run scripts/ci.sh. The only waivable check.
  --notes=FILE    release notes. Without it, GitHub generates them.
EOF
}

for arg in "$@"; do
  case "$arg" in
    --execute) execute=yes ;;
    --dry-run) execute=no ;;
    --from-tag) from_tag=yes ;;
    --preflight) preflight_only=yes ;;
    --skip-ci) skip_ci=yes ;;
    --notes=*) notes_file="${arg#--notes=}" ;;
    -h | --help)
      usage
      exit 0
      ;;
    -*)
      echo "unknown option: $arg" >&2
      usage >&2
      exit 2
      ;;
    *)
      if [ -n "$version" ]; then
        echo "two versions given: $version and $arg" >&2
        exit 2
      fi
      version="$arg"
      ;;
  esac
done

step() {
  echo
  echo "=== $*"
}
refuse() {
  local code="$1"
  shift
  echo
  echo "REFUSED: $*" >&2
  exit "$code"
}

# ==============================================================================
# 1. PREFLIGHT: what version, and is this tree allowed to produce one
# ==============================================================================

step "preflight"

if [ -z "$version" ]; then
  # An exact tag on HEAD is the one case where the version can be inferred
  # without guessing. `git describe` without --exact-match would answer
  # something like v0.1.0-3-gabc1234, which is a description of a commit and not
  # a version anybody meant to release.
  version="$(git describe --tags --exact-match 2>/dev/null || true)"
  if [ -z "$version" ]; then
    echo "no version given and HEAD carries no tag." >&2
    usage >&2
    exit 2
  fi
  echo "  version    $version (inferred from the tag on HEAD)"
  from_tag=yes
else
  echo "  version    $version"
fi

# The shape is checked rather than trusted, because every consumer downstream
# parses it: package-linux.sh strips the v for dpkg, the release URL contains
# it whole, and the scoop manifest carries both forms. A version that is nearly
# right produces a release that is wrong in one of the three.
if ! printf '%s' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$'; then
  refuse 2 "'$version' is not a version. want vMAJOR.MINOR.PATCH, optionally -something."
fi

if ! git rev-parse --git-dir >/dev/null 2>&1; then
  refuse 1 "this is not a git repository, so there is no commit to release."
fi
commit="$(git rev-parse HEAD)"
echo "  commit     $commit"

# THE DIRTY TREE CHECK, and it counts untracked files.
#
# Tracked-only would pass a tree with a file the build reads and the commit does
# not carry, which is the same lie in a form that is harder to see.
dirty="$(git status --porcelain)"
if [ -n "$dirty" ]; then
  echo
  echo "$dirty" >&2
  refuse 3 "the working tree is not clean. commit or remove the above, then run this again."
fi
echo "  tree       clean"

tag_exists=no
git rev-parse --verify --quiet "refs/tags/$version" >/dev/null && tag_exists=yes
if [ "$from_tag" = "yes" ]; then
  [ "$tag_exists" = "yes" ] ||
    refuse 4 "--from-tag was passed and $version does not exist here."
  tagged="$(git rev-list -n 1 "$version")"
  [ "$tagged" = "$commit" ] ||
    refuse 4 "$version points at $tagged and HEAD is $commit. build what the tag names, not what is checked out."
  echo "  tag        $version exists and is HEAD"
else
  [ "$tag_exists" = "no" ] ||
    refuse 4 "$version already exists here. a tag is the only name a release has, so this one will not move it."
  echo "  tag        $version does not exist yet and would be created"
fi

# The remote is asked only when something is actually going to be pushed, and a
# failure to reach it is reported rather than fatal. A dry run must work on a
# train.
if [ "$execute" = "yes" ] && [ "$from_tag" = "no" ]; then
  if remote_tags="$(git ls-remote --tags origin "refs/tags/$version" 2>/dev/null)"; then
    [ -z "$remote_tags" ] ||
      refuse 4 "$version already exists on origin. somebody may have installed from it."
    echo "  origin     does not have $version"
  else
    echo "  origin     could not be reached, so nothing is known about the tag there"
  fi
fi

if [ -n "$notes_file" ] && [ ! -f "$notes_file" ]; then
  refuse 2 "no notes file at $notes_file"
fi

if [ "$execute" = "yes" ] && ! command -v gh >/dev/null 2>&1; then
  refuse 1 "gh is not on PATH, and it is the only way anything here talks to GitHub."
fi

echo
if [ "$execute" = "yes" ]; then
  echo "  mode       EXECUTE. the tag, the push and the release are real."
else
  echo "  mode       dry run. nothing outside build.claude/ will be touched."
fi

if [ "$preflight_only" = "yes" ]; then
  echo
  echo "preflight passed. nothing was built."
  exit 0
fi

if ! command -v go >/dev/null 2>&1; then
  refuse 1 "go is not on PATH, so there is nothing to build."
fi

out="build.claude/release/$version"

# ==============================================================================
# 2. CI: the same gate the workflow runs, run here first
# ==============================================================================

if [ "$skip_ci" = "yes" ]; then
  step "ci: skipped by --skip-ci"
else
  step "ci"
  bash scripts/ci.sh || refuse 8 "ci did not pass. a release is not the place to find that out."
fi

# ==============================================================================
# 3. BUILD: five platforms, then the Linux packages
# ==============================================================================

step "build"
bash scripts/release.sh "$version"

step "the deb and the rpm"
# nfpm is a Go program package-linux.sh will fetch, which needs the network. A
# machine without it can still prove everything else, so this is reported and
# not fatal in a dry run. Under --execute it is fatal, because a release missing
# two of its six artefacts is not the release the manifest describes.
if ! bash scripts/package-linux.sh "$version"; then
  if [ "$execute" = "yes" ]; then
    refuse 1 "the linux packages did not build, and a partial release is worse than none."
  fi
  echo
  echo "  the linux packages did not build. under --execute this would stop here."
fi

# ==============================================================================
# 4. IDENTITY: does the binary agree with the release it is in
# ==============================================================================

step "does the binary agree"

hostos="$(go env GOOS)"
hostarch="$(go env GOARCH)"
hostext=""
[ "$hostos" = "windows" ] && hostext=".exe"
hostbin="$out/atrium_${version}_${hostos}_${hostarch}/atrium$hostext"

if [ ! -x "$hostbin" ]; then
  echo "  no build for $hostos/$hostarch in this release, so nothing here can be run."
  echo "  the version was NOT confirmed against a running binary."
else
  reported="$("$hostbin" version --short)"
  if [ "$reported" != "$version" ]; then
    echo "  $hostbin says '$reported'" >&2
    refuse 5 "the binary reports '$reported' and the release is '$version'. one of them is a lie."
  fi
  echo "  atrium version --short  ->  $reported"
  full="$("$hostbin" version)"
  printf '%s\n' "$full" | sed 's/^/    /'
  printf '%s' "$full" | grep -q "$commit" ||
    refuse 5 "the binary does not report the commit being tagged. the ldflags did not take."
fi

# ==============================================================================
# 5. REPRODUCIBLE: build the commit again, somewhere the working tree cannot
#    reach, and compare
# ==============================================================================

step "is this build reproducible from the commit"

# `git archive` writes exactly what the commit carries and nothing else. No
# .git, no untracked file, no editor buffer. Building there and getting a
# different binary means the artefact does not come from the tag.
#
# ONE target, linux/amd64, because the question is whether the INPUTS were the
# commit and not whether every platform is deterministic. -trimpath and
# CGO_ENABLED=0 are what make the answer stable, and both live in release.sh,
# which is the script this calls rather than reimplements.
probe="$(mktemp -d)"
trap 'rm -rf "$probe"' EXIT
git archive --format=tar HEAD | tar -x -C "$probe"

probe_log="$probe/build.log"
if ATRIUM_COMMIT="$commit" ATRIUM_TARGETS="linux/amd64" \
  bash "$probe/scripts/release.sh" "$version" >"$probe_log" 2>&1; then
  a="$out/atrium_${version}_linux_amd64/atrium"
  b="$probe/build.claude/release/$version/atrium_${version}_linux_amd64/atrium"
  # THE BINARIES ARE COMPARED, NOT THE ARCHIVES. A tar carries modification
  # times, so two identical builds produce two different tarballs and comparing
  # those would fail every time for a reason that has nothing to do with the
  # code.
  ha="$(sha256sum "$a" | cut -d' ' -f1)"
  hb="$(sha256sum "$b" | cut -d' ' -f1)"
  if [ "$ha" != "$hb" ]; then
    echo "  from the working tree  $ha" >&2
    echo "  from the commit alone  $hb" >&2
    refuse 6 "the commit does not produce this build. something outside it reached the compiler."
  fi
  echo "  linux/amd64 builds byte for byte the same from the commit alone"
  echo "  $ha"
else
  tail -n 20 "$probe_log" | sed 's/^/    /' >&2
  if [ "$execute" = "yes" ]; then
    refuse 6 "the probe build failed, so reproducibility is unproven and this will not publish."
  fi
  echo "  the probe build failed. under --execute this would stop here."
fi
rm -rf "$probe"
trap - EXIT

# ==============================================================================
# 6. CHECKSUMS AND THE ASSET LIST
# ==============================================================================

step "the artefacts, and their hashes"
# publish-release.sh without --publish re-hashes every asset against
# checksums.txt and then prints the exact gh command it would run. That is the
# same verification the publish step does, done here where it is still cheap to
# act on.
notes_arg=()
[ -n "$notes_file" ] && notes_arg=(--notes="$notes_file")
bash scripts/publish-release.sh "$version" "${notes_arg[@]+"${notes_arg[@]}"}" ||
  refuse 7 "the artefacts do not match their checksums."

# ==============================================================================
# 7. THE MANIFEST: scoop, filled in
# ==============================================================================

step "the scoop manifest"

# SCOOP FIRST, and this is the reason it is first: it needs no account, no
# review and no certificate, so it exercises the whole release shape end to end
# before anything harder depends on the shape being right. If the archive layout
# or the checksum file is wrong, `scoop install atrium` is where it shows, and
# it shows to you.
winzip="atrium_${version}_windows_amd64.zip"
# The name is read with the leading `*` stripped, because sha256sum writes
# `hash *name` when it read the file in binary mode and `hash  name` when it did
# not, and which one you get depends on the platform the release was cut on. A
# manifest that silently loses its hash on Windows and finds it on Linux is the
# kind of difference that only shows up in somebody else's `scoop install`.
winhash="$(awk -v f="$winzip" '{ n = $2; sub(/^\*/, "", n); if (n == f) print $1 }' \
  "$out/checksums.txt")"
if [ -z "$winhash" ]; then
  refuse 7 "there is no line for $winzip in checksums.txt, so the manifest cannot be written."
fi

manifest_dir="$out/scoop"
manifest="$manifest_dir/atrium.json"
mkdir -p "$manifest_dir"
# The template is the source of truth for everything except the three fields a
# release decides. Substituting into it rather than generating a manifest from
# scratch means the notes, the autoupdate block and the description stay in one
# reviewable file.
#
# The tag form goes first: after v0.0.0 becomes v0.1.0, the only "0.0.0" left is
# the bare version field.
sed \
  -e "s#v0\.0\.0#${version}#g" \
  -e "s#\"0\.0\.0\"#\"${version#v}\"#g" \
  -e "s#0000000000000000000000000000000000000000000000000000000000000000#${winhash}#" \
  packaging/scoop-atrium.json | tee "$manifest" >/dev/null

echo "  written to $manifest"
echo "  version     ${version#v}"
echo "  hash        $winhash"
echo
echo "  it goes in the BUCKET repository as bucket/atrium.json, not in this one:"
echo "    https://github.com/dovholuknf/scoop-bucket"

# ==============================================================================
# 8. PUBLISH: the only part that leaves this machine
# ==============================================================================

if [ "$execute" != "yes" ]; then
  step "nothing was published"
  cat <<EOF
This was a dry run, which is the default. No tag was created, nothing was
pushed, and no release exists.

What --execute would run, in this order:

EOF
  if [ "$from_tag" = "no" ]; then
    echo "  git tag -a $version -m \"atrium $version\""
    echo "  git push origin $version"
  else
    echo "  (the tag already exists and would not be touched)"
  fi
  echo "  gh release create $version --verify-tag ... (the command printed above)"
  echo
  echo "run it again with --execute when the list above is what you want."
else
  if [ "$from_tag" = "no" ]; then
    step "the tag"
    git tag -a "$version" -m "atrium $version"
    echo "  created $version at $commit"
    git push origin "$version"
    echo "  pushed to origin"
    echo
    echo "  PUSHING THE TAG FIRES .github/workflows/release.yml, which runs this same"
    echo "  script with --from-tag --execute on a runner. If that workflow is enabled,"
    echo "  the publish below will race it and one of the two will find the release"
    echo "  already exists. Pick one. The workflow is the one to prefer."
  fi

  step "publish"
  bash scripts/publish-release.sh "$version" --publish "${notes_arg[@]+"${notes_arg[@]}"}"
fi

# ==============================================================================
# WHAT IS STILL A HUMAN'S, IN ORDER
# ==============================================================================

cat <<EOF

================================================================================
STILL YOURS, IN ORDER. None of it can be done from here, and each says why.
================================================================================

  1. THE SCOOP BUCKET. Create github.com/dovholuknf/scoop-bucket if it does not
     exist, copy $manifest to bucket/atrium.json,
     commit and push. Needs a repository this script has no business creating.

  2. PROVE THE SHAPE. On any Windows machine:
       scoop bucket add dovholuknf https://github.com/dovholuknf/scoop-bucket
       scoop install atrium
       atrium version
     That last line is the whole test. It must print $version and not dev.

  3. INSTALL A LINUX PACKAGE ON A LINUX MACHINE. Nothing here has ever done it,
     so the postinstall's live path and KillMode=mixed in the unit are unproven.
     See docs/packaging.md, "what was proved and what was not".

  4. SIGNING. Nothing is signed. macOS will quarantine the darwin archives and
     Chocolatey will not take an unsigned package. Both need a certificate that
     belongs to a person.

  5. AN APT AND YUM REPOSITORY, if and when there are users to upgrade. It costs
     a GPG key with a storage, trust and rotation story. The .deb and .rpm this
     built are the substrate either way, so nothing is wasted by waiting.

EOF
