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

### @LETTER@3. Phase 3, the meter endpoint: below and past the threshold

1. File seven `docs` items whose `feature:` has no work row, then `GET /_hub/doc-debt`.
2. File an eighth.

**Expected:** the first answer has `items: 7`, `crossed: false`, `threshold_items: 8`, `threshold_days: 10`. The second has
`crossed: true`.

### @LETTER@4. Phase 3: the age starts at landed

1. File a `docs` item whose feature has a work row that has not landed.
2. Read the meter.
3. Let the feature land and read it again.

**Expected:** at first the item is `pending` and not in `items`. After landing it counts, with `from: landed` and its age
from that step. An item with no work row says `from: filing`. A dropped feature's item never counts.

### @LETTER@5. Phase 3: the settings move the line

1. Write `doc_debt.items` and `doc_debt.days` as hub settings, for example 4 and 5.
2. Read the meter.

**Expected:** `threshold_items` and `threshold_days` follow. Three or more items with the oldest at the days threshold
cross it, and two items however old do not.

### @LETTER@6. Phase 4: a touched page shows the hint

1. Land a feature, then file a `docs` item for it naming `README.md` and `docs/cli.md` under `stale:`.
2. Commit a change to `README.md` on the hub mirror's claude/main, and let the mirror pass run.
3. `GET /_hub/doc-debt`.

**Expected:** the item carries `landed_sha` and `changed: [{path: README.md, sha, date}]` with the newest commit that
touched it. `docs/cli.md` is not in `changed`. The item is still open and still counted.

### @LETTER@7. Phase 4: no hint without a landed sha, a mirror or git

1. Read the meter with an item whose `feature:` has no work row.
2. Read it again with the repository not held by the hub (or `flow_repo` unset with several held).

**Expected:** the hand-filed item has no `landed_sha` and no `changed`. With no mirror every item answers as before, with
no `changed`, and the answer is a 200 and not an error.
