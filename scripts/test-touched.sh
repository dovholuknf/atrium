#!/usr/bin/env bash
# Test what this branch touched, and nothing else. What a worker runs before handing back.
#
#   bash scripts/test-touched.sh                 diff against claude/main
#   bash scripts/test-touched.sh --base REF      diff against REF (its merge base with HEAD)
#   bash scripts/test-touched.sh --plan          print the plan and stop
#   bash scripts/test-touched.sh --budget MS     headless unit time to allow, from board-suite-weights.json (150000)
#
# THE DIFF is everything between the merge base and the working tree: commits, staged, unstaged, and untracked files.
# THE PLAN comes from scripts/test-touched-plan.js, which says WHY it picked each item:
#   - DECLARED FIRST: scripts/test-buckets.json says which bucket owns a changed file. The plan names the bucket, and a
#     board bucket's units and node tests are the candidates. A file in no bucket falls back to the inference below
#   - go test on each changed Go package, through scripts/test.sh, with -tags integration only when an integration
#     file (*_integration_test.go, or one carrying that build tag) changed
#   - the headless board units read off the code for each changed internal/api/web file, plus bootClean, run through
#     scripts/test-board-sharded.js --local --units (which sets HEADLESS_UNITS per shard). A web file that maps to no
#     unit gets bootClean only, and a warning
#   - THE FLAKY SUITE FIRST: a changed package or unit that scripts/flaky-suite.json lists runs ahead of the rest, once
#     and with no retry, and the later run skips it. The answer from the bad tests comes before the long run
#   - gofmt -l over the changed .go files, failing and naming any it lists
#   - the node scripts/test-*.js whose subject changed, a syntax check of changed scripts
#
# THE WHOLE SUITE IS NOT THIS SCRIPT'S JOB. It runs after landing, by whoever lands, with FULL_SUITE=1. A red there is
# a follow-up, not a reason to run it all before handing back.
#
# Exits non-zero when anything it ran failed. A headless run that skipped itself (no playwright) is said loudly and
# does not fail the run.
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$here"

base="claude/main"; plan_only=0; budget=""
while [ $# -gt 0 ]; do
  case "$1" in
    --base) [ $# -ge 2 ] || { echo "--base needs a ref" >&2; exit 2; }; base="$2"; shift 2 ;;
    --base=*) base="${1#--base=}"; shift ;;
    --plan) plan_only=1; shift ;;
    --budget) [ $# -ge 2 ] || { echo "--budget needs milliseconds" >&2; exit 2; }; budget="$2"; shift 2 ;;
    -h|--help) sed -n '2,7p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown flag $1. see --help" >&2; exit 2 ;;
  esac
done

if ! command -v node >/dev/null 2>&1; then echo "test-touched: node is not on PATH, and the plan is made by node" >&2; exit 2; fi
if ! git rev-parse --verify --quiet "$base^{commit}" >/dev/null; then
  echo "test-touched: no ref $base here. pass --base REF (origin/main, say)" >&2; exit 2
fi
mb="$(git merge-base "$base" HEAD)" || { echo "test-touched: $base and HEAD share no history" >&2; exit 2; }

t0=$(date +%s)
files="$( { git diff --name-only "$mb"; git ls-files --others --exclude-standard; } | sort -u)"
# new-side line ranges of the headless harness, so a change inside one section runs that section's unit
harness_lines="$( { git diff -U0 "$mb" -- scripts/test-board-headless.js
  git ls-files --others --exclude-standard -- scripts/test-board-headless.js | while read -r f; do echo "@@ -0,0 +1,$(wc -l <"$f") @@"; done; } |
  sed -n 's/^@@ -[0-9,]* +\([0-9]*\)\(,\([0-9]*\)\)\{0,1\} @@.*/\1 \3/p' |
  awk '{ n = ($2 == "") ? 1 : $2; if (n == 0) n = 1; printf "%s%d-%d", (NR > 1 ? "," : ""), $1, $1 + n - 1 }')"

nfiles=$(printf '%s\n' "$files" | grep -c . || true)
echo "test-touched: base $base, merge base $(git rev-parse --short "$mb"), $nfiles changed file(s)"
plan_args=()
[ -n "$harness_lines" ] && plan_args+=(--harness-lines "$harness_lines")
[ -n "$budget" ] && plan_args+=(--budget "$budget")
actions="$(printf '%s\n' "$files" | node scripts/test-touched-plan.js ${plan_args[@]+"${plan_args[@]}"})" || {
  echo "test-touched: the planner failed" >&2; exit 2; }
if [ -z "$actions" ]; then echo "test-touched: nothing to run"; exit 0; fi
[ "$plan_only" = 1 ] && exit 0

fail=0; failed=(); skipped=()
step() {
  local label="$1"; shift
  local s=$(date +%s)
  echo "--- $label"
  "$@"
  local c=$?
  echo "--- $label: $([ $c = 0 ] && echo ok || echo FAILED) in $(( $(date +%s) - s ))s"
  if [ $c != 0 ]; then fail=1; failed+=("$label"); fi
  return 0
}

