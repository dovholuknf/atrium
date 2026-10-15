#!/usr/bin/env bash
# Everything CI runs, in one file you can run yourself. The same file runs on linux, macos and windows (Git Bash).
#
#   bash scripts/ci.sh                  every step
#   bash scripts/ci.sh --only go-test   one step, or several: --only gofmt,go-vet
#   bash scripts/ci.sh --list           the step names
#
# THE WORKFLOW CONTAINS NO LOGIC. `.github/workflows/ci.yml` checks out the code, installs Go and node, and calls this.
# That is the whole rule and the reason for it is ordinary: a check that only exists inside a YAML file can only be
# debugged by pushing, and every push is a five minute round trip to find out you got a quoting wrong.
#
# It follows that this has to work on a developer's machine, which here means Windows with Git Bash, a Mac, or Linux.
# Nothing below assumes a tool that is not also in Git Bash.
#
# THE TOOLCHAIN IS CHECKED, NOT INSTALLED. Go must be the version go.mod names and node must be major 22, or the run
# stops before it checks anything (CI_ALLOW_TOOLCHAIN=1 turns that into a loud warning, for a machine that cannot have
# them). A missing node is a failure: a skipped check that reads as green is worse than a red one.
#
# THE REPORT. build.claude/ci/summary.md says what ran, how long each step took, which tests failed with the end of
# their output, the slowest tests, and the line that reproduces the run. The workflow copies it to the run's summary.
#
# A flaky test is a failure. There is no rerun and no quarantine list.

set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$here"

all_steps="machine toolchain gofmt go-vet go-build go-test go-race time-guard govulncheck board skins contrast opencode release parse powershell ps-tests clean-tree"

only=""
while [ $# -gt 0 ]; do
  case "$1" in
    --only) [ $# -ge 2 ] && [ -n "$2" ] || { echo "--only needs a step name. steps: $all_steps" >&2; exit 2; }; only="$2"; shift 2 ;;
    --only=*) only="${1#--only=}"; shift ;;
    --list) echo $all_steps | tr ' ' '\n'; exit 0 ;;
    -h|--help) sed -n '2,8p' "$0"; exit 0 ;;
    *) echo "usage: scripts/ci.sh [--only step[,step]] [--list]" >&2; exit 2 ;;
  esac
done
for o in $(echo "$only" | tr ',' ' '); do
  case " $all_steps " in *" $o "*) ;; *) echo "no step named '$o'. steps: $all_steps" >&2; exit 2 ;; esac
done
# The machine and toolchain steps are cheap and say what everything else ran on, so they always run.
want() {
  [ -z "$only" ] && return 0
  case "$1" in machine|toolchain) return 0 ;; esac
  case ",$only," in *",$1,"*) return 0 ;; esac
  return 1
}

case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*) os=windows ;;
  Darwin) os=macos ;;
  *) os=linux ;;
esac

fail=0; nfail=0
mark_fail() { fail=1; nfail=$((nfail + 1)); }
# ARTEFACTS. What is too big for the log goes here, and ci.yml uploads the folder with the run: go test's raw -json
# stream, every failed package's whole output, every test's time, the goroutine dumps a failing test writes, and
# summary.md.
artefacts="build.claude/ci"
# --only keeps the last go-test.json unless go-test is being rerun, so time-guard can read it.
kept=""
if [ -n "$only" ] && [ -f "$artefacts/go-test.json" ]; then
  case ",$only," in *",go-test,"*) ;; *) kept="$(mktemp)"; cp "$artefacts/go-test.json" "$kept" ;; esac
fi
rm -rf "$artefacts"
mkdir -p "$artefacts/goroutines"
if [ -n "$kept" ]; then mv "$kept" "$artefacts/go-test.json"; fi
steps_tsv="$artefacts/steps.tsv"
: > "$steps_tsv"
# Read by internal/testdiag. An absolute path, because each test binary runs in its own package's folder.
export CI_DIAG_DIR="$here/$artefacts/goroutines"

