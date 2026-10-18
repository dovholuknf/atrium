#!/usr/bin/env bash
# Tell the hub a CI run failed. The script decides what is sent, and the workflow only carries it.
#
#   ci-notify.sh [EVENT_FILE] [--conclusion C] [--out FILE] [--url URL] [--secret S] [--local]
#
# With no EVENT_FILE the body is built from this run's own context: GITHUB_REPOSITORY, GITHUB_RUN_ID,
# GITHUB_RUN_ATTEMPT, GITHUB_WORKFLOW, GITHUB_WORKFLOW_REF, GITHUB_HEAD_REF or GITHUB_REF_NAME, GITHUB_SHA (a pull
# request's own head sha from GITHUB_EVENT_PATH when jq is there), GITHUB_EVENT_NAME and GITHUB_SERVER_URL, in the one
# shape the hub reads (a workflow_run event's fields). That is what the ci workflow's last job runs. --conclusion is
# failure, timed_out, cancelled, success or action_required, default failure. Run by hand outside GitHub, every field
# the run would give falls back to the repo's git state, a fresh run id and dovholuknf/atrium.
# With EVENT_FILE (a workflow_run event JSON) the body is that event as is, which replays a recorded run. Anything
# else exits 3 and writes nothing.
# The body is written to --out (default build.claude/ci-notify-body.json) and that path is printed. With no --url
# that is all it does, and the ziti-hook step carries the body. With --url it POSTs the body with the secret in
# X-Ziti-Hook-Secret. --local is http://127.0.0.1:7778/_hub/ci-hook, the hub's loopback route, so a route can be
# tried without GitHub. The secret comes from --secret or ATRIUM_CI_HOOK_SECRET, and is never printed.
#
# Exit codes: 0 sent or written, 2 a usage problem, 3 EVENT_FILE is not a workflow_run event, 1 the POST failed.
set -uo pipefail

event=""; conclusion="failure"; out="${CI_NOTIFY_OUT:-build.claude/ci-notify-body.json}"; url=""; secret="${ATRIUM_CI_HOOK_SECRET:-}"
while [ $# -gt 0 ]; do
  case "$1" in
    --conclusion) [ $# -ge 2 ] || { echo "--conclusion needs a value" >&2; exit 2; }; conclusion="$2"; shift 2 ;;
    --out) [ $# -ge 2 ] || { echo "--out needs a file" >&2; exit 2; }; out="$2"; shift 2 ;;
    --url) [ $# -ge 2 ] || { echo "--url needs an address" >&2; exit 2; }; url="$2"; shift 2 ;;
    --secret) [ $# -ge 2 ] || { echo "--secret needs a value" >&2; exit 2; }; secret="$2"; shift 2 ;;
    --local) url="http://127.0.0.1:7778/_hub/ci-hook"; shift ;;
    -h|--help) sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*) echo "unknown flag $1. see --help" >&2; exit 2 ;;
    *) [ -z "$event" ] || { echo "one event file only" >&2; exit 2; }; event="$1"; shift ;;
  esac
done

mkdir -p "$(dirname "$out")" || exit 2

if [ -n "$event" ]; then
  [ -f "$event" ] || { echo "ci-notify: no file $event" >&2; exit 2; }
  if ! grep -Eq '"workflow_run"[[:space:]]*:[[:space:]]*\{' "$event" ||
     ! grep -Eq '"conclusion"[[:space:]]*:[[:space:]]*"[a-z_]+"' "$event"; then
    echo "ci-notify: $event is not a finished workflow_run event, so nothing is sent" >&2
    exit 3
  fi
  cp "$event" "$out" || exit 2
else
  case "$conclusion" in success|failure|cancelled|timed_out|action_required) ;;
    *) echo "ci-notify: --conclusion $conclusion is not one the hub knows" >&2; exit 2 ;; esac
  # json_str makes one value safe inside a JSON string: backslash and quote escaped, line breaks dropped.
  json_str() { printf '%s' "$1" | tr -d '\r\n' | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'; }
  repo="${GITHUB_REPOSITORY:-dovholuknf/atrium}"
  id="${GITHUB_RUN_ID:-$(date +%s)}"
  attempt="${GITHUB_RUN_ATTEMPT:-1}"
  case "$id$attempt" in ''|*[!0-9]*) echo "ci-notify: the run id and attempt must be numbers" >&2; exit 2 ;; esac
  wfname="${GITHUB_WORKFLOW:-ci}"
  wfpath="${GITHUB_WORKFLOW_REF:-}"; wfpath="${wfpath#"$repo"/}"; wfpath="${wfpath%%@*}"
  [ -n "$wfpath" ] || wfpath=".github/workflows/ci.yml"
  branch="${GITHUB_HEAD_REF:-${GITHUB_REF_NAME:-$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)}}"
  sha="${GITHUB_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
  if [ "${GITHUB_EVENT_NAME:-}" = pull_request ] && [ -f "${GITHUB_EVENT_PATH:-}" ] && command -v jq >/dev/null 2>&1; then
    prsha="$(jq -r '.pull_request.head.sha // empty' "$GITHUB_EVENT_PATH")"
    [ -z "$prsha" ] || sha="$prsha"
  fi
  printf '{"action":"completed","workflow_run":{"id":%s,"run_attempt":%s,"name":"%s","path":"%s","head_branch":"%s","head_sha":"%s","event":"%s","conclusion":"%s","html_url":"%s/%s/actions/runs/%s"},"repository":{"full_name":"%s"}}\n' \
    "$id" "$attempt" "$(json_str "$wfname")" "$(json_str "$wfpath")" "$(json_str "$branch")" "$(json_str "$sha")" \
    "$(json_str "${GITHUB_EVENT_NAME:-local}")" "$conclusion" "$(json_str "${GITHUB_SERVER_URL:-https://github.com}")" \
    "$(json_str "$repo")" "$id" "$(json_str "$repo")" >"$out" || exit 2
fi
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
