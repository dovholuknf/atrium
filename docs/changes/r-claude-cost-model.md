Docs: none, this change is the doc `docs/runtime/claude-cost-model.md` and no code or board behaviour changes.

## Test plan

## @LETTER@. The cost model doc matches its script

### @LETTER@1. The blocks match

1. Run `node scripts/cost-model.js --check`.

**Expected:** it prints `33 blocks match` and exits 0.

### @LETTER@2. A changed price shows up

1. Change a price at the top of `scripts/cost-model.js`, then run `--check` again.

**Expected:** it exits non-zero and names the blocks that differ. Undo the change.
