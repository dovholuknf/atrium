#!/usr/bin/env bash
# Pick which ci.yml jobs a manual dispatch runs, as the `select` job's outputs.
#
#   ci-select.sh [--os all|ubuntu|macos|windows] [--suite all|go|ui]
#
# Prints one `name=value` line per job to stdout, the format $GITHUB_OUTPUT takes: flaky_<os> and check_<os> are the JSON
# array of groups that job runs, `[]` when the selection skips it, and `skipped` counts the jobs left at `[]`.
# A group is ui when its name starts with flaky-board or board, every other group is go. go-checks-visual is go: it is
# the go checks, and its board-visual part never fails the job. No flags is all and all, which is every push and pull
# request.
set -euo pipefail

os=all; suite=all
while [ $# -gt 0 ]; do
  case "$1" in
    --os) [ $# -ge 2 ] || { echo "--os needs a value" >&2; exit 2; }; os="${2:-all}"; shift 2 ;;
    --suite) [ $# -ge 2 ] || { echo "--suite needs a value" >&2; exit 2; }; suite="${2:-all}"; shift 2 ;;
    -h|--help) sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument $1. see --help" >&2; exit 2 ;;
  esac
done
[ -n "$os" ] || os=all
[ -n "$suite" ] || suite=all
case "$os" in all|ubuntu|macos|windows) ;; *) echo "ci-select: --os $os is not all, ubuntu, macos or windows" >&2; exit 2 ;; esac
case "$suite" in all|go|ui) ;; *) echo "ci-select: --suite $suite is not all, go or ui" >&2; exit 2 ;; esac

flaky_ubuntu="flaky-go"
flaky_macos="flaky-go flaky-board"
flaky_windows="flaky-go flaky-board"
check_ubuntu="go-all board-1 board-2"
check_macos="go-test go-checks-visual board-1 board-2"
check_windows="go-test-1 go-test-2 go-checks-visual board-1 board-2"

skipped=0
for kind in flaky check; do
  for p in ubuntu macos windows; do
    var="${kind}_${p}"
    json=""
    if [ "$os" = all ] || [ "$os" = "$p" ]; then
      for g in ${!var}; do
        case "$g" in flaky-board*|board*) side=ui ;; *) side=go ;; esac
        if [ "$suite" = all ] || [ "$suite" = "$side" ]; then json="${json:+$json,}\"$g\""; fi
      done
    fi
    [ -n "$json" ] || skipped=$((skipped + 1))
    echo "$var=[$json]"
  done
done
echo "skipped=$skipped"
