#!/usr/bin/env bash
# Put a built release on GitHub Releases. Prints what it would do unless told
# otherwise.
#
#   scripts/publish-release.sh v0.4.1              # says what it would upload
#   scripts/publish-release.sh v0.4.1 --publish    # actually does it
#
# IT REFUSES TO PUBLISH BY DEFAULT, and that is not timidity. Every other script
# here can be run twice with no consequence. This one creates a tag other people
# will install from, and a release with the wrong hash in it is the packaging
# failure that is only ever found by a stranger. So the safe thing is the
# default and the dangerous thing is a word you have to type.
#
# ================================================================================
# WHY GITHUB RELEASES AND NOT AN APT REPOSITORY OR GHCR
# ================================================================================
#
# The three candidates, and what each costs. The full write-up is in
# docs/packaging.md; the short version is here because this is the file that
# implements the answer.
#
#   GITHUB RELEASES, .deb and .rpm as assets.  CHOSEN.
#     Costs nothing that is not already built: release.sh writes the archives
#     and the checksums, package-linux.sh writes the packages, and this uploads
#     them. Installing is `curl` then `dpkg -i` or `dnf install ./atrium.rpm`,
#     which is one line in a README. It is also the substrate under both of the
#     others: an apt repository and an OCI artefact would both be built from
#     exactly these files, so nothing here is thrown away by changing course.
#     What it does not give you is `apt upgrade`. Named as the cost rather than
#     discovered: you find out about a new atrium the way you find out about a
#     new anything on GitHub, by looking.
#
#   AN APT AND YUM REPOSITORY, hosted on GitHub Pages.
#     Buys `apt upgrade atrium` and `dnf upgrade atrium`, which is the real
#     thing the other two only approximate. Costs a GPG SIGNING KEY, and that
#     key is the whole story: it has to be generated, kept somewhere that is not
#     a repository, put in CI as a secret, published for people to trust, and
#     rotated at some point by somebody who remembers how. It is the same class
#     of cost as code signing, which docs/packaging.md already names as the
#     thing that gates Chocolatey and a pleasant Homebrew. Worth doing when
#     there are users to upgrade. There are not yet.
#
#   GHCR AS AN OCI REGISTRY.
#     Worth being precise about, because the obvious reading of it is wrong. A
#     CONTAINER IMAGE of the daemon is close to useless: atrium opens pseudo
#     terminals and spawns claude sessions that need the user's PATH, shell
#     configuration, ssh agent and Claude Code configuration, and a container
#     has none of those. That is the same reason the systemd unit is a user
#     unit, one layer out. GHCR can also hold non-container OCI ARTEFACTS,
#     which is a real and different idea: `oras pull` a .deb. But nothing on a
#     Linux machine reaches for oras to install software, so it buys a
#     distribution channel with no clients.
#
# Overruling this is cheap and that is deliberate: the artefacts are the same
# either way, and only the last step changes.

set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$here"

version="${1:-}"
publish=no
notes_file=""
for arg in "$@"; do
  case "$arg" in
    --publish) publish=yes ;;
    --notes=*) notes_file="${arg#--notes=}" ;;
  esac
done

if [ -z "$version" ] || [ "${version#-}" != "$version" ]; then
  echo "usage: $0 <version> [--publish] [--notes=FILE]" >&2
  exit 2
fi

out="build.claude/release/$version"
if [ ! -d "$out" ]; then
  echo "no release at $out" >&2
  echo "build one:  scripts/release.sh $version && scripts/package-linux.sh $version" >&2
  exit 1
fi

# WHAT GOES UP: every archive, every package, and the checksum file.
#
# The checksum file is listed explicitly and last, because the scoop manifest's
# autoupdate block reads it from the release URL to find its own hash. A release
# missing it is a release where scoop cannot update itself, and that is not
# visible until somebody tries.
assets=""
for f in "$out"/*.zip "$out"/*.tar.gz "$out"/*.deb "$out"/*.rpm; do
  [ -e "$f" ] && assets="$assets $f"
done
if [ -e "$out/checksums.txt" ]; then
  assets="$assets $out/checksums.txt"
else
  echo "there is no checksums.txt in $out, and a release without one is not one." >&2
  exit 1
fi

# EVERY ASSET IS RE-HASHED HERE, against the file that claims to know.
#
# This is the one check worth having in a publish step, because it catches the
# exact failure that costs the most: a checksums.txt written before the last
# rebuild, so the manifest says one thing and the artefact is another. Cheap to
# do, and the only moment anybody would notice is now.
echo "verifying $out/checksums.txt against what is actually there"
(
  cd "$out"
  if ! sha256sum -c checksums.txt --quiet; then
    echo "the checksums do not match the files. do not publish this." >&2
    exit 1
  fi
)
echo "  every hash matches."
echo

echo "release $version would carry:"
for f in $assets; do
  printf '  %10s  %s\n' "$(du -h "$f" | cut -f1)" "$(basename "$f")"
done
echo

if [ "$publish" != "yes" ]; then
  cat <<EOF
NOTHING WAS PUBLISHED. --publish was not passed.

what it would run:
  gh release create $version --title "atrium $version" ${notes_file:+--notes-file $notes_file} \\
$(for f in $assets; do echo "    $f \\"; done)
    --verify-tag

before that is safe, the tag has to exist and be pushed:
  git tag -a $version -m "atrium $version"
  git push origin $version
EOF
  exit 0
fi

if ! command -v gh >/dev/null 2>&1; then
  echo "gh is not on PATH. it is the only way this script talks to GitHub." >&2
  exit 1
fi

# `--verify-tag` REFUSES TO INVENT A TAG.
#
# Without it, `gh release create` will happily create the tag for you off
# whatever HEAD happens to be, which means a release can be cut from a commit
# nobody tagged and nobody can find again. The tag is a decision; this script
# only publishes against one that was already made.
notes_arg=()
[ -n "$notes_file" ] && notes_arg=(--notes-file "$notes_file")
[ -z "$notes_file" ] && notes_arg=(--generate-notes)

# shellcheck disable=SC2086
gh release create "$version" \
  --title "atrium $version" \
  --verify-tag \
  "${notes_arg[@]}" \
  $assets

echo
echo "published $version."
echo
echo "still yours to do, because none of it is automatable from here:"
echo "  - update the scoop bucket: version, url and hash from checksums.txt"
echo "    (or let the autoupdate block in packaging/scoop-atrium.json do it)"
echo "  - nothing is signed. macOS will quarantine the darwin archives."
