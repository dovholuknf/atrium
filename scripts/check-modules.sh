#!/usr/bin/env bash
# Builds and vets every module alone, with GOWORK=off, so a missing require or replace fails here and not only in the
# workspace. Then fails when `go work sync` changes a go.mod or go.sum, which means the pinned versions drifted.
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
saved="$(mktemp -d "$here/.check-modules.XXXXXX")"
files="$here/go.work $here/go.work.sum"
while IFS= read -r dir; do
  [ -n "$dir" ] || continue
  files="$files $dir/go.mod $dir/go.sum"
done <<<"$dirs"
# go work sync must leave the tree as it found it, whatever the platform does to go.work
restore() {
  n=0
  for f in $files; do
    n=$((n + 1))
    if [ -f "$saved/$n" ]; then cp "$saved/$n" "$f"; else rm -f "$f"; fi
  done
  rm -rf "$saved"
}
n=0
for f in $files; do
  n=$((n + 1))
  if [ -f "$f" ]; then cp -p "$f" "$saved/$n"; fi
done
trap restore EXIT
before="$(sums)"
go work sync
if [ "$before" != "$(sums)" ]; then
  echo "FAILED: go work sync changed a go.mod or go.sum, run it and commit the result" >&2
  rc=1
fi
exit "$rc"