# The steps: name, seconds, result. A step is closed by the next one starting, or by the end.
cur=""; cur_start=0; cur_fail=0
close_step() {
  [ -z "$cur" ] && return
  local res=ok
  [ "$nfail" != "$cur_fail" ] && res=FAILED
  printf '%s\t%s\t%s\n' "$cur" "$(( $(date +%s) - cur_start ))" "$res" >> "$steps_tsv"
  cur=""
}
# step <name> <title>: returns 1 when --only leaves this step out, and then the caller skips its body.
step() {
  close_step
  if ! want "$1"; then return 1; fi
  cur="$1"; cur_start=$(date +%s); cur_fail=$nfail
  echo; echo "=== $2  [$(date -u +%H:%M:%S)]"
}
check() {
  local label="$1"; shift
  if "$@"; then
    echo "ok: $label"
  else
    echo "FAILED: $label" >&2
    mark_fail
  fi
}
# Said once, in the summary: the machine and the toolchain, as the run saw them.
machine_note=""

# THE SAME RUN HERE AS ON A RUNNER. A runner has four CPUs, so a laptop with twenty runs four packages at a time and
# four threads in each, like it does, unless CI_CPUS says otherwise. -count=1 because a cached pass is not a pass:
# setup-go restores the test cache on a runner, and a rerun here would replay the last result without running
# anything.
cpus="${CI_CPUS:-4}"
export GOMAXPROCS="$cpus"
# A shell inside an atrium room carries ATRIUM_* pointers at the live room, and a runner has none. testguard scrubs
# them inside the test binaries, but not from the scripts below, so they go here too.
leaked="$(env | sed -n 's/^\(ATRIUM_[A-Za-z0-9_]*\)=.*/\1/p' | tr '\n' ' ')"
for v in $leaked; do unset "$v"; done

# What the working tree looked like before, for the last step: a run must not leave a change in a tracked file or add
# an untracked one. The tree may be dirty to begin with on a laptop, so it is compared, not required to be empty.
git_before="$(git status --porcelain --untracked-files=all 2>/dev/null | grep -v ' build\.claude/' | sort)"