gopkgs=(); mode=unit; units=""; gofiles=(); govet=(); nodes=(); jschecks=(); shchecks=(); gobuild=0
while read -r kind rest; do
  case "$kind" in
    BUCKET) ;;   # which bucket owns what changed: said in the plan, nothing to run
    GO) gopkgs+=("$rest") ;;
    TAGS) mode=all ;;
    GOFILES) gofiles+=("$rest") ;;
    GOVET) govet+=("$rest") ;;
    GOBUILD) gobuild=1 ;;
    UNITS) units="$rest" ;;
    NODE) nodes+=("$rest") ;;
    JSCHECK) jschecks+=("$rest") ;;
    SHCHECK) shchecks+=("$rest") ;;
  esac
done <<<"$actions"

# Playwright, for the headless units and for the node checks that drive a browser too.
if [ -n "$units" ] || [ ${#nodes[@]} -gt 0 ]; then
  # Playwright: NODE_PATH, then this checkout, then any worktree beside it that has it, as merge-check.ps1 looks.
  if ! node -e 'require.resolve("playwright")' >/dev/null 2>&1 && ! node -e 'require.resolve("@playwright/test")' >/dev/null 2>&1; then
    for nm in "${ATRIUM_NODE_PATH:-}" "$here/node_modules" "$here"/../*/node_modules; do
      if [ -n "$nm" ] && { [ -d "$nm/playwright" ] || [ -d "$nm/@playwright/test" ]; }; then
        export NODE_PATH="$(cd "$nm" && pwd)"; echo "test-touched: playwright from $NODE_PATH"; break
      fi
    done
  fi
fi

# gofmt over the changed Go files: CI's gofmt check goes red on what go test does not mind
gofmt_changed() {
  local listed
  listed="$(printf '%s\n' "$files" | grep '\.go$' | while read -r f; do [ -f "$f" ] && echo "$f"; done | xargs gofmt -l 2>&1)"
  if [ -n "$listed" ]; then
    echo "gofmt: these changed files are not formatted (run gofmt -w on them):"; echo "$listed"; return 1
  fi
  return 0
}
if printf '%s\n' "$files" | grep -q '\.go$'; then step "gofmt -l (changed Go files)" gofmt_changed; fi
for f in ${jschecks[@]+"${jschecks[@]}"}; do step "node --check $f" node --check "$f"; done
for f in ${shchecks[@]+"${shchecks[@]}"}; do step "bash -n $f" bash -n "$f"; done
# the flaky suite's tests this diff touched, first and alone, then skipped by the runs below
flaky_skip=()
if [ ${#gopkgs[@]} -gt 0 ] || [ -n "$units" ]; then
  flaky_re="$(node scripts/flaky-suite.js go-regex)"
  flaky_gopkgs=" $(node scripts/flaky-suite.js go-pkgs) "
  flaky_board=",$(node scripts/flaky-suite.js board-units),"
  fpk=()
  for p in ${gopkgs[@]+"${gopkgs[@]}"}; do case "$flaky_gopkgs" in *" $p "*) fpk+=("$p") ;; esac; done
  if [ ${#fpk[@]} -gt 0 ] && [ -n "$flaky_re" ]; then
    step "flaky suite: go test ${fpk[*]}" bash scripts/test.sh all -run "$flaky_re" "${fpk[@]}"
    flaky_skip=(-skip "$flaky_re")
  fi
  fu=""; rest_units=""
  for u in $(echo "$units" | tr ',' ' '); do
    case "$flaky_board" in *",$u,"*) fu="${fu:+$fu,}$u" ;; *) rest_units="${rest_units:+$rest_units,}$u" ;; esac
  done
  if [ -n "$fu" ]; then
    step "flaky suite: headless units $fu" bash -c 'node scripts/test-board-sharded.js --local --no-retry --units "$1"' _ "$fu"
    units="$rest_units"
  fi
fi
[ "$gobuild" = 1 ] && step "go build ./..." go build -o /dev/null ./...
for g in ${gofiles[@]+"${gofiles[@]}"}; do step "go test $g" go test -count=1 $g; done
for g in ${govet[@]+"${govet[@]}"}; do step "go vet $g" go vet "$g"; done
# test.sh: unit mode keeps the 20ms budget, all mode adds -tags integration.
[ ${#gopkgs[@]} -gt 0 ] && step "go test ($mode) ${gopkgs[*]}" bash scripts/test.sh "$mode" ${flaky_skip[@]+"${flaky_skip[@]}"} "${gopkgs[@]}"
for n in ${nodes[@]+"${nodes[@]}"}; do step "node $n" node "$n"; done

if [ -n "$units" ]; then
  n=$(echo "$units" | tr ',' '\n' | grep -c .)
  shards=$(( n < 4 ? n : 4 ))
  out="$(mktemp)"
  step "headless units ($n): $units" bash -c 'node scripts/test-board-sharded.js --local --shards "$1" --units "$2" 2>&1 | tee "$3"; exit "${PIPESTATUS[0]}"' _ "$shards" "$units" "$out"
  if grep -q "playwright is not installed\|chromium browser is not installed" "$out"; then
    skipped+=("headless units: playwright or chromium is missing, so $units did NOT run")
  fi
  rm -f "$out"
fi

echo
echo "test-touched: $(( $(date +%s) - t0 ))s in all"
for s in ${skipped[@]+"${skipped[@]}"}; do echo "SKIPPED $s"; done
if [ "$fail" != 0 ]; then
  for f in "${failed[@]}"; do echo "FAILED $f"; done
  exit 1
fi
echo "test-touched: everything it ran passed"
