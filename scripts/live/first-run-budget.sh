#!/usr/bin/env bash
# First-run budget: from an empty home, `atrium run` comes up and a first card shows on the board within BUDGET_S
# seconds. The card comes from the SessionStart hook `atrium run` wired into the empty home's claude settings, fired
# the way claude fires it, so no model or login is needed. Every run prints its time, under budget or not.
#
#   bash scripts/live/first-run-budget.sh [--runs N] [--budget S] [--port P]
#
# The budget is about twice the median measured on sgg (880ms over 7 runs, 2026-11-19). ATRIUM_BIN skips the build.
# Ports P-1..P+3 must be free. Everything lives under build.claude/first-run-budget.
set -euo pipefail

budget=${BUDGET_S:-2}
runs=1
port=7968
while [ $# -gt 0 ]; do
  case "$1" in
    --runs) runs=$2; shift 2 ;;
    --budget) budget=$2; shift 2 ;;
    --port) port=$2; shift 2 ;;
    *) echo "unknown flag $1" >&2; exit 2 ;;
  esac
done

repo=$(cd "$(dirname "$0")/../.." && pwd)
work="$repo/build.claude/first-run-budget"
mkdir -p "$work"
exe=""
case "$(uname -s)" in MINGW*|MSYS*|CYGWIN*) exe=".exe" ;; esac
bin=${ATRIUM_BIN:-"$work/atrium$exe"}
if [ -z "${ATRIUM_BIN:-}" ]; then
  echo "== build $bin"
  (cd "$repo" && go build -o "$bin" ./cmd/atrium)
fi
# A new binary's first exec pays the OS's scan of it (2.3s on sgg), which is not atrium's to budget.
"$bin" version >/dev/null

board="127.0.0.1:$port"
roomhttp="127.0.0.1:$((port + 3))"
# A run over budget still waits up to this long, so its time is printed rather than cut off.
cap_ms=60000
now_ms() { date +%s%3N; }

# until_ok runs "$@" every 100ms until it succeeds or the deadline in ms passes.
until_ok() {
  local deadline=$(( $(now_ms) + $1 )); shift
  until "$@"; do
    [ "$(now_ms)" -lt "$deadline" ] || return 1
    sleep 0.1 # wait: poll interval under the deadline above
  done
}

# The hub answers /v1/tasks with 200 only once this machine's room has attached.
room_attached() { [ "$(curl -s -o /dev/null -w '%{http_code}' "http://$board/v1/tasks")" = 200 ]; }
card_shows() { curl -s "http://$board/v1/tasks" | grep -q '"wire_name": *"first-card"'; }

# clean_env runs "$@" in the empty home with none of the caller's atrium settings, so no live room is found or touched.
clean_env() {
  local unset=()
  for v in $(env | sed -n 's/^\(ATRIUM_[A-Za-z0-9_]*\)=.*/\1/p'); do unset+=(-u "$v"); done
  env "${unset[@]}" -u WORKTREE_ROOT -u CLAUDE_CONFIG_DIR -u XDG_RUNTIME_DIR -u XDG_STATE_HOME -u XDG_CONFIG_HOME \
    -u XDG_CACHE_HOME HOME="$home" USERPROFILE="$whome" LOCALAPPDATA="$whome/AppData/Local" \
    APPDATA="$whome/AppData/Roaming" "$@"
}

stop_all() {
  [ -n "${home:-}" ] && clean_env "$bin" stop --now --runners --url "http://$roomhttp" >/dev/null 2>&1 || true
  [ -n "${runpid:-}" ] && kill "$runpid" 2>/dev/null || true
  [ -n "${runpid:-}" ] && wait "$runpid" 2>/dev/null || true
  runpid=""
}
trap stop_all EXIT

fail() {
  grep -v 'no room is attached' "$home/run.log" | tail -40 || true
  echo "FAIL: $1" >&2
  exit 1
}

times=()
over=0
for i in $(seq 1 "$runs"); do
  home="$work/home-$i"
  rm -rf "$home"; mkdir -p "$home/first-card"
  whome="$home"
  cwd="$home/first-card"
  if [ -n "$exe" ]; then whome=$(cygpath -w "$home"); cwd=$(cygpath -m "$cwd"); fi

  t0=$(now_ms)
  clean_env "$bin" run --addr "$board" --link "127.0.0.1:$((port + 1))" --http "$roomhttp" \
    --agent "127.0.0.1:$((port - 1))" >"$home/run.log" 2>&1 &
  runpid=$!
  until_ok "$cap_ms" room_attached || fail "run $i: no room attached to the board on $board within $((cap_ms / 1000))s"
  tup=$(now_ms)

  settings="$home/.claude/settings.json"
  hookline=$(sed -n '/"SessionStart"/,/"command"/s/.*"command": *"\([^"]*\)".*/\1/p' "$settings" 2>/dev/null || true)
  [ -n "$hookline" ] || fail "run $i: atrium run wired no SessionStart hook into $settings"
  payload="{\"session_id\":\"first-run-budget-$i\",\"cwd\":\"$cwd\",\"hook_event_name\":\"SessionStart\",\"source\":\"startup\"}"
  # The hook's own binary may be an installed atrium, so the one under test runs its arguments.
  # shellcheck disable=SC2086
  (cd "$home/first-card" && echo "$payload" | clean_env "$bin" ${hookline#* } >/dev/null)
  until_ok "$cap_ms" card_shows || fail "run $i: no first card on the board within $((cap_ms / 1000))s"
  ms=$(( $(now_ms) - t0 ))
  times+=("$ms")
  verdict=ok
  if [ "$ms" -gt $((budget * 1000)) ]; then verdict=OVER; over=$((over + 1)); fi
  echo "run $i: room attached $((tup - t0))ms, first card ${ms}ms, budget ${budget}s: $verdict"
  stop_all
done

median=$(printf '%s\n' "${times[@]}" | sort -n | sed -n "$(( (runs + 1) / 2 ))p")
echo "FIRST-RUN: median ${median}ms over $runs run(s), budget ${budget}s, $over over"
[ "$over" -eq 0 ] || { echo "FAIL: $over of $runs first run(s) over the ${budget}s budget" >&2; exit 1; }
