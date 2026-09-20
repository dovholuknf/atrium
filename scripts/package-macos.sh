#!/usr/bin/env bash
# Build the macOS installer, both architectures, from an already-built release.
#
#   scripts/release.sh v0.4.1        # first: the binaries and the archives
#   scripts/package-macos.sh v0.4.1  # then: the .pkg installers
#
# Separate from release.sh for the same reason package-linux.sh is: release.sh
# needs nothing but a Go toolchain and is the thing you run to check a build
# compiles everywhere, and this needs tools that only exist on macOS.
#
# THIS ONE ONLY RUNS ON A MAC, and that is a real limit rather than an oversight.
# `pkgbuild` and `productbuild` are Apple's, ship with the developer tools, and
# have no cross-platform equivalent, so unlike nfpm this cannot build a macOS
# artefact from Windows. On any other platform it says so and exits, and CI parses
# it without running it. docs/packaging.md records that no .pkg has been built on
# this machine because there is not a Mac here to build it on.
#
# ALL THE LOGIC IS HERE and the workflow only checks out and calls it, per the
# convention every script in this directory follows.
#
# WHAT IT DOES NOT DO: sign or notarize. An unsigned .pkg installs after a
# right-click-open or a one-time Gatekeeper approval, and a signed one needs a
# Developer ID Installer certificate and an Apple account to notarize against,
# both of which belong to a person and not to a repository. Set
# ATRIUM_INSTALLER_IDENTITY to a "Developer ID Installer: ..." identity to sign;
# leave it empty for the unsigned artefact. Notarization is a separate step and
# is listed at the end.

set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$here"

version="${1:-}"
if [ -z "$version" ]; then
  version="$(git describe --tags --exact-match 2>/dev/null || echo dev)"
fi

# THE LEADING v COMES OFF, matching package-linux.sh. macOS does not reject a
# leading letter the way dpkg does, but `installer` compares versions to decide
# whether one package supersedes another, and a version that sorts differently on
# three platforms is a version that will confuse one of them. The git tag keeps
# its v; the package version does not.
pkgversion="${version#v}"

out="build.claude/release/$version"
if [ ! -d "$out" ]; then
  echo "no release at $out" >&2
  echo "build one first:  scripts/release.sh $version" >&2
  exit 1
fi

if [ "$(uname -s)" != "Darwin" ]; then
  echo "scripts/package-macos.sh builds a .pkg and needs Apple's pkgbuild and" >&2
  echo "productbuild, which exist only on macOS. this is $(uname -s)." >&2
  echo "run it on a Mac. the darwin binaries in $out are what it packages." >&2
  exit 2
fi

for tool in pkgbuild productbuild; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "$tool is not on PATH. install the Xcode command line tools:" >&2
    echo "  xcode-select --install" >&2
    exit 1
  }
done

identity="${ATRIUM_INSTALLER_IDENTITY:-}"
echo "atrium $version -> macOS .pkg (package version $pkgversion)"
if [ -n "$identity" ]; then
  echo "  signing with: $identity"
else
  echo "  UNSIGNED. set ATRIUM_INSTALLER_IDENTITY to sign. see the notes below."
fi
echo

