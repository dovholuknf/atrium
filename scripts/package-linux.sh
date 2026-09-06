#!/usr/bin/env bash
# Build the deb and the rpm, both architectures, from an already-built release.
#
#   scripts/release.sh v0.4.1        # first: the binaries and the archives
#   scripts/package-linux.sh v0.4.1  # then: the packages
#
# Separate from release.sh on purpose. release.sh needs nothing but a Go
# toolchain and is the thing you run to check a build compiles everywhere; this
# needs nfpm and produces artefacts only Linux can read. Folding them together
# would mean a Windows-only smoke test could not run without nfpm.
#
# ALL THE LOGIC IS HERE and the workflow only checks out and calls it, per the
# convention every script in this directory follows. Run it by hand first: the
# failures in packaging are a hash that does not match or a file that unpacks
# without its execute bit, and finding those out from a stranger is a bad way to
# find them out.

set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$here"

version="${1:-}"
if [ -z "$version" ]; then
  version="$(git describe --tags --exact-match 2>/dev/null || echo dev)"
fi

# THE LEADING v COMES OFF, and this is not cosmetic.
#
# dpkg requires an upstream version to BEGIN WITH A DIGIT. `v0.4.1` is rejected
# outright by a strict dpkg and accepted-then-mis-sorted by a lenient one, and
# rpm sorts a leading letter in a way nobody predicts. The git tag keeps its v,
# because that is what the tag is called and what the release URL contains; the
# package version does not.
#
# `dev` is left alone: it is not a version anybody upgrades between, and a
# package built from a working tree should look like one.
pkgversion="${version#v}"

out="build.claude/release/$version"
if [ ! -d "$out" ]; then
  echo "no release at $out" >&2
  echo "build one first:  scripts/release.sh $version" >&2
  exit 1
fi

# FINDING nfpm, in the order that respects what is already on the machine.
#
# A copy on PATH is somebody's choice and is used as-is. Otherwise one is built
# into build.claude/bin, which is where every Go build in this repository goes
# and is gitignored, so filling it with a tool is safe. A pinned version rather
# than @latest: a packaging tool that changes under you between two releases
# produces two differently-shaped packages for the same config, and the
# difference shows up as a dpkg error on somebody else's machine.
nfpm_version="v2.43.0"
nfpm="$(command -v nfpm || true)"
if [ -z "$nfpm" ]; then
  for candidate in "$here/build.claude/bin/nfpm" "$here/build.claude/bin/nfpm.exe"; do
    [ -x "$candidate" ] && nfpm="$candidate" && break
  done
fi
if [ -z "$nfpm" ]; then
  echo "nfpm is not here. building it into build.claude/bin ($nfpm_version)"
  GOBIN="$here/build.claude/bin" go install "github.com/goreleaser/nfpm/v2/cmd/nfpm@$nfpm_version"
  for candidate in "$here/build.claude/bin/nfpm" "$here/build.claude/bin/nfpm.exe"; do
    [ -x "$candidate" ] && nfpm="$candidate" && break
  done
fi
if [ -z "$nfpm" ]; then
  echo "could not build nfpm, so no deb and no rpm." >&2
  exit 1
fi

echo "atrium $version -> deb and rpm (package version $pkgversion)"
echo "  nfpm: $nfpm"
echo

# THE STAGING PATH, and the reason it exists rather than a third env var.
#
# nfpm expands environment variables through most of a config and NOT in
# `contents.src`: that value goes straight to the globber, which reports
# `glob failed: ${BINARY}: no matching files` and says nothing about expansion.
# Verified against v2.43.0 with a two-line config, after the first guess was
# that the path was wrong.
#
# So one fixed path is named in the config and the right binary is copied onto
# it once per architecture. Under build.claude/, which is gitignored.
stage="build.claude/pkg"
mkdir -p "$stage"

# ANY PACKAGE ALREADY IN HERE IS SWEPT AWAY FIRST, and this is not tidiness.
#
# A run that failed halfway leaves a zero-byte .deb behind, because nfpm creates
# the target file and then discovers it cannot fill it. The next run hashes
# whatever .deb files it finds, so that empty one goes into checksums.txt as a
# real artefact, and it goes into the release, and somebody installs it. That
# happened on the first run of this script. The empty-file hash is
# e3b0c44298fc..., which is worth recognising on sight.
rm -f "$out"/*.deb "$out"/*.rpm

for arch in amd64 arm64; do
  binary="$out/atrium_${version}_linux_${arch}/atrium"
  if [ ! -f "$binary" ]; then
    echo "  no linux/$arch binary at $binary, skipping" >&2
    continue
  fi
  cp "$binary" "$stage/atrium"
  for format in deb rpm; do
    printf '  %-5s %-6s ' "$format" "$arch"
    VERSION="$pkgversion" ARCH="$arch" \
      "$nfpm" package -f packaging/nfpm.yaml -p "$format" -t "$out"
  done
done
rm -rf "$stage"

echo
# ONE CHECKSUM FILE, REWRITTEN FROM WHAT IS ACTUALLY PRESENT.
#
# Not appended to. Appending means a line survives the artefact it describes:
# rebuild a release and the old hash is still in the file, and the first thing
# anybody knows about it is a scoop install that fails an integrity check.
#
# It has to cover the archives as well as the packages, because it is the file
# release.sh wrote and the file every manifest points at. Two checksum files is
# how a manifest ends up reading the one without its artefact in it.
(
  cd "$out"
  sha256sum ./*.zip ./*.tar.gz ./*.deb ./*.rpm 2>/dev/null |
    sed 's#\./##' | sort -k2 | tee checksums.txt
)

echo
echo "in $out"
echo
echo "nothing was published and nothing was signed. see docs/packaging.md."
