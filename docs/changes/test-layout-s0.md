Docs: none, test tooling only. The design is `docs/rnd/test-layout-design.md`.

## Test plan

## @LETTER@. Test buckets and the planner

### @LETTER@1. The lint passes on the tree

1. Run `node scripts/check-test-buckets.js`.

**Expected:** it prints `check-test-buckets ok` with the counts of board files, Go files, units and buckets.

### @LETTER@2. A new file outside a bucket fails

1. Run `node scripts/check-test-buckets.js --probe-file internal/api/web/js/new.js`.
2. Run it again with `--probe-unit newUnit`.

**Expected:** each exits 1 and says the file or unit is in no bucket and to add it to `scripts/test-buckets.json`.

### @LETTER@3. The planner names the bucket

1. Touch `internal/api/web/js/pulls.js` and run `bash scripts/test-touched.sh --plan`.
2. Touch `internal/link/pulls.go` and run it again.

**Expected:** the first plan has a `bucket pulls` row and the `pulls` units. The second has `bucket hub/pulls` and the
`./internal/link` package.

### @LETTER@4. A file in no bucket falls back to inference

1. Add a new file under `internal/api/web/js/` that is in no bucket and run `bash scripts/test-touched.sh --plan`.

**Expected:** a warning says it is in no bucket and to add it to `scripts/test-buckets.json`. The units are inferred
as before.

### @LETTER@5. The self-test

1. Run `node scripts/test-touched-selftest.js`.

**Expected:** it ends `test-touched-selftest ok`, with one check per bucket and the lint's failures proven.
