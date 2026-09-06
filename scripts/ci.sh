#!/usr/bin/env bash
# Everything CI runs, in one file you can run yourself.
#
#   scripts/ci.sh
#
# THE WORKFLOW CONTAINS NO LOGIC. `.github/workflows/ci.yml` checks out the
# code, installs Go and node, and calls this. That is the whole rule and the
# reason for it is ordinary: a check that only exists inside a YAML file can
# only be debugged by pushing, and every push is a five minute round trip to
# find out you got a quoting wrong.
#
# It follows that this has to work on a developer's machine, which here means
# Windows with Git Bash. Nothing below assumes a Linux tool that is not also in
# Git Bash, and anything optional is skipped with a line saying so rather than
# failing.

set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$here"

fail=0
step() { echo; echo "=== $*"; }
check() {
  local label="$1"; shift
  if "$@"; then
    echo "ok: $label"
  else
    echo "FAILED: $label" >&2
    fail=1
  fi
}

step "gofmt"
# `gofmt -l` prints the files it would change and exits zero either way, so the
# emptiness of the output is the test rather than the exit code.
unformatted="$(gofmt -l . | grep -v '^build\.claude/' || true)"
if [ -n "$unformatted" ]; then
  echo "these files are not gofmt clean:" >&2
  echo "$unformatted" >&2
  fail=1
else
  echo "ok: gofmt"
fi

step "go vet"
check "go vet" go vet ./...

step "go build"
# `-o build.claude/` even though `./...` writes nothing anybody keeps.
#
# Go builds in this repository land in build.claude/ and never in the root, and
# there is a hook that enforces it on a developer's machine. A CI script that
# would be blocked by the repository's own hook is a script nobody can run
# locally, which is the one thing this file exists to avoid.
check "go build" go build -o build.claude/ ./...

step "go test"
check "go test" go test ./...

step "the board"
check "board" bash scripts/check-board.sh

step "the skins"
check "skins" bash scripts/check-skins.sh

step "the packaging scripts parse"
# A packaging script is run once, by a person, on the day it matters. A syntax
# error in one is found at exactly the wrong moment, so both shells are asked to
# parse without running: `sh -n` for the package scriptlets, and PowerShell's
# own parser for the Windows side.
for f in packaging/postinstall.sh packaging/preremove.sh \
         scripts/atrium-service.sh scripts/package-linux.sh \
         scripts/release.sh scripts/publish-release.sh; do
  if ! bash -n "$f"; then
    echo "FAILED: $f does not parse" >&2
    fail=1
  fi
done
echo "ok: shell scripts parse"

if command -v pwsh >/dev/null 2>&1; then
  check "powershell" pwsh -NoProfile -File scripts/check-powershell.ps1
else
  echo "skipped: no pwsh on PATH, so the PowerShell scripts were not parsed."
fi

echo
if [ "$fail" != "0" ]; then
  echo "ci failed. see above." >&2
  exit 1
fi
echo "everything passed."
