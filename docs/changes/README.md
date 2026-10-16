# Pending changes

Every merge used to conflict on `CHANGELOG.md` and `docs/test-plan.md`, and several workers picked the same test-plan
letter. So a branch does not touch either file. Each item ships ONE file here, `docs/changes/<item>.md`, and the
merger folds it in with `scripts/fold-changes.ps1`.

The changelog is not written here any more. `CHANGELOG.md` is frozen. A change writes its own entry file,
`changelog/<dept>/<yyyy-mm-dd>-<item>.md`, of 1 to 5 lines: what changed for a user, and the item id. See
`changelog/README.md`.

## What you write

- The file is named by item id: `docs/changes/u-007.md`, `docs/changes/77b.md`. No item id, use a short slug.
- Do NOT edit `CHANGELOG.md` or `docs/test-plan.md`, and do NOT pick a test-plan letter.
- One section, with this exact heading line: `## Test plan`. It is required.
- The test-plan section starts with `## @LETTER@. Title` and its steps are `### @LETTER@1. Step`, `### @LETTER@2. Step`.
  Type `@LETTER@` literally. The merger substitutes the next free letter.
- Wrap at 120. No em-dashes, no semicolons in prose.

Example, `docs/changes/u-099.md`, next to its entry `changelog/ui/2026-09-29-u-099.md`:

```markdown
## Test plan

## @LETTER@. A card can be pinned

### @LETTER@1. Pin and restart

1. Pin a card on the board.
2. Restart the daemon.

**Expected:** the card is still pinned and still first in its column.

### @LETTER@2. Unpin

1. Unpin it.

**Expected:** it returns to its sorted position.
```

## Docs debt: the `Docs:` line and the impact block

Put one line at the top of `docs/changes/<item>.md`, above `## Test plan`: `Docs: d-<n>` or `Docs: none, <why>`.

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
Design: `docs/rnd/doc-debt-from-features.md`. The meter, the fold warning and the landed check are later phases.

## An older file with a `## Changelog` section

Files written before the freeze may still have one. The fold does not refuse them, and it never writes that section
into `CHANGELOG.md`. It moves the section as it stands into `changelog/<dept>/<today>-<item>.md`. The dept comes
from the item prefix: `u` ui, `t` terminal, `r` runtime, `f` fabric, `m` merge. An item with no prefix (`77b`) needs
`-Dept <director>`, naming the director whose branch it came from, and the fold stops without it. It warns when the
moved entry is longer than 5 lines. `@LETTER@` in that section is still refused.

## What the merger runs

```
pwsh scripts/fold-changes.ps1 -DryRun           # say what it would do
pwsh scripts/fold-changes.ps1                   # do it, then commit
pwsh scripts/fold-changes.ps1 -Item u-099       # fold one file
pwsh scripts/fold-changes.ps1 -Dept runtime     # dept for a leftover changelog section on an unprefixed item
```

For each file it takes the letter after the highest one in `docs/test-plan.md` (`A`..`Z`, then `AA`, `AB` and so on),
substitutes it for `@LETTER@`, appends the section to the end of `docs/test-plan.md`, moves any leftover changelog
section to `changelog/`, and `git rm`s the file. It prints each letter it chose. A malformed file stops it before
anything is written. With nothing pending it prints so and exits 0.

## Worker rules

**77c. Merge before you report.** Before reporting done, merge `claude/main` into your branch, resolve any conflicts
yourself, and pass your targeted checks on the merged result. A conflict found by the merger costs a round trip that
you can do in place, with the context you hold.

**77g. One report per batch.** The merger sends ONE report to the orchestrator for a whole batch, and nothing per
merge. Workers report to the merger, not the orchestrator.
