## Test plan

## @LETTER@. The flaky suite runs first

### @LETTER@1. The needs graph

1. Run `yq '.jobs | to_entries | .[] | .key + " <- " + (.value.needs // [] | tojson)' .github/workflows/ci.yml`.

**Expected:** the three flaky jobs need nothing, each check job needs its own platform's flaky job, rollup needs all six and notify needs rollup.

### @LETTER@2. A red flaky group stops the checks

1. On a pushed branch, make one flaky test fail on one platform.

**Expected:** that platform's check jobs are skipped, the other platforms' flaky and check jobs still run, and the notify body says `failure`.
