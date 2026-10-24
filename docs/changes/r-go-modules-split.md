Docs: none, a CI check with no user-facing surface

## Test plan

## @LETTER@. The import layer check

### @LETTER@1. The graph is pinned

1. Run `go run scripts/check-imports.go`.
2. Add an import of `internal/store` to `internal/guard`, then run it again.

**Expected:** the first run prints `ok`, the second fails with `internal/guard -> internal/store (not allowed: ...)`.

### @LETTER@2. A new package must be listed

1. Add a package under `internal/` with no row in the table, then run the check.

**Expected:** it fails and says to add the package to the table in `scripts/check-imports.go`.
