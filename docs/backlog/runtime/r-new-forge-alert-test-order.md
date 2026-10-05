# r-new-forge-alert-test-order. The forge alert test fails in a package run and passes alone

Status: filed by the orchestrator 2026-10-04 23:45. Owned by @runtime. Not started.

`TestTheHubRaisesTheForgeAlertOnceAndEndsItOnSuccess` (internal/link, from r-hub-forge 99164911) fails on every
`go test ./internal/link` on sg4 and passes with `-run` alone. The failure reads "gh is not logged in" and shows a
duplicate alert, so the test reaches the real `gh` or shares state with a test that ran before it.

## Done looks like

- The test never runs the machine's own `gh`, and holds no state another test in the package leaves behind.
- `go test ./internal/link` passes it three runs out of three on sg4.
