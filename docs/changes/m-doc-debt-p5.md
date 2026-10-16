Docs: none, the landed check is described in `docs/rnd/doc-debt-from-features.md` and nothing a reader names is left wrong.

## Test plan

## @LETTER@. The landed check on an item's detail

### @LETTER@1. A feature that said nothing about its docs

1. Land a feature whose `docs/changes/<item>.md` has no `Docs:` line above `## Test plan`.
2. Open the item on the Items tab, or `GET /_hub/work/<item>`.

**Expected:** the detail shows `docs: not said`, and the answer carries `docs.not_said`. Adding `Docs: d-<n>` or
`Docs: none, <why>` to the note at the landed commit (a later pass reads the same commit) removes it. A note the landed
commit does not hold, or a mirror that cannot be read, shows nothing.

### @LETTER@2. A doc worker's item lands

1. Launch a worker on a `d-<n>` doc item (kind `doc`) and let it reach `landed`.
2. Open its detail.

**Expected:** the detail offers `close d-<n>?` with `open d-<n>` and `not now`. Nothing is closed by the hub. `not now`
hides the offer on that browser, and closing the doc item removes it for everybody. No schema changed.
