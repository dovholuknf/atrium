#!/usr/bin/env bash
# Refuse a branch that commits a card's own BRIEF.md, REPORT.md or QUESTIONS.md at the repo root. Every card writes
# them there, so one on claude/main shows as modified on every branch and collides at every merge.
#
#   bash scripts/check-card-files.sh [BASE]     BASE defaults to claude/main, compared from its merge base with HEAD
#
# Committed and staged files count. An untracked one is the card's own and does not.
set -uo pipefail
base="${1:-claude/main}"
mb="$(git merge-base "$base" HEAD)" || { echo "check-card-files: $base and HEAD share no history" >&2; exit 2; }
found="$(git diff --cached --name-only --diff-filter=d "$mb" -- BRIEF.md REPORT.md QUESTIONS.md)"
if [ -n "$found" ]; then
  echo "check-card-files: these are a card's own files and never land at the repo root (git rm --cached them):"
  echo "$found"
  exit 1
fi
