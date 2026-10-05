# r-new-forge-alert-test-order

## Cause
Not the real `gh` and not state shared with another test. The test already uses a fake forge and a fake runner. It also failed with `-run` alone on sg4. `forgeAlertID` was `forge|tool@host|<UnixNano in base 36>`. The Windows clock is coarser than two raises in a row, so the second raise (after the success ended the first alert) got the same id as the first. A growler id is raised once ever, so the store recorded a duplicate and `raised` read as two rows with one id, which is the "duplicate alert" with the "gh is not logged in" body.

## Fix
This is a production fix, since the id collision is a real bug on a coarse clock and not a test artefact. `forgeAlertID` in `internal/link/forgeroute.go` now appends `.` and a package-level atomic sequence number, so every raise has its own id. The `forge|tool@host|` prefix that `openIn` matches on is unchanged. No test change was needed. Changelog: `changelog/runtime/2026-10-04-r-new-forge-alert-test-order.md`.

## Runs
`go test ./internal/link -count=1` with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` cleared, on sg4:
1. ok, 453.9s
2. ok, 315.1s
3. ok, 308.7s

gofmt reported nothing. The whole package passed, so the known pre-existing failures did not show here.
