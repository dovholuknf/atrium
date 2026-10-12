No Go test assigns a package variable any more. Hooks, delays, limits and clocks are fields on the struct that uses them, so one test's timer cannot fire into the next test's daemon. test-hooks-off-package.
`scripts/check-test-globals.go` fails CI when one comes back.
