#!/usr/bin/env bash
# Posts one event to the hub's POST /_hub/events from the machine the hub runs on. See docs/fabric/land-webhook-design.md, 6.
#
#   scripts/atrium-event.sh --kind land --repo dovholuknf/atrium --branch main --outcome pushed --from 3f1c0de --to 9a0b1c2 --commits 14
#
# Prints one line: `event: 2 subscribers` or `event: duplicate`. On any failure it prints one warning and exits 0, because a
# land must never fail since the hub was down.

kind="" repo="" branch="" outcome="" from="" to="" commits=0 reason="" hub="${ATRIUM_HUB_URL:-}"
while [ $# -gt 0 ]; do
  case "$1" in
    --kind) kind="$2"; shift 2 ;;
    --repo) repo="$2"; shift 2 ;;
    --branch) branch="$2"; shift 2 ;;
    --outcome) outcome="$2"; shift 2 ;;
    --from) from="$2"; shift 2 ;;
    --to) to="$2"; shift 2 ;;
    --commits) commits="$2"; shift 2 ;;
    --reason) reason="$2"; shift 2 ;;
    --hub-url) hub="$2"; shift 2 ;;
    *) echo "atrium-event: warning: unknown argument $1" >&2; exit 0 ;;
  esac
done
hub="${hub:-http://127.0.0.1:7778}"

json_escape() {
  printf '%s' "$1" | tr '\r\n\t' '   ' | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

case "$commits" in
  ''|*[!0-9]*) commits=0 ;;
esac

body=$(printf '{"kind":"%s","repo":"%s","branch":"%s","outcome":"%s","from":"%s","to":"%s","commits":%s,"reason":"%s"}' \
  "$(json_escape "$kind")" "$(json_escape "$repo")" "$(json_escape "$branch")" "$(json_escape "$outcome")" \
  "$(json_escape "$from")" "$(json_escape "$to")" "$commits" "$(json_escape "$reason")")

out=$(curl -sS -m 5 -X POST -H 'Content-Type: application/json' --data "$body" -w '\n%{http_code}' "${hub%/}/_hub/events" 2>&1)
status=$?
code=$(printf '%s' "$out" | tail -n 1)
text=$(printf '%s' "$out" | sed '$d')

if [ $status -ne 0 ]; then
  echo "atrium-event: warning: the hub did not take the event: $(printf '%s' "$out" | tr '\r\n' '  ')" >&2
  exit 0
fi
case "$code" in
  202)
    n=$(printf '%s' "$text" | sed -n 's/.*"subscribers":\([0-9]*\).*/\1/p')
    echo "event: ${n:-0} subscribers"
    ;;
  200)
    echo "event: duplicate"
    ;;
  *)
    echo "atrium-event: warning: the hub did not take the event: $code $(printf '%s' "$text" | tr '\r\n' '  ')" >&2
    ;;
esac
exit 0
