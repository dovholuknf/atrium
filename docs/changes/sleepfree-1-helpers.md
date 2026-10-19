## Test plan

## @LETTER@. Flat waits in tests can only go down

### @LETTER@1. The lint passes on the baseline

1. Run `go run scripts/check-test-waits.go`.

**Expected:** `ok`, exit 0.

### @LETTER@2. A new flat wait fails

1. Add `await p.waitForTimeout(300);` to a unit in `scripts/board-tests/chat.js` and rerun the lint.
2. Add `// wait: <why>` to that line and rerun it.

**Expected:** the first run names the file with its count and the baseline and exits 1, the second passes.

### @LETTER@3. A removed wait asks for a lower baseline

1. Delete one flat wait from a counted file and rerun the lint.

**Expected:** it exits 0 and says to run `go run scripts/check-test-waits.go -write`.
