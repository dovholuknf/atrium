#!/usr/bin/env bash
# Tell the hub a CI run finished. The script decides what is sent, and the workflow only carries it.
#
#   ci-notify.sh EVENT_FILE [--out FILE] [--url URL] [--secret S] [--local]
#
# EVENT_FILE is the workflow_run event JSON ($GITHUB_EVENT_PATH). Anything else exits 3 and writes nothing.
# The body is the event as is, written to --out (default build.claude/ci-notify-body.json), and that path is printed.
# With no --url that is all it does, which is what the workflow's validate step runs before the ziti-hook step.
# With --url it POSTs the body with the secret in X-Ziti-Hook-Secret. --local is
# http://127.0.0.1:7778/_hub/ci-hook, the hub's loopback route, so a route can be tried without GitHub.
# The secret comes from --secret or ATRIUM_CI_HOOK_SECRET, and is never printed.
#
# Exit codes: 0 sent or written, 2 a usage problem, 3 not a workflow_run event, 1 the POST failed.
set -uo pipefail

event=""; out="${CI_NOTIFY_OUT:-build.claude/ci-notify-body.json}"; url=""; secret="${ATRIUM_CI_HOOK_SECRET:-}"
while [ $# -gt 0 ]; do
  case "$1" in
    --out) [ $# -ge 2 ] || { echo "--out needs a file" >&2; exit 2; }; out="$2"; shift 2 ;;
    --url) [ $# -ge 2 ] || { echo "--url needs an address" >&2; exit 2; }; url="$2"; shift 2 ;;
    --secret) [ $# -ge 2 ] || { echo "--secret needs a value" >&2; exit 2; }; secret="$2"; shift 2 ;;
    --local) url="http://127.0.0.1:7778/_hub/ci-hook"; shift ;;
    -h|--help) sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*) echo "unknown flag $1. see --help" >&2; exit 2 ;;
    *) [ -z "$event" ] || { echo "one event file only" >&2; exit 2; }; event="$1"; shift ;;
  esac
done

[ -n "$event" ] || { echo "ci-notify: name the event file" >&2; exit 2; }
[ -f "$event" ] || { echo "ci-notify: no file $event" >&2; exit 2; }

if ! grep -Eq '"workflow_run"[[:space:]]*:[[:space:]]*\{' "$event" ||
   ! grep -Eq '"conclusion"[[:space:]]*:[[:space:]]*"[a-z_]+"' "$event"; then
  echo "ci-notify: $event is not a finished workflow_run event, so nothing is sent" >&2
  exit 3
fi

mkdir -p "$(dirname "$out")" || exit 2
cp "$event" "$out" || exit 2
echo "$out"

[ -n "$url" ] || exit 0
[ -n "$secret" ] || { echo "ci-notify: a url needs a secret: --secret or ATRIUM_CI_HOOK_SECRET" >&2; exit 2; }

hdr="$(mktemp)" || exit 1
trap 'rm -f "$hdr"' EXIT
chmod 600 "$hdr"
printf 'X-Ziti-Hook-Secret: %s\n' "$secret" >"$hdr"
if ! curl -sS --fail-with-body --max-time 30 -X POST -H "Content-Type: application/json" -H "@$hdr" \
     --data-binary "@$out" "$url" >&2; then
  echo "ci-notify: the hub did not take it" >&2
  exit 1
fi
echo >&2
