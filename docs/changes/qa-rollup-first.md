Docs: none, a CI summary page has no reference page

# qa-rollup-first

The `summary` step of each `check` job in `ci.yml` writes one line (job, status, artifact name) to
`$GITHUB_STEP_SUMMARY`. The full `summary.md` is in the artifact only. GitHub orders job summaries by job completion
time, so the `rollup` job, which needs the matrix, is still last, but after twelve lines and not twelve reports.

## Test plan

## @LETTER@. The run page is short

### @LETTER@1. Per-job lines

1. Open the run page of a ci run.

**Expected:** each `check (...)` job shows one line with its status and the artifact name, and the `rollup` section
follows them. Downloading that artifact still has `summary.md`.
