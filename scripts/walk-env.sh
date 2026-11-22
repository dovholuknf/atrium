#!/usr/bin/env bash
# A throwaway atrium to drive the board's walk in: its own hub, room, state and ptyhost, and seven fake cards whose
# checkouts hold real git branches. It never touches the live atrium. See `bash scripts/walk-env.sh help`.
#
#   bash scripts/walk-env.sh up      build the env, print the board link
#   bash scripts/walk-env.sh down    stop it and delete it
#   bash scripts/walk-env.sh status  is it up, and the card links
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
env=${WALK_ENV_DIR:-$root/build.claude/walk-env}
port=${WALK_ENV_PORT:-7840}
room_port=$((port + 2))
bin=$root/build.claude/atrium.exe
api=http://127.0.0.1:$room_port
board=http://127.0.0.1:$port

q() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }

git_in() { git -C "$1" -c user.name=walker -c user.email=walker@example.test "${@:2}"; }

commit() { # dir message: commits everything staged and unstaged
  git_in "$1" add -A
  git_in "$1" commit -q -m "$2"
}

lines() { # n prefix: n numbered lines
  local i
  for ((i = 1; i <= $1; i++)); do echo "$2 line $i"; done
}

seed() { # dir: a project with a base commit on main, pushed to a local origin so origin/main is the base
  local dir=$1 origin=$env/origins/$(basename "$1").git
  git init -q --bare -b main "$origin"
  git clone -q "$origin" "$dir" 2>/dev/null
  git_in "$dir" checkout -q -b main 2>/dev/null || true
  mkdir -p "$dir/src" "$dir/docs"
  {
    echo "package main"
    echo
    echo "func total(xs []int) int {"
    echo "	n := 0"
    echo "	for _, x := range xs {"
    echo "		n += x"
    echo "	}"
    echo "	return n"
    echo "}"
  } >"$dir/src/sum.go"
  lines 12 "# readme" >"$dir/docs/readme.md"
  lines 8 "legacy" >"$dir/src/old_name.go"
  printf '\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01' >"$dir/logo.png"
  commit "$dir" "base"
  git_in "$dir" push -q origin main 2>/dev/null
}

card() { # name: a fresh checkout, a seed, and the branch the walk reads
  local dir=$env/cards/$1
  mkdir -p "$env/cards"
  seed "$dir"
  git_in "$dir" checkout -q -b "claude/$1"
  echo "$dir"
}

build_small() {
  local d; d=$(card small)
  sed -i.bak 's/n := 0/n := 0 \/\/ start at zero/' "$d/src/sum.go" && rm "$d/src/sum.go.bak"
  commit "$d" "Comment the accumulator"
}

build_large() {
  local d i; d=$(card large)
  for i in 1 2 3 4 5 6; do
    mkdir -p "$d/pkg/mod$i"
    lines $((10 * i)) "module $i" >"$d/pkg/mod$i/mod.go"
    commit "$d" "Add module $i"
  done
  lines 40 "# readme" >"$d/docs/readme.md"
  mkdir -p "$d/cmd/tool"
  lines 30 "tool" >"$d/cmd/tool/main.go"
  lines 25 "tool test" >"$d/cmd/tool/main_test.go"
  commit "$d" "Rewrite the readme and add the tool"
  sed -i.bak 's/return n/return n + 0/' "$d/src/sum.go" && rm "$d/src/sum.go.bak"
  lines 15 "extra" >>"$d/src/old_name.go"
  commit "$d" "Touch the base files"
}

build_dirty() {
  local d; d=$(card dirty)
  lines 6 "committed" >"$d/docs/committed.md"
  commit "$d" "One committed change"
  sed -i.bak 's/n += x/n += x * 1/' "$d/src/sum.go" && rm "$d/src/sum.go.bak"
  lines 4 "edited" >>"$d/docs/readme.md"
  lines 5 "staged" >"$d/docs/staged.md"
  git_in "$d" add docs/staged.md
  lines 9 "untracked" >"$d/docs/untracked.md"
}

build_merged() {
  local d; d=$(card merged)
  lines 5 "landed" >"$d/docs/landed.md"
  commit "$d" "A change that already landed"
  git_in "$d" checkout -q main
  git_in "$d" merge -q --no-ff "claude/merged" -m "Merge claude/merged"
  git_in "$d" push -q origin main
  git_in "$d" checkout -q "claude/merged"
}

