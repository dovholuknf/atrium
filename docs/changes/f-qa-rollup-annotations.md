Docs: none, a CI run page has no reference page

# f-qa-rollup-annotations

`scripts/ci-rollup.js` prints `::error`, `::notice` and `::warning` workflow commands to stdout and appends the table to
`--summary FILE` (`$GITHUB_STEP_SUMMARY` in the workflow). `--out` and `--local` work as before.

## Test plan

## @LETTER@. The ci run page leads with the failing units

### @LETTER@1. A failing run

1. Open the run page of a ci run with a failed job.

**Expected:** the Annotations block at the top lists one error per failing unit, NEW first, then units that passed on
retry, at most 10 annotations in all, with "and N more, see the table" when cut and a notice of totals. The table is
still in the `rollup` job summary.

### @LETTER@2. A green run

1. Open a green ci run.

**Expected:** one notice "green, all N jobs passed", and no errors.
