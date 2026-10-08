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
step() { echo; echo "=== $*  [$(date -u +%H:%M:%S)]"; }
check() {
  local label="$1"; shift
  if "$@"; then
    echo "ok: $label"
  else
    echo "FAILED: $label" >&2
    fail=1
  fi
}

# ARTEFACTS. What is too big for the log goes here, and ci.yml uploads the
# folder with the run: go test's raw -json stream, every failed package's whole
# output, every test's time, and the goroutine dumps a failing test writes.
artefacts="build.claude/ci"
rm -rf "$artefacts"
mkdir -p "$artefacts/goroutines"
# Read by internal/testdiag. An absolute path, because each test binary runs in
# its own package's folder.
export CI_DIAG_DIR="$here/$artefacts/goroutines"

# THE SAME RUN HERE AS ON A RUNNER. A runner has four CPUs, so a laptop with
# twenty runs four packages at a time and four threads in each, like it does,
# unless CI_CPUS says otherwise. -count=1 because a cached pass is not a pass:
# setup-go restores the test cache on a runner, and a rerun here would replay
# the last result without running anything. No -race: it needs cgo, which a
# Windows runner does not set up, and the two platforms must run one command.
cpus="${CI_CPUS:-4}"
export GOMAXPROCS="$cpus"
# A shell inside an atrium room carries ATRIUM_* pointers at the live room, and
# a runner has none. testguard scrubs them inside the test binaries, but not
# from the scripts below, so they go here too.
leaked="$(env | sed -n 's/^\(ATRIUM_[A-Za-z0-9_]*\)=.*/\1/p' | tr '\n' ' ')"
for v in $leaked; do unset "$v"; done

step "the machine"
# The differences that have made a test pass in one place and fail in another.
echo "os:          $(uname -srm)"
echo "cpus:        $(getconf _NPROCESSORS_ONLN 2>/dev/null || echo "${NUMBER_OF_PROCESSORS:-?}") on the machine, GOMAXPROCS=$GOMAXPROCS, go test -p $cpus"
echo "user:        $(id -un 2>/dev/null || echo "${USERNAME:-?}")"
echo "go:          $(go version)"
echo "go env:      $(go env GOOS GOARCH CGO_ENABLED GOFLAGS GOTMPDIR | tr '\n' ' ')"
echo "temp:        TMPDIR=${TMPDIR:-} TEMP=${TEMP:-} TMP=${TMP:-}"
echo "free disk:   $(df -h "${TMPDIR:-${TEMP:-/tmp}}" 2>/dev/null | awk 'NR==2 {print $4 " in temp"}')"
echo "git:         $(git --version 2>/dev/null || echo none)"
echo "node:        $(node --version 2>/dev/null || echo none)"
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    echo "pwsh:        $(pwsh -NoProfile -Command '$PSVersionTable.PSVersion.ToString()' 2>/dev/null || echo none)" ;;
esac
echo "shells:      sh=$(command -v sh || echo none) bash=$BASH_VERSION"
echo "cleared:     ${leaked:-no ATRIUM_* variables were set}"
echo "artefacts:   $here/$artefacts"

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
# 20 minutes, not go test's 10. internal/daemon is about 1600 tests and takes
# 550 to 600 s on a windows-latest runner, so the default killed it mid-run and
# the dump read like a hang when nothing was stuck. A real hang still ends here.
#
# -json through scripts/ci-report.go, which prints one line per package, the
# output of each failed test and nothing of the ones that passed, the end of
# a package that died without a failing test (a timeout's panic names the
# tests still running), and the slowest tests. The raw stream is kept whole.
started=$(date +%s)
go test -json -count=1 -p "$cpus" -timeout 20m ./... 2>&1 \
  | tee "$artefacts/go-test.json" \
  | go run scripts/ci-report.go -dir "$artefacts"
codes=("${PIPESTATUS[@]}")
echo "go test took $(( $(date +%s) - started ))s"
if [ "${codes[0]}" = "0" ] && [ "${codes[2]}" = "0" ]; then
  echo "ok: go test"
else
  echo "FAILED: go test (go test exit ${codes[0]}, report exit ${codes[2]})" >&2
  fail=1
fi
dumps="$(ls "$artefacts/goroutines" 2>/dev/null | wc -l | tr -d ' ')"
if [ "$dumps" != "0" ]; then
  echo "$dumps goroutine dump(s) in $artefacts/goroutines, uploaded with the run"
fi

step "govulncheck"
# A vulnerability in the standard library or a dependency is found by this and
# not by an audit. Pinned so a new govulncheck cannot redden a build by itself,
# and the database it reads is always the current one, which is the point: a
# vulnerability published tomorrow fails the next run. Needs the network.
check "govulncheck" go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...

step "the board"
check "board" bash scripts/check-board.sh

step "the skins"
check "skins" bash scripts/check-skins.sh

step "the contrast"
# The skins check says every skin sets every variable. This one says the values
# are legible: text against the surface it actually sits on, in all twenty one
# palettes, with every rgba lift composited first.
#
# Skipped rather than failed with no node, like the board check above: it is a
# lint over a stylesheet, not a build step.
if command -v node >/dev/null 2>&1; then
  check "contrast" node scripts/check-contrast.js internal/api/web/css
else
  echo "skipped: no node on PATH, so the palette's contrast was not checked."
fi

step "the opencode plugin"
# The plugin is the opencode runner's permission gate, and it runs inside
# opencode where no Go test reaches. Its child_process is mocked, so this needs
# no atrium and no opencode.
if command -v node >/dev/null 2>&1; then
  check "opencode plugin" node --test --experimental-test-module-mocks scripts/opencode/atrium.test.mjs
else
  echo "skipped: no node on PATH, so the opencode plugin was not tested."
fi

step "what the release refuses"
# The refusals, not the release. This runs `cut-release.sh --preflight` against
# a throwaway repository, so it needs no network, builds nothing, and takes a
# second. The refusals are the part of a release script that is only ever
# exercised on the day it matters, which is the worst day to learn one of them
# was wrong.
check "release refusals" bash scripts/check-release.sh

step "the packaging scripts parse"
# A packaging script is run once, by a person, on the day it matters. A syntax
# error in one is found at exactly the wrong moment, so both shells are asked to
# parse without running: `sh -n` for the package scriptlets, and PowerShell's
# own parser for the Windows side.
for f in packaging/postinstall.sh packaging/preremove.sh \
         packaging/macos/scripts/postinstall \
         scripts/atrium-service.sh scripts/package-linux.sh \
         scripts/package-macos.sh \
         scripts/release.sh scripts/publish-release.sh \
         scripts/cut-release.sh scripts/check-release.sh \
         scripts/ci-linux.sh; do
  if ! bash -n "$f"; then
    echo "FAILED: $f does not parse" >&2
    fail=1
  fi
done
echo "ok: shell scripts parse"

# The PowerShell parse check runs in the Windows job only. pwsh is never started off Windows, even where one is
# installed (the GitHub ubuntu runner has it), so a Linux or macOS run skips this.
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*) on_windows=1 ;;
  *) on_windows=0 ;;
esac
if [ "$on_windows" = 1 ] && command -v pwsh >/dev/null 2>&1; then
  check "powershell" pwsh -NoProfile -File scripts/check-powershell.ps1
else
  echo "skipped: not the Windows job, or no pwsh on PATH, so the PowerShell scripts were not parsed."
fi

echo
if [ "$fail" != "0" ]; then
  echo "ci failed. see above." >&2
  exit 1
fi
echo "everything passed."
