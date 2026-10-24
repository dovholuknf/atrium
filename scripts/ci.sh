#!/usr/bin/env bash
# Everything CI runs, in one file you can run yourself. The same file runs on linux, macos and windows (Git Bash).
#
#   bash scripts/ci.sh                  every step
#   bash scripts/ci.sh --only go-test   one step, or several: --only gofmt,go-vet
#   bash scripts/ci.sh --list           the step names
#   bash scripts/ci.sh --group board    one group of steps, as one workflow job runs it (--list-groups names them)
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
# The board groups (board, board-*) need no Go and the job does not install it. go-test groups call node for the flaky list.
#
# THE REPORT. build.claude/ci/summary.md says what ran, how long each step took, which tests failed with the end of
# their output, the slowest tests, and the line that reproduces the run. The workflow copies it to the run's summary.
#
# FLAKY_REPEAT=N runs the test steps of flaky-go and flaky-board (and flaky-board-1|2) N times in a row, 1 when unset, and
# ci.yml sets it once for the three flaky jobs. Every pass runs even after one fails, each failing row carries "[pass i/N]",
# and a failure in any pass fails the job. The clean-tree, toolchain and install steps are not repeated.
#
# A flaky test is a failure. There is no rerun and no quarantine list. The one exception is the board's pixel and
# timing units on scripts/board-visual-units.txt: the board-visual step runs them and a failure there is reported but
# never fails the exit status, in whichever group it runs. They are run and reported, never dropped.
# The sharded board runner alone retries a failed unit once, alone. A unit on scripts/flaky-suite.json that then passes is
# marked PASSED ON RETRY in the log and the summary, never reads as a clean pass, and does not fail the job. Any other
# unit that passes on retry is marked LOAD, listed the same way and does not fail the job either.

set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$here"

all_steps="machine toolchain flaky-go flaky-board gofmt go-vet go-build go-test go-race time-guard govulncheck board board-visual skins contrast opencode release parse powershell ps-tests clean-tree"

# GROUPS. The workflow runs one job per group and platform, so the Go tests, the Go checks and the board run side by
# side and a run takes as long as its slowest group. time-guard reads go-test's stream, so they share a group, and each
# group ends with clean-tree, which only looks at what that job did.
#
# THE FLAKY SUITE RUNS FIRST. scripts/flaky-suite.json lists every test known to fail now and then, and the flaky groups
# run only those, with no quarantine: a failure fails the job, and a board unit gets one retry (a go test gets none), so
# a listed unit that fails and then passes is marked PASSED ON RETRY and does not fail the job. Every other step skips
# them, so a test runs in exactly one place, and the workflow starts these first with no `needs`, and each platform's
# check jobs wait on that platform's flaky job, so the tests known to fail answer fast and the two groups never share
# runners.
#
# THE FLAKY SUITE IS TWO JOBS PER PLATFORM. flaky-go is the flaky go tests: it needs Go and no node. flaky-board is the
# flaky board units and the flaky check scripts (they drive playwright) on one shard so no unit loads another: it needs
# node and no Go. flaky-board-1 and flaky-board-2 are its two halves by weight (board_units says how), kept for local runs.
#
# THE GO TESTS ARE TWO JOBS ON WINDOWS. go-test-1 and go-test-2 are the go-test step over half the suite each, split by
# wall time (the go-test step says how), so a run waits on half of internal/daemon and not all of it. go-test is the
# whole suite in one job, which the faster macos and linux runners use.
#
# THE BOARD IS TWO JOBS TOO. board-1 and board-2 are the board step over half the gating units each (board_units
# says how they are halved), and board is all of them in one job.
#
# THE SMALL CHECKS RIDE WITH go-checks, the shortest job, and not with the board, the longest: the skins, the
# contrast, the opencode plugin, the release refusals and the script parses have no platform in them and run once, on
# the linux job (linux_job_only). The PowerShell parse and tests are Windows only and skip themselves elsewhere.
#
# THE MERGED GROUPS use fewer runners. go-checks-visual is go-checks and board-visual in one job, go-all is go-test,
# go-checks and board-visual in one. board-visual never fails the exit status (soft_nfail), so merging it hides no
# go-checks failure and no go-checks failure hides it.
group_list="flaky-go flaky-board flaky-board-1 flaky-board-2 go-all go-test go-test-1 go-test-2 go-checks go-checks-visual board board-1 board-2 board-visual board-1-visual build-board-1 race-board-2"
group_steps() {
  case "$1" in
    flaky-go) echo "flaky-go,clean-tree" ;;
    flaky-board|flaky-board-1|flaky-board-2) echo "flaky-board,clean-tree" ;;
    go-test) echo "go-build,go-test,time-guard,clean-tree" ;;
    go-test-1|go-test-2) echo "go-test,time-guard,clean-tree" ;;
    go-checks) echo "gofmt,go-vet,go-race,govulncheck,skins,contrast,opencode,release,parse,powershell,ps-tests,clean-tree" ;;
    go-checks-visual) echo "gofmt,go-vet,go-race,govulncheck,skins,contrast,opencode,release,parse,powershell,ps-tests,board-visual,clean-tree" ;;
    go-all) echo "go-build,go-test,time-guard,gofmt,go-vet,govulncheck,skins,contrast,opencode,release,parse,powershell,ps-tests,clean-tree" ;;
    board-visual) echo "board-visual,clean-tree" ;;
    board|board-1|board-2) echo "board,clean-tree" ;;
    board-1-visual) echo "board,board-visual,clean-tree" ;;
    build-board-1) echo "go-build,board,clean-tree" ;;
    race-board-2) echo "go-race,board,clean-tree" ;;
    *) return 1 ;;
  esac
}
# A group name ending in -1 or -2 picks that part of a step that has parts. Empty is the whole step.
part_of() { case "$1" in *-1|*-1-visual) echo 1 ;; *-2) echo 2 ;; esac; }

