#!/usr/bin/env bash
# N empty hubs and some rooms on this machine, each with its own dir and ports, standing in for separate machines.
#
#   scripts/multihub.sh up [-n N] [-r name:hub ...] [--root DIR] [--base PORT]   start N hubs, add and join the rooms
#   scripts/multihub.sh status [--root DIR]                                      what runs, and the ports
#   scripts/multihub.sh down [--root DIR]                                        stop everything this root started
#   scripts/multihub.sh rehome NAME HUB [--root DIR] [--base PORT]               join a room to another hub, keeping its log
#   scripts/multihub.sh hub N [--root DIR] [--base PORT]                         start one more empty hub, a late joiner
#
# A room is joined to ONE hub: -r m1a:0 -r m1b:2 puts m1a on hub 0 and m1b on hub 2. Never the real hub or room: every
# process runs under --root with its own HOME, its own ports (hub i at BASE+10i and +1, room j at BASE+100+10j and +1)
# and --isolated. The binary is $ATRIUM_BIN, default build.claude/atrium.exe.
set -u
here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
A="${ATRIUM_BIN:-$here/build.claude/atrium.exe}"
cmd="${1:-}"; shift || true
n=3; base=18800; root="${MULTIHUB_ROOT:-$here/build.claude/multihub}"; rooms=(); pos=()
while [ $# -gt 0 ]; do
  case "$1" in
    -n) n="$2"; shift 2 ;;
    -r) rooms+=("$2"); shift 2 ;;
    --root) root="$2"; shift 2 ;;
    --base) base="$2"; shift 2 ;;
    *) pos+=("$1"); shift ;;
  esac
done
mkdir -p "$root"; root="$(cd "$root" && pwd)"
export HOME="$root/home" ATRIUM_ROOT="$root/atrium-root"
mkdir -p "$HOME" "$ATRIUM_ROOT"

hubport() { echo $((base + 10 * $1)); }
roomport() { echo $((base + 100 + 10 * $1)); }

wait_for() { for _ in $(seq 1 150); do eval "$1" >/dev/null 2>&1 && return 0; sleep 0.2; done; return 1; }

start_hub() {
  local i=$1 d="$root/hub$1" p; p=$(hubport "$1")
  mkdir -p "$d"
  nohup "$A" run --no-room --addr "127.0.0.1:$p" --link "127.0.0.1:$((p + 1))" --atrium-dir "$d" --atrium-db "$d/hub.db" \
    >"$d/hub.log" 2>&1 &
  echo $! >"$d/pid"
  wait_for "curl -sf http://127.0.0.1:$p/_hub/rooms" || { echo "hub $i did not start, see $d/hub.log" >&2; return 1; }
}

start_room() {
  local name=$1 hub=$2 j=$3 d="$root/room-$1" p hp; p=$(roomport "$j"); hp=$(hubport "$hub")
  mkdir -p "$d"
  rm -rf "$d/cfg"
  local out join
  out=$("$A" rooms add "$name" --atrium-dir "$root/hub$hub" --atrium-db "$root/hub$hub/hub.db" --link "127.0.0.1:$((hp + 1))" \
    --atrium-board-addr "127.0.0.1:$hp" 2>&1)
  join=$(echo "$out" | sed -n -E 's/.*atrium room join ([^ ]+).*/\1/p' | head -1)
  [ -n "$join" ] || { echo "no join string for $name: $out" >&2; return 1; }
  nohup "$A" room join "$join" --dir "$d/cfg" --db "$d/room.db" --http "127.0.0.1:$p" --agent "127.0.0.1:$((p + 1))" \
    --isolated >"$d/room.log" 2>&1 &
  echo $! >"$d/pid"
  echo "$hub" >"$d/hub"; echo "$j" >"$d/index"
  wait_for "curl -s http://127.0.0.1:$hp/_hub/rooms | grep -q '\"name\":\"$name\"'" || {
    echo "room $name did not attach to hub $hub, see $d/room.log" >&2; return 1; }
}

case "$cmd" in
  up)
    [ -x "$A" ] || { echo "no binary at $A: make build first" >&2; exit 1; }
    for i in $(seq 0 $((n - 1))); do start_hub "$i" || { "$0" down --root "$root"; exit 1; }; done
    j=0
    for spec in "${rooms[@]+"${rooms[@]}"}"; do
      start_room "${spec%%:*}" "${spec##*:}" "$j" || { "$0" down --root "$root"; exit 1; }
      j=$((j + 1))
    done
    "$0" status --root "$root" --base "$base"
    ;;
  status)
    for d in "$root"/hub* "$root"/room-*; do
      [ -f "$d/pid" ] || continue
      pid=$(cat "$d/pid"); st=down; kill -0 "$pid" 2>/dev/null && st=up
      echo "$(basename "$d") pid $pid $st"
    done
    ;;
  down)
    for d in "$root"/room-* "$root"/hub*; do
      [ -f "$d/pid" ] || continue
      kill "$(cat "$d/pid")" 2>/dev/null; rm -f "$d/pid"
    done
    ;;
  hub) start_hub "${pos[0]}" ;;
  rehome)
    name="${pos[0]}"; hub="${pos[1]}"; d="$root/room-$name"
    [ -f "$d/index" ] || { echo "no room $name under $root" >&2; exit 1; }
    [ -f "$d/pid" ] && { kill "$(cat "$d/pid")" 2>/dev/null; rm -f "$d/pid"; sleep 1; }
    start_room "$name" "$hub" "$(cat "$d/index")"
    ;;
  *) sed -n '2,9p' "$0" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