# ANY .pkg ALREADY HERE IS SWEPT AWAY FIRST, for the reason package-linux.sh
# spells out: a run that fails halfway leaves a partial file behind, and the next
# run hashes whatever it finds and puts that into the release.
rm -f "$out"/*.pkg

# The Xcode tools sometimes strip the execute bit when the payload is unpacked
# from an archive on a case-insensitive volume, so it is set explicitly rather
# than trusted from the checkout.
chmod +x packaging/macos/scripts/postinstall

for arch in arm64 amd64; do
  # GOARCH is amd64 and arm64; macOS and its installer call the Intel one
  # x86_64. The archive directory is named with the GOARCH form, the installer
  # metadata with the Apple form.
  case "$arch" in
    amd64) macarch="x86_64" ;;
    arm64) macarch="arm64" ;;
  esac

  binary="$out/atrium_${version}_darwin_${arch}/atrium"
  if [ ! -f "$binary" ]; then
    echo "  no darwin/$arch binary at $binary, skipping" >&2
    continue
  fi

  printf '  %-6s ' "$arch"

  # THE PAYLOAD ROOT, mirroring an installed system. Everything under it lands at
  # the same path on the target, so /usr/local/bin/atrium here is
  # /usr/local/bin/atrium there.
  #
  # /usr/local rather than /usr/bin: /usr on macOS is on the read-only system
  # volume and owned by the OS, and a package that writes there needs to defeat
  # System Integrity Protection. /usr/local is the documented home for
  # third-party command line tools and is what Homebrew, among others, uses.
  root="build.claude/macos/$arch/root"
  rm -rf "$root"
  mkdir -p "$root/usr/local/bin" \
           "$root/usr/local/share/atrium" \
           "$root/usr/local/share/doc/atrium"

  cp "$binary" "$root/usr/local/bin/atrium"
  chmod 0755 "$root/usr/local/bin/atrium"

  # THE PLIST TEMPLATE TRAVELS WITH THE PACKAGE, so the postinstall can render it
  # for whoever is at the console. It is the same template scripts/atrium-service.sh
  # renders by hand, which is what keeps the loaded LaunchAgent identical however
  # atrium was installed.
  cp packaging/atrium.plist "$root/usr/local/share/atrium/atrium.plist"
  cp README.md "$root/usr/local/share/doc/atrium/README.md"
  cp docs/packaging.md "$root/usr/local/share/doc/atrium/packaging.md"

  component="build.claude/macos/$arch/atrium-component.pkg"
  pkgbuild \
    --root "$root" \
    --identifier io.github.dovholuknf.atrium \
    --version "$pkgversion" \
    --install-location / \
    --scripts packaging/macos/scripts \
    "$component" >/dev/null

  # THE DISTRIBUTION WRAPS THE COMPONENT so the installer refuses the wrong
  # architecture. The template carries @PLACEHOLDERS@ because a distribution
  # cannot expand a variable, so they are substituted here, once per arch.
  dist="build.claude/macos/$arch/distribution.xml"
  sed -e "s#@VERSION@#$pkgversion#g" \
      -e "s#@ARCH@#$macarch#g" \
      -e "s#@TITLE@#atrium $version#g" \
      -e "s#@COMPONENT@#atrium-component.pkg#g" \
      packaging/macos/distribution.xml.in > "$dist"

  final="$out/atrium_${version}_darwin_${arch}.pkg"
  sign=()
  [ -n "$identity" ] && sign=(--sign "$identity")
  productbuild \
    --distribution "$dist" \
    --package-path "build.claude/macos/$arch" \
    "${sign[@]+"${sign[@]}"}" \
    "$final" >/dev/null

  echo "$(basename "$final")"
done

rm -rf build.claude/macos

echo
# ONE CHECKSUM FILE, REWRITTEN FROM WHAT IS ACTUALLY PRESENT, and it covers every
# artefact type so running this after package-linux.sh does not drop the deb and
# the rpm from the file every manifest points at. The reasoning is in
# package-linux.sh at length.
(
  cd "$out"
  # COLLECTED rather than globbed straight into sha256sum: a glob that matches
  # nothing is passed through literally, and under `pipefail` sha256sum fails on
  # the literal `./*.rpm` and takes the whole script down, even though every real
  # artefact was already written. release.sh spells the trap out at length.
  artefacts=""
  for f in ./*.zip ./*.tar.gz ./*.deb ./*.rpm ./*.pkg ./*.msi; do
    [ -e "$f" ] && artefacts="$artefacts $f"
  done
  # shellcheck disable=SC2086
  sha256sum $artefacts | sed 's#\./##' | sort -k2 | tee checksums.txt
)

echo
echo "in $out"
echo
if [ -z "$identity" ]; then
  cat <<'EOF'
NOTHING WAS SIGNED. To ship this without a Gatekeeper warning clint must supply:

  1. A "Developer ID Installer" certificate, from an Apple Developer account
     ($99/year). Then re-run with:
       ATRIUM_INSTALLER_IDENTITY="Developer ID Installer: Name (TEAMID)" \
         scripts/package-macos.sh <version>

  2. Notarization, which is a separate step after signing and needs an
     app-specific password or an App Store Connect API key:
       xcrun notarytool submit atrium_<version>_darwin_<arch>.pkg \
         --apple-id you@example.com --team-id TEAMID --password <app-pw> --wait
       xcrun stapler staple atrium_<version>_darwin_<arch>.pkg

An unsigned .pkg still installs: right-click the file and choose Open, or clear
the quarantine with  xattr -d com.apple.quarantine atrium_<version>_darwin_<arch>.pkg
EOF
fi
