#!/usr/bin/env bash
# Builds and vets every module alone, with GOWORK=off, so a missing require or replace fails here and not only in the
# workspace. Then fails when `go work sync` changes a go.mod or go.sum, which means the pinned versions drifted.
# See docs/design/go-modules.md section 5.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$here"

dirs="$(go list -m -f '{{.Dir}}' | tr -d '\r')"

rc=0
while IFS= read -r dir; do
  [ -n "$dir" ] || continue
  if ! (cd "$dir" && GOWORK=off go build ./... && GOWORK=off go vet ./...); then
    echo "FAILED: $dir does not build alone with GOWORK=off" >&2
    rc=1
  fi
done <<<"$dirs"

sums() {
  while IFS= read -r dir; do
    [ -n "$dir" ] || continue
    cat "$dir/go.mod" "$dir/go.sum" 2>/dev/null || true
  done <<<"$dirs" | cksum
}
before="$(sums)"
go work sync
if [ "$before" != "$(sums)" ]; then
  echo "FAILED: go work sync changed a go.mod or go.sum, run it and commit the result" >&2
  rc=1
fi
exit "$rc"