build_binary() {
  local d; d=$(card binary)
  printf '\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x02\xff\xfe\x00\x01' >"$d/logo.png"
  head -c 4096 /dev/urandom >"$d/data.bin"
  lines 3 "caption" >"$d/docs/caption.md"
  commit "$d" "New logo, a data blob and a caption"
}

build_rename() {
  local d; d=$(card rename)
  git_in "$d" mv src/old_name.go src/new_name.go
  echo "// renamed and edited" >>"$d/src/new_name.go"
  git_in "$d" mv docs/readme.md docs/README.md
  commit "$d" "Rename two files, edit one"
}

build_findings() {
  local d; d=$(card findings)
  sed -i.bak 's/n := 0/n := 0 \/\/ start at zero/' "$d/src/sum.go" && rm "$d/src/sum.go.bak"
  commit "$d" "A change with review findings"
  mkdir -p "$d/findings"
  printf '%s\n' "https://example.test/pull/1" "MED src/sum.go line 4: n := 0" "" "- the accumulator should be named" \
    "- a sum of nothing is zero, say so" "" "Evidence" "Id: f1" >"$d/findings/01-med-src-sum.go-L4.txt"
  printf '%s\n' "https://example.test/pull/1" "LOW src/sum.go line 8: return n" "" "- fine as is" "" "Evidence" "Id: f2" \
    >"$d/findings/02-low-src-sum.go-L8.txt"
}

up() {
  [ -x "$bin" ] || (cd "$root" && make build)
  if curl -fsS -m2 "$api/v1/harnesses" >/dev/null 2>&1; then echo "already up: $board"; status; return; fi
  mkdir -p "$env/hub" "$env/room" "$env/logs" "$env/state"
  build_small; build_large; build_dirty; build_merged; build_binary; build_rename; build_findings
  # WORKTREE_ROOT moves the state dir, and with it the ptyhost, so no live terminal is shared.
  WORKTREE_ROOT=$env/state ATRIUM_ROOT=$env/root nohup "$bin" run --isolated --no-hooks --no-open \
    --addr "127.0.0.1:$port" --link "127.0.0.1:$((port + 1))" --atrium-dir "$env/hub" --atrium-db "$env/hub/hub.db" \
    --dir "$env/room" --db "$env/room/room.db" --http "127.0.0.1:$room_port" --agent "127.0.0.1:$((port + 3))" \
    --log-dir "$env/logs" >"$env/logs/run.out" 2>&1 </dev/null &
  disown
  local i
  for i in $(seq 1 30); do
    curl -fsS -m2 "$api/v1/harnesses" >/dev/null 2>&1 && break
    perl -e 'select(undef,undef,undef,1)'
  done
  curl -fsS -m2 "$api/v1/harnesses" >/dev/null || { echo "the room did not come up, see $env/logs" >&2; exit 1; }
  curl -fsS -X PUT "$api/v1/harnesses/fake" -H 'content-type: application/json' \
    -d '{"label":"fake","enabled":true,"cmd":"/bin/cat","args":[],"launch_mode":"pty"}' >/dev/null
  local n
  for n in small large dirty merged binary rename findings; do
    curl -fsS -X POST "$api/v1/launch" -H 'content-type: application/json' \
      -d "{\"harness\":\"fake\",\"cwd\":\"$env/cards/$n\",\"title\":\"walk-$n\"}" >/dev/null
  done
  status
}

status() {
  curl -fsS -m2 "$api/v1/harnesses" >/dev/null 2>&1 || { echo "walk env is down"; return 1; }
  echo "walk env board: $board  (the room's own board: $api)"
  curl -fsS "$api/v1/tasks" | q "'\n'.join('  {} {}'.format(t['wire_name'], '$board/#card=' + t['id'] + '&walk=' + t['worktree']) for t in (d if isinstance(d,list) else d.get('tasks',[])))"
}

down() {
  pkill -f "$env/" 2>/dev/null || true
  pkill -f "ptyhost --state-dir $env/state" 2>/dev/null || true
  rm -rf "$env"
  echo "walk env removed"
}

case "${1:-help}" in
  up) up ;;
  down) down ;;
  status) status ;;
  *) sed -n 2,7p "$0" ;;
esac