# THE SUMMARY. Written at the end however the run ends.
write_summary() {
  close_step
  local f="$artefacts/summary.md" verdict="PASSED"
  [ "$fail" != "0" ] && verdict="FAILED"
  {
    echo "# ci on $os: $verdict"
    echo
    echo "- $machine_note"
    echo "- commit: $(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
    echo
    echo "## Steps"
    echo
    echo "| step | seconds | result |"
    echo "|---|---|---|"
    awk -F'\t' '{printf "| %s | %s | %s |\n", $1, $2, $3}' "$steps_tsv"
    local failed_steps
    failed_steps="$(awk -F'\t' '$3=="FAILED" {printf "%s%s", sep, $1; sep=","}' "$steps_tsv")"
    # A failed package's whole output is in <package>.txt, and the tail of each is here.
    local any=0 t
    for t in "$artefacts"/*.txt; do
      [ -f "$t" ] || continue
      if [ "$any" = 0 ]; then echo; echo "## Failed packages (end of their output)"; any=1; fi
      echo
      echo "### $(basename "$t" .txt)"
      echo
      echo '```'
      tail -n 40 "$t"
      echo '```'
    done
    if [ -f "$artefacts/timings.tsv" ]; then
      echo
      echo "## Slowest 10 tests"
      echo
      echo "| seconds | package | test |"
      echo "|---|---|---|"
      tail -n +2 "$artefacts/timings.tsv" | sort -t$'\t' -k1,1 -rn > "$artefacts/timings.sorted.tsv"
      head -n 10 "$artefacts/timings.sorted.tsv" \
        | awk -F'\t' '{printf "| %s | %s | %s |\n", $1, $2, $3}'
    fi
    echo
    echo "## Reproduce"
    echo
    echo '```'
    echo "bash scripts/ci.sh"
    [ -n "$failed_steps" ] && echo "bash scripts/ci.sh --only $failed_steps"
    echo '```'
    echo
    echo "Raw files are in the run's artefacts (build.claude/ci/)."
  } > "$f"
  echo "summary: $f"
}

step machine "the machine"
# The differences that have made a test pass in one place and fail in another.
echo "os:          $(uname -srm)"
echo "cpus:        $(getconf _NPROCESSORS_ONLN 2>/dev/null || echo "${NUMBER_OF_PROCESSORS:-?}") on the machine, GOMAXPROCS=$GOMAXPROCS, go test -p $cpus"
echo "user:        $(id -un 2>/dev/null || echo "${USERNAME:-?}")"
echo "go:          $(go version 2>/dev/null || echo none)"
echo "go env:      $(go env GOOS GOARCH CGO_ENABLED GOFLAGS GOTMPDIR 2>/dev/null | tr '\n' ' ')"
echo "temp:        TMPDIR=${TMPDIR:-} TEMP=${TEMP:-} TMP=${TMP:-}"
echo "free disk:   $(df -h "${TMPDIR:-${TEMP:-/tmp}}" 2>/dev/null | awk 'NR==2 {print $4 " in temp"}')"
echo "git:         $(git --version 2>/dev/null || echo none)"
echo "node:        $(node --version 2>/dev/null || echo none)"
if [ "$os" = windows ]; then
  echo "pwsh:        $(pwsh -NoProfile -Command '$PSVersionTable.PSVersion.ToString()' 2>/dev/null || echo none)"
fi
echo "shells:      sh=$(command -v sh || echo none) bash=$BASH_VERSION"
echo "cleared:     ${leaked:-no ATRIUM_* variables were set}"
echo "artefacts:   $here/$artefacts"
machine_note="$os, $(uname -m), $(go version 2>/dev/null | awk '{print $3}'), node $(node --version 2>/dev/null || echo none), $cpus cpus"

step toolchain "the toolchain"
# Checked, not installed: the workflow's setup- steps install, a laptop installs its own.
want_go="$(sed -n 's/^go \([0-9.]*\)$/\1/p' go.mod)"
have_go="$(go version 2>/dev/null | sed -n 's/^go version go\([0-9.]*\).*/\1/p')"
want_node=22
have_node="$(node --version 2>/dev/null | sed -n 's/^v\([0-9]*\).*/\1/p')"
tool_bad=0
if [ "$have_go" != "$want_go" ]; then
  echo "go is '${have_go:-missing}', go.mod says $want_go" >&2; tool_bad=1
fi
if [ -z "$have_node" ]; then
  echo "node is missing. CI needs node $want_node for the board checks." >&2; tool_bad=1
elif [ "$have_node" != "$want_node" ]; then
  echo "node is major $have_node, CI uses $want_node" >&2; tool_bad=1
fi
if [ "$tool_bad" = 1 ]; then
  if [ "${CI_ALLOW_TOOLCHAIN:-}" = "1" ]; then
    echo "WARNING: wrong toolchain, continuing because CI_ALLOW_TOOLCHAIN=1. this run does not reproduce CI." >&2
  else
    echo "FAILED: toolchain. install the versions above, or CI_ALLOW_TOOLCHAIN=1 to run anyway." >&2
    mark_fail
    write_summary
    exit 1
  fi
else
  echo "ok: go $have_go, node $have_node"
fi
# The headless board tests need playwright and its browser. Installed here, so a fresh machine needs nothing else.
if want board && command -v npm >/dev/null 2>&1; then
  if [ -f package-lock.json ]; then
    check "npm ci" npm ci --no-audit --no-fund
  else
    check "npm install" npm install --no-audit --no-fund
  fi
  deps=""
  if [ "$os" = linux ] && [ -n "${CI:-}" ]; then deps="--with-deps"; fi
  check "playwright browser" npx --no-install playwright install $deps chromium
fi

if step gofmt "gofmt"; then
  # `gofmt -l` prints the files it would change and exits zero either way, so the emptiness of the output is the
  # test rather than the exit code.
  unformatted="$(gofmt -l . | grep -v '^build\.claude/' || true)"
  if [ -n "$unformatted" ]; then
    echo "these files are not gofmt clean:" >&2
    echo "$unformatted" >&2
    mark_fail
  else
    echo "ok: gofmt"
  fi
fi

if step go-vet "go vet"; then
  # With the tag, so the integration test files are vetted too. See scripts/test.sh.
  check "go vet" go vet -tags integration ./...
fi

if step go-build "go build"; then
  # `-o build.claude/` even though `./...` writes nothing anybody keeps. Go builds in this repository land in
  # build.claude/ and never in the root, and there is a hook that enforces it on a developer's machine.
  check "go build" go build -o build.claude/ ./...
  # No test assigns a package variable: a hook, delay, limit or clock belongs on the struct that uses it.
  check "test globals" go run scripts/check-test-globals.go
fi

if step go-test "go test"; then
  # 20 minutes, not go test's 10. internal/daemon is about 1600 tests and takes 550 to 600 s on a windows-latest
  # runner, so the default killed it mid-run and the dump read like a hang when nothing was stuck.
  #
  # -json through scripts/ci-report.go, which prints one line per package, the output of each failed test and
  # nothing of the ones that passed, the end of a package that died without a failing test, and the slowest tests.
  # The raw stream is kept whole.
  started=$(date +%s)
  # ALL the tests, unit and integration. CI is the only place the integration tests run. See scripts/test.sh.
  bash scripts/test.sh full -json -p "$cpus" -timeout 20m 2>&1 \
    | tee "$artefacts/go-test.json" \
    | go run scripts/ci-report.go -dir "$artefacts"
  codes=("${PIPESTATUS[@]}")
  echo "go test took $(( $(date +%s) - started ))s"
  if [ "${codes[0]}" = "0" ] && [ "${codes[2]}" = "0" ]; then
    echo "ok: go test"
  else
    echo "FAILED: go test (go test exit ${codes[0]}, report exit ${codes[2]})" >&2
    mark_fail
  fi
  dumps="$(ls "$artefacts/goroutines" 2>/dev/null | wc -l | tr -d ' ')"
  if [ "$dumps" != "0" ]; then
    echo "$dumps goroutine dump(s) in $artefacts/goroutines, uploaded with the run"
  fi
fi

if step go-race "go test -race (unit tests)"; then
  # The unit tests only, under the race detector, on linux and macos. Windows has no cgo on a runner, and the race
  # detector needs it, so Windows skips this.
  if [ "$os" = windows ]; then
    echo "skipped: -race needs cgo, which the windows job does not set up."
  else
    check "go test -race" go test -race -count=1 -p "$cpus" -timeout 20m ./...
  fi
fi

if step time-guard "test time guard"; then
  # A package over 60s or a test over 5s fails unless scripts/test-time-allow.txt names it. Reads the stream kept above.
  # The self-test and the parser's Go tests run first, so a broken guard is not taken for a fast suite.
  check "time guard self-test" bash scripts/test-time-guard-test.sh
  check "time guard parser" go test scripts/ci-report.go scripts/ci-report_test.go
  # A hosted runner is about twice as slow as the box the ceilings were measured on (run 38269244777: 12 over, none by
  # more than 2.2x), so it gets twice the room. A developer's machine keeps the real limits.
  if [ -n "${GITHUB_ACTIONS:-}" ]; then export TEST_TIME_SCALE=2; fi
  if [ -f "$artefacts/go-test.json" ]; then
    check "test time guard" bash scripts/test-time-guard.sh "$artefacts/go-test.json"
  else
    echo "FAILED: no $artefacts/go-test.json to read. run the go-test step first." >&2
    mark_fail
  fi
fi

if step govulncheck "govulncheck"; then
  # A vulnerability in the standard library or a dependency is found by this and not by an audit. Pinned so a new
  # govulncheck cannot redden a build by itself, and the database it reads is always the current one. Needs the network.
  check "govulncheck" go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
fi

if step board "the board"; then
  # Includes the headless browser run. Any screenshots it takes go under build.claude/ and are not compared.
  # The whole suite, so it says so: check-board.sh refuses a full run without FULL_SUITE=1.
  check "board" env FULL_SUITE=1 bash scripts/check-board.sh
fi

if step skins "the skins"; then
  check "skins" bash scripts/check-skins.sh
fi

if step contrast "the contrast"; then
  # The skins check says every skin sets every variable. This one says the values are legible: text against the
  # surface it actually sits on, in all twenty one palettes, with every rgba lift composited first.
  check "contrast" node scripts/check-contrast.js internal/api/web/css
fi

if step opencode "the opencode plugin"; then
  # The plugin is the opencode runner's permission gate, and it runs inside opencode where no Go test reaches. Its
  # child_process is mocked, so this needs no atrium and no opencode.
  check "opencode plugin" node --test --experimental-test-module-mocks scripts/opencode/atrium.test.mjs
fi

if step release "what the release refuses"; then
  # The refusals, not the release: `cut-release.sh --preflight` against a throwaway repository. No network, builds
  # nothing, takes a second.
  check "release refusals" bash scripts/check-release.sh
fi

if step parse "the packaging scripts parse"; then
  # A packaging script is run once, by a person, on the day it matters. A syntax error in one is found at exactly the
  # wrong moment, so the shell is asked to parse without running.
  for f in packaging/postinstall.sh packaging/preremove.sh \
           packaging/macos/scripts/postinstall \
           scripts/atrium-service.sh scripts/package-linux.sh \
           scripts/package-macos.sh \
           scripts/release.sh scripts/publish-release.sh \
           scripts/cut-release.sh scripts/check-release.sh \
           scripts/ci.sh scripts/ci-linux.sh scripts/test.sh \
           scripts/test-time-guard.sh scripts/test-time-guard-test.sh; do
    if ! bash -n "$f"; then
      echo "FAILED: $f does not parse" >&2
      mark_fail
    fi
  done
  echo "ok: shell scripts parse"
fi

if step powershell "the PowerShell scripts parse"; then
  # Windows job only. pwsh is never started off Windows, even where one is installed.
  if [ "$os" = windows ] && command -v pwsh >/dev/null 2>&1; then
    check "powershell" pwsh -NoProfile -File scripts/check-powershell.ps1
  elif [ "$os" = windows ]; then
    echo "FAILED: no pwsh on PATH in the windows job." >&2
    mark_fail
  else
    echo "skipped: not the Windows job."
  fi
fi

if step ps-tests "the PowerShell tests"; then
  # The test-*.ps1 scripts, on Windows only. test-groups.ps1 runs the Go suite in groups and test-touched.ps1 runs what a
  # diff touched, so neither is a test of its own.
  if [ "$os" = windows ] && command -v pwsh >/dev/null 2>&1; then
    for t in scripts/test-*.ps1; do
      case "$(basename "$t")" in test-groups.ps1|test-touched.ps1) continue ;; esac
      check "$(basename "$t")" pwsh -NoProfile -File "$t"
    done
  elif [ "$os" = windows ]; then
    echo "FAILED: no pwsh on PATH in the windows job." >&2
    mark_fail
  else
    echo "skipped: not the Windows job."
  fi
fi

if step clean-tree "the tree is as it was"; then
  # Nothing the run did may show in git: a test that rewrites a tracked file or leaves a new one behind is a bug.
  git_after="$(git status --porcelain --untracked-files=all 2>/dev/null | grep -v ' build\.claude/' | sort)"
  if [ "$git_before" = "$git_after" ]; then
    echo "ok: git status is unchanged"
  else
    echo "FAILED: the run changed the working tree:" >&2
    diff <(echo "$git_before") <(echo "$git_after") >&2
    mark_fail
  fi
fi

echo
write_summary
if [ "$fail" != "0" ]; then
  echo "ci failed. see above." >&2
  exit 1
fi
echo "everything passed."
