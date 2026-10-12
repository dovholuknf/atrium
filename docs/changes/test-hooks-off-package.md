## Test plan

## @LETTER@. No test changes a package variable

### @LETTER@1. The check is clean

1. Run `go run scripts/check-test-globals.go`.

**Expected:** it prints nothing and exits 0.

### @LETTER@2. The check catches one

1. Add `var probe = 1` to a package and `probe = 2` inside a test in the same package.
2. Run `go run scripts/check-test-globals.go`.

**Expected:** it names the file and line, and exits 1.

### @LETTER@3. The suites still pass

1. Run `go test -tags "integration git" -count=1 ./internal/...`.

**Expected:** every package passes, the same as before the move.
