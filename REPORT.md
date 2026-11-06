# qa-refit report

Fix: `TestALiveRefitRestoresTheCursorOnlyWhenThePtyMoves` waits for the replay's cursor move in its `hostWait`.

PROVEN (by reading `internal/daemon/attach.go`):
- Write order on attach is `caps`, `size` (`tellSize`), then the replay body with the cursor move. The size frame
  always precedes the replay, so a wait on the size frame cannot guarantee the replay has landed.
- The failing log's `large.seen()` was `caps` then `size 200x20` and nothing else, matching a check made between the
  size write and the replay write.

INFERRED:
- That this gap is the whole cause on the Windows runner. Not reproduced locally, so no before/after run.

Product bug: none. The size-before-backlog order is deliberate (see the comment in `attach.go`).
