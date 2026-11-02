# Pending changes

Every merge used to conflict on `CHANGELOG.md`, so a branch does not touch it. Each item ships ONE file here,
`docs/changes/<item>.md`, and the hub's landed check reads its `Docs:` line.

A change also writes its own entry file, `changelog/<dept>/<yyyy-mm-dd>-<item>.md`, of 1 to 5 lines: what changed for
a user, and the item id. See `changelog/README.md`. A weekly roll-up card owns `CHANGELOG.md`, and a branch never
edits it.

## What you write

- The file is named by item id: `docs/changes/u-007.md`, `docs/changes/77b.md`. No item id, use a short slug.
- Do NOT edit `CHANGELOG.md`.
- There is no test plan section. Older files still carry one and the landed check ignores it.
- Wrap at 120. No em-dashes, no semicolons in prose.

## Docs debt: the `Docs:` line and the impact block

Put one line at the top of `docs/changes/<item>.md`: `Docs: d-<n>` or `Docs: none, <why>`.

A change is a doc-debt feature when it writes a changelog fragment AND adds, renames, removes or changes the default
of something a reader can name: a verb, a flag, an endpoint, a setting, a tab, a button, a file format. A fix that
makes the code match what the docs already say is not one, and a refactor is not one. The worker decides.

In your last step, before you report done, fix the reference pages you can change without a merge fight (one
`website/docs/*.md` page, a package doc). Do NOT edit `README.md`, `FEATURES.md` or `docs/how-atrium-works.md`: every
branch that does conflicts. If pages are left wrong, file ONE item in department `docs` (id `d-<n>`) with
`atrium_backlog` or `atrium backlog file --dept docs`, whose body is only this block:

```
feature: <item id of the feature>
changelog: changelog/<dept>/<date>-<item>.md
shas: <branch tip when filed>
new: <one line each, verb, flag, endpoint, setting or tab>
stale: <path>  <one line, what it says wrong>
updated: <paths the branch already fixed>
```

Nothing left wrong means no item, and `Docs: none, <why>`. Never copy the changelog sentence into the block.
Design: `docs/rnd/doc-debt-from-features.md`. The meter and the landed check are later phases.

## Worker rules

**77c. Merge before you report.** Before reporting done, merge `claude/main` into your branch, resolve any conflicts
yourself, and pass your targeted checks on the merged result. A conflict found by the merger costs a round trip that
you can do in place, with the context you hold.

**77g. One report per batch.** The merger sends ONE report to the orchestrator for a whole batch, and nothing per
merge. Workers report to the merger, not the orchestrator.
