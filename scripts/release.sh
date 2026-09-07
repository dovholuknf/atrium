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
# The commit is read from git, and can be OVERRIDDEN by the environment. That
# override exists for exactly one caller: the reproducibility probe in
# scripts/cut-release.sh unpacks the commit being tagged into a directory with
# no .git in it and builds there. Without the override it would have to repeat
# the ldflags, and a binary stamped two different ways in two places is a probe
# that proves nothing the day one of them is edited.
commit="${ATRIUM_COMMIT:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"

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
#
# Overridable for the same one caller. The probe rebuilds a SINGLE target and
# compares it, because five would cost five times as much to prove the same one
# thing: that nothing outside the commit reached the compiler.
targets="${ATRIUM_TARGETS:-windows/amd64 linux/amd64 linux/arm64 darwin/arm64 darwin/amd64}"

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
  #
  # `-buildvcs=false` because the version below is the only version. Go stamps
  # the revision and a dirty flag into the binary by itself whenever it can see
  # a .git, which means the SAME COMMIT builds two different binaries depending
  # on whether it was built in a checkout or in an export of itself. The
  # reproducibility probe in cut-release.sh does exactly that comparison and
  # found this, which is the whole reason the probe exists.
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
    -trimpath \
    -buildvcs=false \
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
  # The archives are collected rather than globbed straight into sha256sum,
  # because a glob that matches nothing is passed through literally and this
  # script runs under `pipefail`. That is not hypothetical: the reproducibility
  # probe in cut-release.sh builds ONE target, so one of the two globs is always
  # empty, and the failure looked like the probe itself was broken.
  archives=""
  for f in ./*.zip ./*.tar.gz; do
    [ -e "$f" ] && archives="$archives $f"
  done
  # shellcheck disable=SC2086
  sha256sum $archives | sed 's#\./##' | tee checksums.txt
)

echo
echo "in $out"
echo
echo "not done here, on purpose: nothing is signed, nothing is published, and"
echo "no tag was created. see docs/packaging.md for what each ecosystem needs."
