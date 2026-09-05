#!/usr/bin/env bash
# Build atrium for every platform it runs on, and hash the results.
#
# ALL THE LOGIC IS HERE and the CI workflow only checks out and calls this, per
# the convention in the global instructions. That way a release can be built and
# inspected on this machine before anything is published, which matters more
# than usual: the failures in packaging are things like a manifest pointing at a
# hash that does not match, and finding that out from a user is a bad way to
# find it out.
#
#   scripts/release.sh v0.4.1
#   scripts/release.sh            # dev build of every platform, for a smoke test
#
# What this does NOT do, deliberately:
#
#   - Sign anything. Code signing is the real cost of packaging and it needs
#     certificates that belong to a person, not to a repository.
#   - Publish anything. No release is created, no manifest is pushed, no tap is
#     updated. Those are one command each and they are the operator's to run.
#   - Tag anything. The version is read, never written.

set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$here"

version="${1:-}"
if [ -z "$version" ]; then
  version="$(git describe --tags --exact-match 2>/dev/null || echo dev)"
fi
commit="$(git rev-parse HEAD 2>/dev/null || echo unknown)"

# Under build.claude like everything else. It is gitignored, which is what makes
# it safe to fill with binaries.
out="build.claude/release/$version"
rm -rf "$out"
mkdir -p "$out"

echo "atrium $version ($commit)"
echo

# The platforms atrium is known to run on. Windows is the primary and is first.
#
# No 32-bit anything: nothing atrium talks to ships 32-bit builds any more, and
# a target nobody tests is a target that is broken. linux/arm64 earns its place
# because a small always-on box is a reasonable home for a daemon that outlives
# sessions.
targets="windows/amd64 linux/amd64 linux/arm64 darwin/arm64 darwin/amd64"

for target in $targets; do
  goos="${target%/*}"
  goarch="${target#*/}"
  ext=""
  [ "$goos" = "windows" ] && ext=".exe"

  name="atrium_${version}_${goos}_${goarch}"
  binary="$out/$name/atrium$ext"
  mkdir -p "$out/$name"

  printf '  %-22s' "$target"
  # CGO_ENABLED=0 is not a precaution, it is the point: `modernc.org/sqlite` is
  # pure Go so there is no cgo anywhere, and cross-compiling is a matter of two
  # environment variables. Setting it explicitly keeps a machine with a C
  # toolchain from quietly producing a binary that needs one.
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath \
    -ldflags "-s -w \
      -X github.com/dovholuknf/atrium/internal/cli.Version=$version \
      -X github.com/dovholuknf/atrium/internal/cli.Commit=$commit" \
    -o "$binary" ./cmd/atrium

  # An archive per platform, in the shape each ecosystem expects: zip for
  # Windows because scoop and everything else on that platform reads one, tar.gz
  # everywhere else.
  (
    cd "$out"
    if [ "$goos" = "windows" ]; then
      # A zip, by whichever means this machine has. `zip` is not installed on
      # a stock Windows box with Git Bash, which is the machine most likely to
      # be cutting this release, and discovering that at release time is the
      # worst moment to discover it. PowerShell's `Compress-Archive` is always
      # there.
      # PowerShell is looked for by full path as well as on PATH, because a Git
      # Bash shell on Windows frequently has neither `zip` NOR `powershell` on
      # its PATH while `powershell.exe` is very much installed.
      pwsh_exe=""
      for candidate in pwsh powershell.exe \
        /c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe; do
        if command -v "$candidate" >/dev/null 2>&1; then pwsh_exe="$candidate"; break; fi
      done
      if command -v zip >/dev/null 2>&1; then
        zip -q -r "$name.zip" "$name"
      elif [ -n "$pwsh_exe" ]; then
        "$pwsh_exe" -NoProfile -Command \
          "Compress-Archive -Path '$name' -DestinationPath '$name.zip' -Force" >/dev/null
      else
        echo "no zip and no powershell: cannot package the windows build" >&2
        exit 1
      fi
      echo "$name.zip"
    else
      tar -czf "$name.tar.gz" "$name"
      echo "$name.tar.gz"
    fi
  )
done

echo
# ONE checksum file covering everything, which is the shape every package
# manager reads. A manifest that carries a hash and an archive that does not
# match it is the single most common packaging failure, so the file is written
# here rather than by hand later.
(
  cd "$out"
  sha256sum ./*.zip ./*.tar.gz 2>/dev/null | sed 's#\./##' | tee checksums.txt
)

echo
echo "in $out"
echo
echo "not done here, on purpose: nothing is signed, nothing is published, and"
echo "no tag was created. see docs/packaging.md for what each ecosystem needs."