only=""; group=""; part=""
while [ $# -gt 0 ]; do
  case "$1" in
    --only) [ $# -ge 2 ] && [ -n "$2" ] || { echo "--only needs a step name. steps: $all_steps" >&2; exit 2; }; only="$2"; shift 2 ;;
    --only=*) only="${1#--only=}"; shift ;;
    --group) [ $# -ge 2 ] && only="$(group_steps "$2")" || { echo "--group needs one of: $group_list" >&2; exit 2; }; group="$2"; part="$(part_of "$2")"; shift 2 ;;
    --list) echo $all_steps | tr ' ' '\n'; exit 0 ;;
    --list-groups) for g in $group_list; do echo "$g: $(group_steps "$g")"; done; exit 0 ;;
    -h|--help) sed -n '2,8p' "$0"; exit 0 ;;
    *) echo "usage: scripts/ci.sh [--only step[,step]] [--group name] [--list] [--list-groups]" >&2; exit 2 ;;
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

fail=0; nfail=0; soft_nfail=0
mark_fail() { fail=1; nfail=$((nfail + 1)); }
# Reported in the log, the summary and failing.tsv, but not counted against the job: only board-visual's failures.
hard_failed() { [ $((nfail - soft_nfail)) -gt 0 ]; }
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
cur=""; cur_start=0; cur_fail=0; cur_ran="-"; cur_failed="-"
failing_tsv="$artefacts/failing.tsv"
: > "$failing_tsv"
# What a step counts, for the Steps table: tests run and tests failed. A step that runs no tests leaves both as "-".
# Each failed test also goes to failing.tsv as step, name, first error line, which the summary lists under its own heading.
note_counts() { cur_ran="$1"; cur_failed="$2"; }
note_failing() { printf '%s\t%s\t%s\n' "$1" "$2" "${pass_tag:-}$3" >> "$failing_tsv"; }
# FLAKY_REPEAT passes of each flaky group's test steps, 1 when unset. pass_tag prefixes every failing row of a pass.
flaky_repeat="${FLAKY_REPEAT:-1}"
case "$flaky_repeat" in ''|*[!0-9]*|0) echo "FLAKY_REPEAT must be a whole number of 1 or more, not '$flaky_repeat'" >&2; exit 2 ;; esac
pass_tag=""; pass_sfx=""
# flaky_pass <i>: the log header of a pass, and the tag and file suffix it uses. Only a repeated run says "pass".
flaky_pass() {
  pass_tag=""; pass_sfx=""
  [ "$flaky_repeat" -gt 1 ] || return 0
  pass_tag="[pass $1/$flaky_repeat] "
  [ "$1" -gt 1 ] && pass_sfx="-p$1"
  echo; echo "--- flaky pass $1 of $flaky_repeat  [$(date -u +%H:%M:%S)]"
}
# A board unit that failed in its shard and passed on retry: a pass, never a clean one. retried.tsv is step, unit, first
# error line, both timings, and the summary lists it under its own heading.
retried_tsv="$artefacts/retried.tsv"
: > "$retried_tsv"
note_retried() { printf '%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$4" >> "$retried_tsv"; }
close_step() {
  [ -z "$cur" ] && return
  local res=ok
  [ "$nfail" != "$cur_fail" ] && res=FAILED
  printf '%s\t%s\t%s\t%s\t%s\n' "$cur" "$(( $(date +%s) - cur_start ))" "$res" "$cur_ran" "$cur_failed" >> "$steps_tsv"
  cur=""; cur_ran="-"; cur_failed="-"
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
# A check with no platform in it runs once per run, on the linux job. A laptop runs it wherever it is.
linux_job_only() {
  if [ -n "${GITHUB_ACTIONS:-}" ] && [ "$os" != linux ]; then echo "skipped: runs once, on the linux job."; return 0; fi
  return 1
}
# THE BOARD'S UNITS, split by scripts/board-visual-units.txt (one unit a line, # for a comment). `board_units gating`
# is every unit not on the list, `board_units visual` the ones on it, both comma separated. A name on the list that
# the suite no longer has is an error, so the list cannot rot.
#
# `board_units gating-1` and `gating-2` are the gating units in two halves of about equal time, for the two board
# jobs (`flaky-1` and `flaky-2` are the flaky board units the same way, and `flaky-scripts-1` and `-2` the flaky check
# scripts that go with each, weighed at a measured 113 s unless the weight file has "script:<name>"): the harness's pin
# groups stay whole, and each goes, longest first, to the half with less so far, weighed by
# scripts/board-suite-weights.json (a unit it lacks weighs the median). The same arithmetic as test-board-sharded.js
# and deterministic, so the two jobs never both run a unit and never both leave one out.
board_units() {
  HEADLESS_LIST=1 node scripts/test-board-headless.js | node -e '
    const fs = require("fs");
    const lines = fs.readFileSync(0, "utf8").split("\n").reverse();
    const json = lines.find(l => l.startsWith("{"));
    if (!json) { process.stderr.write("the harness listed no units\n"); process.exit(1); }
    const list = JSON.parse(json), all = list.units;
    const visual = fs.readFileSync("scripts/board-visual-units.txt", "utf8").split("\n")
      .map(l => l.replace(/#.*/, "").trim()).filter(Boolean);
    const gone = visual.filter(u => !all.includes(u));
    if (gone.length) { process.stderr.write("board-visual-units.txt names units the suite lacks: " + gone.join(" ") + "\n"); process.exit(1); }
    const entries = JSON.parse(fs.readFileSync("scripts/flaky-suite.json", "utf8")).entries;
    const flaky = entries.filter(e => e.kind === "board").map(e => e.name);
    const which = process.argv[1];
    const isFlaky = /^flaky(-scripts)?(-[12])?$/.test(which);
    const scripts = isFlaky ? entries.filter(e => e.kind === "script").map(e => e.name) : [];
    let want = isFlaky ? all.filter(u => flaky.includes(u))
      : which === "visual" ? visual.filter(u => !flaky.includes(u))
      : all.filter(u => !visual.includes(u) && !flaky.includes(u));
    const half = (which.match(/^(?:gating|flaky|flaky-scripts)-([12])$/) || [])[1];
    if (half) {
      const ms = (JSON.parse(fs.readFileSync("scripts/board-suite-weights.json", "utf8")).ms) || {};
      const known = Object.values(ms).filter(x => x > 0).sort((a, b) => a - b);
      const dflt = known.length ? known[Math.floor(known.length / 2)] : 1;
      const w = u => (ms[u] > 0 ? ms[u] : dflt);
      const pinOf = new Map();
      for (const [g, p] of Object.entries(list.pins || {})) for (const u of p.units) pinOf.set(u, g);
      const groups = [], byPin = new Map();
      for (const n of scripts) groups.push({ units: [], scripts: [n], weight: ms["script:" + n] > 0 ? ms["script:" + n] : 113000 });
      for (const u of want) {
        const g = pinOf.get(u);
        if (!g) { groups.push({ units: [u], weight: w(u) }); continue; }
        if (!byPin.has(g)) { const x = { units: [], weight: 0 }; byPin.set(g, x); groups.push(x); }
        byPin.get(g).units.push(u); byPin.get(g).weight += w(u);
      }
      const bins = [{ units: [], weight: 0 }, { units: [], weight: 0 }];
      for (const g of groups.slice().sort((a, b) => b.weight - a.weight)) {
        const b = bins[0].weight <= bins[1].weight ? bins[0] : bins[1];
        b.units.push(...g.units); b.weight += g.weight;
        if (g.scripts) b.scripts = (b.scripts || []).concat(g.scripts);
      }
      const at = new Map(all.map((u, i) => [u, i]));
      want = bins[half - 1].units.sort((a, b) => at.get(a) - at.get(b));
      scripts.splice(0, scripts.length, ...(bins[half - 1].scripts || []));
      process.stderr.write(which + ": " + want.length + " units, " + scripts.length + " scripts, " + Math.round(bins[half - 1].weight / 1000) + " s by the weight file\n");
    }
    process.stdout.write(/^flaky-scripts/.test(which) ? scripts.join(",") : want.join(","));
  ' "$1"
}
# What the headless browser on this machine is, for reading a board unit that passes elsewhere and fails here.
board_env_probe() {
  node scripts/board-env-probe.js 2>&1 | tee "$artefacts/$1"
}
# The merged report of a sharded run: one row per unit (unit, time, result), then FAILED sections each unit with its
# first error line indented under it. A unit that passed only on retry and is NOT on the flaky list fails the run too, so
# it is counted as failed. One that IS on the flaky list passes the run and is not counted as failed: it goes to
# retried.tsv, with its timings. The shards' own output, which the runner keeps on a failure or a retried pass and names
# on a "shard output:" line, is copied into the artifacts.
board_report() {
  local name="$1" log="$2" n_units n_bad n_checks dir
  n_units="$(awk '$3 ~ /^(PASS|PASSED|FAIL|NOT|LOAD|FLAKY)$/' "$log" | wc -l | tr -d ' ')"
  n_bad="$(awk '$3 ~ /^(FAIL|NOT)$/ || ($3 == "FLAKY" && $0 ~ /failed every try|failed, not retried/)' "$log" | wc -l | tr -d ' ')"
  n_checks="$(grep -c '^FAIL ' "$log" || true)"
  note_counts "$n_units" "$((n_bad + n_checks))"
  grep '^FAIL ' "$log" | while IFS= read -r line; do note_failing "$name" "(check)" "${line#FAIL }"; done
  awk '
    /^FAILED \(|^FAILED OUTSIDE/ { sec = ($0 ~ /OUTSIDE/) ? 2 : 1; unit = ""; next }
    /^[^ ]/ { sec = 0; next }
    sec == 1 && /^  [^ ]/ { unit = $1; got = 0; next }
    sec == 1 && /^    / && unit != "" && !got { e = $0; sub(/^ +/, "", e); sub(/^first try: /, "", e); printf "%s\t%s\n", unit, e; got = 1; next }
    sec == 2 && /^  [^ ]/ { e = $0; sub(/^ +/, "", e); printf "(outside any unit)\t%s\n", e }
  ' "$log" | while IFS=$'\t' read -r unit err; do note_failing "$name" "$unit" "$err"; done
  awk '
    /^PASSED ON RETRY, NOT A CLEAN PASS \(/ { sec = 1; load = 0; unit = ""; next }
    /^FAILED IN THE SHARDS, PASSED ALONE/ { sec = 1; load = 1; unit = ""; next }
    /^[^ ]/ { sec = 0; next }
    sec == 1 && /^  [^ ]/ { unit = $1; t = $0; if (load) t = "LOAD, not on the flaky list"; else { sub(/^[^[]*\[/, "", t); sub(/\]$/, "", t) } got = 0; next }
    sec == 1 && /^    / && unit != "" && !got { e = $0; sub(/^ +/, "", e); sub(/^first try: /, "", e); printf "%s\t%s\t%s\n", unit, e, t; got = 1 }
  ' "$log" | while IFS=$'\t' read -r unit err times; do note_retried "$name" "$unit" "$err" "$times"; done
  dir="$(sed -n 's/^shard output: //p' "$log" | tail -1)"
  if [ -n "$dir" ] && [ -d "$dir" ]; then cp -R "$dir" "$artefacts/$name-shards" || true; fi
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
  hard_failed && verdict="FAILED"
  [ "$verdict" = PASSED ] && [ "$soft_nfail" != 0 ] && verdict="PASSED (board-visual failed, which does not fail the job)"
  [ "$verdict" = PASSED ] && [ -s "$retried_tsv" ] && verdict="PASSED, BUT $(wc -l < "$retried_tsv" | tr -d ' ') BOARD UNIT(S) PASSED ONLY ON RETRY (not clean, see below)"
  {
    echo "# ci on $os: $verdict"
    echo
    echo "- $machine_note"
    echo "- commit: $(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
    echo
    echo "## Steps"
    echo
    echo "| step | seconds | result | tests run | tests failed |"
    echo "|---|---|---|---|---|"
    awk -F'\t' '{printf "| %s | %s | %s | %s | %s |\n", $1, $2, $3, $4, $5}' "$steps_tsv"
    if [ -s "$retried_tsv" ]; then
      echo
      echo "## PASSED ON RETRY, NOT CLEAN PASSES (they do not fail the job)"
      echo
      echo "Each failed in its shard and passed alone on the retry, so it is slow or stalls under load. A LOAD row is not on scripts/flaky-suite.json. Fix the wait, or list it with its reason."
      echo
      echo "| step | unit | timings | first-try error |"
      echo "|---|---|---|---|"
      awk -F'\t' '{gsub(/\|/, "\\\\|", $3); printf "| %s | %s | %s | %s |\n", $1, $2, $4, $3}' "$retried_tsv"
    fi
    if [ -s "$failing_tsv" ]; then
      echo
      echo "## Failing tests"
      echo
      echo "| step | test | first error |"
      echo "|---|---|---|"
      awk -F'\t' '{gsub(/\|/, "\\|", $3); printf "| %s | %s | %s |\n", $1, $2, $3}' "$failing_tsv"
    fi
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
    if grep -q "^WARN " "$artefacts/time-guard.log" 2>/dev/null; then
      echo
      echo "## Time warnings (they do not fail the job)"
      echo
      echo '```'
      grep "^WARN \|^time guard:" "$artefacts/time-guard.log" | cut -c1-200
      echo '```'
    fi
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
    [ -n "$group" ] && echo "bash scripts/ci.sh --group $group"
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
# Defender on the hosted Windows runner: whether it scans, what a never-seen executable pays, and the folders the
# tests write binaries to excluded, with the cost measured again after. Only on GitHub's runner, which is thrown
# away: a developer's machine is never changed by a CI script. It never fails the step. scripts/ci-defender.ps1.
if [ "$os" = windows ] && [ -n "${GITHUB_ACTIONS:-}" ] && command -v pwsh >/dev/null 2>&1; then
  pwsh -NoProfile -File scripts/ci-defender.ps1 || echo "ci-defender: did not run (exit $?)"
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
case "$group" in flaky-board|flaky-board-*|board|board-*) ;; *)
if [ "$have_go" != "$want_go" ]; then
  echo "go is '${have_go:-missing}', go.mod says $want_go" >&2; tool_bad=1
fi ;;
esac
if [ "$group" = flaky-go ]; then
  :
elif [ -z "$have_node" ]; then
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
if { want board || want board-visual || want flaky-board; } && command -v npm >/dev/null 2>&1; then
  if [ -f package-lock.json ]; then
    check "npm ci" npm ci --no-audit --no-fund
  else
    check "npm install" npm install --no-audit --no-fund
  fi
  deps=""
  if [ "$os" = linux ] && [ -n "${CI:-}" ]; then deps="--with-deps"; fi
  have_browser=1
  while read -r dir; do [ -d "$dir" ] || have_browser=0; done < <(npx --no-install playwright install --dry-run chromium 2>/dev/null | sed -n 's/^ *Install location: *//p')
  if [ "$have_browser" = 1 ]; then
    echo "ok: playwright browser already installed"
  else
    check "playwright browser" npx --no-install playwright install $deps chromium
  fi
fi

if step flaky-go "the flaky go tests"; then
  # No retry. The names are checked first because an empty -run would run everything.
  if check "flaky suite names" node scripts/flaky-suite.js check; then
    flaky_n=0; flaky_bad=0
    flaky_pkgs="$(node scripts/flaky-suite.js go-pkgs)"
    flaky_re="$(node scripts/flaky-suite.js go-regex)"
    if [ -n "$flaky_re" ]; then
      for ((pass = 1; pass <= flaky_repeat; pass++)); do
        flaky_pass "$pass"
        gdir="$artefacts/go-flaky$pass_sfx"
        mkdir -p "$gdir"
        bash scripts/test.sh full -json -timeout 20m -p "$cpus" -run "$flaky_re" $flaky_pkgs 2>&1         | tee "$gdir/stream.json" | go run scripts/ci-report.go -dir "$gdir"
        codes=("${PIPESTATUS[@]}")
        if [ "${codes[0]}" != 0 ] || [ "${codes[2]}" != 0 ]; then
          echo "FAILED: flaky go tests${pass_tag:+ ${pass_tag% }}" >&2; mark_fail
        else
          echo "ok: flaky go tests${pass_tag:+ ${pass_tag% }}"
        fi
        if [ -f "$gdir/go-test.counts" ]; then
          read -r n_ran n_failed < "$gdir/go-test.counts"
          flaky_n=$((flaky_n + n_ran)); flaky_bad=$((flaky_bad + n_failed))
          while IFS=$'	' read -r _ name err; do [ -n "$name" ] && note_failing flaky-go "$name" "$err"; done < "$gdir/go-test.failing.tsv"
        fi
        note_counts "$flaky_n" "$flaky_bad"
      done
      pass_tag=""
    fi
  fi
fi

if step flaky-board "the flaky board units and scripts"; then
  # A board unit gets one retry, alone, and a pass on it is marked PASSED ON RETRY. A check script gets none.
  if check "flaky suite names" node scripts/flaky-suite.js check; then
    flaky_n=0; flaky_bad=0
    flaky_units="$(board_units "flaky${part:+-$part}")"
    flaky_scripts=",$(board_units "flaky-scripts${part:+-$part}"),"
    flaky_script_list="$(node scripts/flaky-suite.js scripts)"
    shards=1
    if [ -n "$flaky_units" ]; then board_env_probe flaky-env.txt; fi
    for ((pass = 1; pass <= flaky_repeat; pass++)); do
      flaky_pass "$pass"
      if [ -n "$flaky_units" ]; then
        # One shard: the units run one after another, so none of them runs under another's load.
        board_log="$artefacts/flaky-board$pass_sfx.log"
        flaky_board_run() {
          env FULL_SUITE=1 ATRIUM_SUITE_LOCAL=1 BOARD_SHARDS="$shards"           node scripts/test-board-sharded.js --local --units "$flaky_units" 2>&1 | tee "$board_log"
          return "${PIPESTATUS[0]}"
        }
        check "flaky board units${pass_tag:+ ${pass_tag% }}" flaky_board_run
        board_report flaky-board "$board_log"
        flaky_n=$((flaky_n + cur_ran)); flaky_bad=$((flaky_bad + cur_failed))
        note_counts "$flaky_n" "$flaky_bad"
      fi
      while IFS=$'	' read -r script_name script_path; do
        [ -n "$script_name" ] || continue
        case "$flaky_scripts" in *",$script_name,"*) ;; *) continue ;; esac
        flaky_n=$((flaky_n + 1))
        if ! check "flaky script $script_name${pass_tag:+ ${pass_tag% }}" node "$script_path"; then
          flaky_bad=$((flaky_bad + 1)); note_failing flaky-board "$script_name" "(script) $script_path failed"
        fi
        note_counts "$flaky_n" "$flaky_bad"
      done <<< "$flaky_script_list"
    done
    pass_tag=""
  fi
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
  check "go vet" go vet -tags integration $(go list -m -f '{{.Path}}/...')
fi

if step go-build "go build"; then
  # `-o build.claude/` even though `./...` writes nothing anybody keeps. Go builds in this repository land in
  # build.claude/ and never in the root, and there is a hook that enforces it on a developer's machine.
  check "go build" go build -o build.claude/ $(go list -m -f '{{.Path}}/...')
  # No test assigns a package variable: a hook, delay, limit or clock belongs on the struct that uses it.
  check "test globals" go run scripts/check-test-globals.go
  # No test gains a flat wait: spin on a condition, or mark the line `// wait: <why>`.
  check "test waits" go run scripts/check-test-waits.go
  # No package imports one the layer table in the file does not allow. See docs/design/go-modules.md.
  check "import layers" go run scripts/check-imports.go
  # Each module builds alone, and the workspace's pins have not drifted. See scripts/check-modules.sh.
  check "go modules" bash scripts/check-modules.sh
  check "flaky suite names" node scripts/flaky-suite.js check
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
  # go_test_run <label> <go test args>: the stream kept whole in <label>.json, and through the reporter live.
  go_test_run() {
    local label="$1" dir; shift
    dir="$artefacts/go-test.$label"; mkdir -p "$dir"
    bash scripts/test.sh full -json -timeout 20m "$@" 2>&1 | tee "$dir/stream.json" | go run scripts/ci-report.go -dir "$dir"
    local codes=("${PIPESTATUS[@]}")
    [ "${codes[0]}" = "0" ] && [ "${codes[2]}" = "0" ]
  }
  flaky_skip="$(node scripts/flaky-suite.js go-regex)"
  skip_flaky=(); [ -n "$flaky_skip" ] && skip_flaky=(-skip "$flaky_skip")
  if [ -z "$part" ]; then
    go_test_run all -p "$cpus" ${skip_flaky[@]+"${skip_flaky[@]}"} $(go list -m -f '{{.Path}}/...'); go_ok=$?
    mv "$artefacts/go-test.all/stream.json" "$artefacts/go-test.json"
    mv "$artefacts"/go-test.all/* "$artefacts/" 2>/dev/null
  else
    # THE TWO PARTS, each about half the suite's wall time on a windows-latest runner (run 39040667475, -p 4):
    # internal/daemon was 480 of the 1480 package seconds, so no split by package halves anything, and daemon is cut
    # by test name instead. Part 1 is daemon's TestA to TestF (51% of its seconds on windows, 44% on linux, 42% on
    # macos) with gitsync, api, cli and three small packages. Part 2 is the rest of daemon with every other package,
    # so a new package lands in part 2 and none is left out. go test takes one -run for every package named, so a
    # part is two invocations side by side: daemon's one binary and -p 3 of the rest, the four binaries a whole run has.
    daemon="./internal/daemon"; cut='^Test[A-F]'
    part1="./internal/gitsync ./internal/api ./internal/cli ./internal/ptyhost ./internal/deployready ./internal/roomspec"
    if [ "$part" = 1 ]; then
      rest="$part1"
      go_test_run daemon -run "$cut" ${skip_flaky[@]+"${skip_flaky[@]}"} "$daemon" & daemon_pid=$!
    else
      mod="$(go list -m)"
      rest="$(go list -tags integration,git $(go list -m -f '{{.Path}}/...') | grep -v -x -F -f <(for p in $daemon $part1; do echo "$mod/${p#./}"; done) | tr '\n' ' ')"
      go_test_run daemon -skip "$cut${flaky_skip:+|$flaky_skip}" "$daemon" & daemon_pid=$!
    fi
    echo "part $part: daemon $([ "$part" = 1 ] && echo -run || echo -skip) '$cut', then: $rest"
    go_test_run rest -p $((cpus - 1)) ${skip_flaky[@]+"${skip_flaky[@]}"} $rest; rest_ok=$?
    wait "$daemon_pid"; daemon_ok=$?
    go_ok=$(( rest_ok || daemon_ok ))
    # The merged stream is what time-guard reads and the files the summary reads come from, so the reporter runs
    # once more over it, quietly: its text is in go-test.report.txt. The parts' own folders are then redundant.
    cat "$artefacts/go-test.daemon/stream.json" "$artefacts/go-test.rest/stream.json" > "$artefacts/go-test.json"
    go run scripts/ci-report.go -dir "$artefacts" < "$artefacts/go-test.json" > "$artefacts/go-test.report.txt" 2>&1 || true
  fi
  rm -rf "$artefacts"/go-test.all "$artefacts"/go-test.daemon "$artefacts"/go-test.rest
  echo "go test took $(( $(date +%s) - started ))s"
  if [ "$go_ok" = "0" ]; then
    echo "ok: go test"
  else
    echo "FAILED: go test (see the FAIL lines above)" >&2
    mark_fail
  fi
  if [ -f "$artefacts/go-test.counts" ]; then
    read -r n_ran n_failed < "$artefacts/go-test.counts"
    note_counts "$n_ran" "$n_failed"
    while IFS=$'\t' read -r _ name err; do [ -n "$name" ] && note_failing go-test "$name" "$err"; done < "$artefacts/go-test.failing.tsv"
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
    # Through the same reporter as go-test, into its own folder so its files do not mix with that step's.
    race_dir="$artefacts/race"; mkdir -p "$race_dir"
    go test -race -json -count=1 -p "$cpus" -timeout 20m $(go list -m -f '{{.Path}}/...') 2>&1 | go run scripts/ci-report.go -dir "$race_dir"
    codes=("${PIPESTATUS[@]}")
    if [ "${codes[0]}" = "0" ] && [ "${codes[1]}" = "0" ]; then
      echo "ok: go test -race"
    else
      echo "FAILED: go test -race (go test exit ${codes[0]}, report exit ${codes[1]})" >&2
      mark_fail
    fi
    if [ -f "$race_dir/go-test.counts" ]; then
      read -r n_ran n_failed < "$race_dir/go-test.counts"
      note_counts "$n_ran" "$n_failed"
      while IFS=$'\t' read -r _ name err; do [ -n "$name" ] && note_failing go-race "$name" "$err"; done < "$race_dir/go-test.failing.tsv"
    fi
  fi
fi

if step time-guard "test time guard"; then
  # A package over 60s or a test over 5s is a WARN line, or over its ceiling in scripts/test-time-allow.txt. Speed is not
  # a functional check, so the guard exits 0 and the job stays green. Reads the stream kept above.
  # The self-test and the parser's Go tests run first, so a broken guard is not taken for a fast suite.
  check "time guard self-test" bash scripts/test-time-guard-test.sh
  check "time guard parser" go test scripts/ci-report.go scripts/ci-report_test.go
  # A hosted runner is about twice as slow as the box the ceilings were measured on (run 38269244777: 12 over, none by
  # more than 2.2x), so it gets twice the room. A developer's machine keeps the real limits.
  if [ -n "${GITHUB_ACTIONS:-}" ]; then export TEST_TIME_SCALE=2; fi
  if [ -f "$artefacts/go-test.json" ]; then
    time_guard_run() { bash scripts/test-time-guard.sh "$artefacts/go-test.json" 2>&1 | tee "$artefacts/time-guard.log"; return "${PIPESTATUS[0]}"; }
    check "test time guard" time_guard_run
  else
    echo "FAILED: no $artefacts/go-test.json to read. run the go-test step first." >&2
    mark_fail
  fi
fi

if step govulncheck "govulncheck"; then
  # A vulnerability in the standard library or a dependency is found by this and not by an audit. Pinned so a new
  # govulncheck cannot redden a build by itself, and the database it reads is always the current one. Needs the network.
  check "govulncheck" go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 $(go list -m -f '{{.Path}}/...')
fi

if step board "the board"; then
  # Includes the headless browser run. Any screenshots it takes go under build.claude/ and are not compared.
  # The whole suite, so it says so: check-board.sh refuses a full run without FULL_SUITE=1.
  # SHARDED: the serial headless run was 25 to 30 minutes per OS. test-board-sharded.js runs the same units as parallel
  # shards (one per core of the runner, 2 to 16) and prints one merged report. ATRIUM_SUITE_LOCAL=1 keeps it on the
  # runner, never dispatched to another room.
  cores="$( (nproc || sysctl -n hw.ncpu) 2>/dev/null)"
  [ -n "$cores" ] || cores="${NUMBER_OF_PROCESSORS:-4}"
  shards=$cores; [ "$shards" -ge 2 ] || shards=2; [ "$shards" -le 16 ] || shards=16
  board_log="$artefacts/board.log"
  # THE BOARD IS TWO RUNS. The units on scripts/board-visual-units.txt measure pixels and wait on timers, so a runner's
  # fonts or load fail them and no code change does. They run in the board-visual group, which never reddens the run.
  # This step runs every other unit, or half of them in the board-1 and board-2 groups (board_units).
  gating="$(board_units "gating${part:+-$part}")"
  board_run() {
    env FULL_SUITE=1 HEADLESS_SHARDED=1 ATRIUM_SUITE_LOCAL=1 SKIP_DRAWER_CHECK=1 BOARD_SHARDS="$shards" HEADLESS_UNITS="$gating" \
      bash scripts/check-board.sh 2>&1 | tee "$board_log"
    return "${PIPESTATUS[0]}"
  }
  board_env_probe board-env.txt
  check "board" board_run
  board_report board "$board_log"
fi

if step board-visual "the board, pixel and timing units"; then
  # Same runner, only the units on the list, with the probe's facts in the artifacts so a failure here can be read
  # against the machine. CR_CONTRAST adds the colour and the background it measured to each contrast failure.
  cores="$( (nproc || sysctl -n hw.ncpu) 2>/dev/null)"
  [ -n "$cores" ] || cores="${NUMBER_OF_PROCESSORS:-4}"
  shards=$cores; [ "$shards" -ge 2 ] || shards=2; [ "$shards" -le 16 ] || shards=16
  board_log="$artefacts/board-visual.log"
  board_visual_run() {
    env FULL_SUITE=1 ATRIUM_SUITE_LOCAL=1 BOARD_SHARDS="$shards" CR_CONTRAST=1 \
      node scripts/test-board-sharded.js --local --units "$(board_units visual)" 2>&1 | tee "$board_log"
    return "${PIPESTATUS[0]}"
  }
  board_env_probe board-visual-env.txt
  nfail_before=$nfail
  check "board (pixel and timing units)" board_visual_run
  soft_nfail=$((nfail - nfail_before))
  board_report board-visual "$board_log"
fi

if step skins "the skins"; then
  linux_job_only || check "skins" bash scripts/check-skins.sh
fi

if step contrast "the contrast"; then
  # The skins check says every skin sets every variable. This one says the values are legible: text against the
  # surface it actually sits on, in all twenty one palettes, with every rgba lift composited first.
  linux_job_only || check "contrast" node scripts/check-contrast.js internal/api/web/css
fi

if step opencode "the opencode plugin"; then
  # The plugin is the opencode runner's permission gate, and it runs inside opencode where no Go test reaches. Its
  # child_process is mocked, so this needs no atrium and no opencode.
  linux_job_only || check "opencode plugin" node --test --experimental-test-module-mocks scripts/opencode/atrium.test.mjs
fi

if step release "what the release refuses"; then
  # The refusals, not the release: `cut-release.sh --preflight` against a throwaway repository. No network, builds
  # nothing, takes a second.
  linux_job_only || check "release refusals" bash scripts/check-release.sh
fi

if step parse "the packaging scripts parse"; then
  # A packaging script is run once, by a person, on the day it matters. A syntax error in one is found at exactly the
  # wrong moment, so the shell is asked to parse without running.
  if ! linux_job_only; then
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
    # Each script prints `ok   <check>` or `FAIL <check>` followed by want and got lines. Those are the tests here.
    ps_log="$artefacts/ps-tests.log"
    ps_run() { pwsh -NoProfile -File "$1" 2>&1 | tee -a "$ps_log"; return "${PIPESTATUS[0]}"; }
    for t in scripts/test-*.ps1; do
      case "$(basename "$t")" in test-groups.ps1|test-touched.ps1) continue ;; esac
      check "$(basename "$t")" ps_run "$t"
    done
    note_counts "$(grep -cE '^(ok  |FAIL )' "$ps_log" || true)" "$(grep -c '^FAIL ' "$ps_log" || true)"
    awk '/^FAIL / { n = substr($0, 6); getline w; sub(/^ +/, "", w); printf "%s\t%s\n", n, w }' "$ps_log" \
      | while IFS=$'\t' read -r name err; do note_failing ps-tests "$name" "$err"; done
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
if hard_failed; then
  echo "ci failed. see above." >&2
  exit 1
fi
if [ "$soft_nfail" != 0 ]; then echo "board-visual failed and does not fail the job. see above." >&2; fi
echo "everything passed."
