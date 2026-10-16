Docs: none, this change is the doc-debt rule itself and `docs/changes/README.md` carries it.

## Test plan

## @LETTER@. A feature files its doc debt

### @LETTER@1. File a doc item

1. File with no id: `atrium backlog file --dept docs --title "docs behind u-099" --body -`, with an impact block on
   stdin (`feature:`, `changelog:`, `shas:`, `new:`, `stale:`, `updated:`).
2. Run `atrium backlog list --dept docs`.

**Expected:** the item has an id `d-001` (then `d-002`) and is listed.

### @LETTER@2. The tool says how

1. Read the `atrium_backlog` tool description.

**Expected:** it names the `docs` department and the six block lines.
