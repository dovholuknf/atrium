#!/usr/bin/env bash
# Three hubs, two rooms. Each room files on its own hub with no id, so the id is the room's, and a late hub 2 takes both
# rooms without a collision. Also an id the caller names still goes through as before. Needs `make build`.
set -u
here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
R="${1:-$here/build.claude/multihub-ids}"; base=18800
M="bash $here/scripts/multihub.sh"
fail=0; ok() { if [ "$2" = "$3" ]; then echo "ok   $1"; else echo "FAIL $1: got '$2' want '$3'"; fail=1; fi; }
$M down --root "$R" >/dev/null 2>&1; rm -rf "$R"
trap '$M down --root "$R" >/dev/null 2>&1' EXIT
$M up -n 2 -r m1a:0 -r m1b:1 --root "$R" >/dev/null || exit 1
rb() { echo $((base + 100 + 10 * $1)); }
ask() { curl -s -X POST "http://127.0.0.1:$(rb "$1")/v1/hub/ask" -H 'Content-Type: application/json' -d "$2"; }
ida=$(ask 0 '{"method":"POST","path":"/_hub/backlog","body":{"dept":"fabric","title":"from a"}}' | jq -r .id)
idb=$(ask 1 '{"method":"POST","path":"/_hub/backlog","body":{"dept":"fabric","title":"from b"}}' | jq -r .id)
ok "room a mints its own id" "$ida" "m1a-1"
ok "room b mints its own id" "$idb" "m1b-1"
named=$(ask 0 '{"method":"POST","path":"/_hub/backlog","body":{"id":"f-900","dept":"fabric","title":"named"}}' | jq -r .id)
ok "a named id is kept" "$named" "f-900"
$M hub 2 --root "$R" >/dev/null
$M rehome m1a 2 --root "$R" >/dev/null; $M rehome m1b 2 --root "$R" >/dev/null
for _ in $(seq 1 80); do
  [ "$(curl -s "http://127.0.0.1:$((base + 20))/_hub/backlog" | jq '.items|length')" = 3 ] && break; sleep 0.5; done
got=$(curl -s "http://127.0.0.1:$((base + 20))/_hub/backlog" | jq -r '[.items[].id]|sort|join(",")')
ok "the late hub holds all three, no collision" "$got" "f-900,m1a-1,m1b-1"
exit $fail
