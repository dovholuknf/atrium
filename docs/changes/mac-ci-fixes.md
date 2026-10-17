## Test plan

No manual steps: the change is to a Go test, `TestOpenMakesTheWorktreeTheRowAndTheCardAndTiesThem`.

Run `go test ./internal/api -run TestOpenMakesTheWorktreeTheRowAndTheCardAndTiesThem -count=20` on macOS.

**Expected:** every run passes.
