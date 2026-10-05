# f-025 report

## Done
- `internal/hubstore/notify_test.go`: tests for the present-without-identity refresh (and the notifyTouch window), the
  unchanged-identity refresh, the error returns (closed store, missing table) and the fire order.
- A real bug found and fixed: `NotifyRecord` ranged over the `ids` map, so the fire list came back in random order.
  It now sorts the ids first (`internal/hubstore/notify.go`).
- `scripts/room-toolchain.ps1`: `local` uses the System32 `powershell.exe` path, as `room-defender.ps1` does.
- `scripts/board-suite-remote.ps1`: suite args are now remaining arguments (`string[]`), kept whole and base64'd one
  per line. A `-DryRun` switch prints them. `scripts/test-board-sharded.js` passes them as separate argv entries.
- f-025 and the fabric QUEUE.md (item 6, open nits) marked BUILT. Changelog `changelog/fabric/2026-10-05-f-025.md`.

## Left
Nothing. The old `-SuiteArgs "--units a,b"` string form no longer splits, use `--units a,b` as plain arguments.

## Verify
- `go test ./internal/hubstore` passes (ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG cleared).
- `pwsh -File scripts/board-suite-remote.ps1 -DryRun --units "a b,c" --list` prints `arg[--units]`, `arg[a b,c]`,
  `arg[--list]`.
- Not run: room-toolchain against a room, board-suite-remote against a live room.
