# Pending changes

Every merge used to conflict on `CHANGELOG.md` and `docs/test-plan.md`, and several workers picked the same test-plan
letter. So a branch does not touch either file. Each item ships ONE file here, `docs/changes/<item>.md`, and the
merger folds it in with `scripts/fold-changes.ps1`.

## What you write

- The file is named by backlog item number: `docs/changes/77b.md`, `docs/changes/72.md`. No item number, use a short
  slug.
- Do NOT edit `CHANGELOG.md` or `docs/test-plan.md`, and do NOT pick a test-plan letter.
- Two sections, in this order, with these exact heading lines: `## Changelog` and `## Test plan`. Both are required.
- The changelog section is one entry in the format `CHANGELOG.md` already uses: `- **Title.** See ...`, a blank line,
  then an indented paragraph or two.
- The test-plan section starts with `## @LETTER@. Title` and its steps are `### @LETTER@1. Step`, `### @LETTER@2. Step`.
  Type `@LETTER@` literally. The merger substitutes the next free letter.
- Wrap at 120. No em-dashes, no semicolons in prose.

Example, `docs/changes/99.md`:

```markdown
## Changelog

- **A card can be pinned.** See `docs/backlog-2.md` item 99.

  Pinning a card keeps it at the top of its column. The pin is stored, so it survives a daemon restart.

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

Leave `@LETTER@` out of the changelog section: the fold refuses it there, since only the test-plan section is
substituted.

## What the merger runs

```
pwsh scripts/fold-changes.ps1 -DryRun     # say what it would do
pwsh scripts/fold-changes.ps1             # do it, then commit
pwsh scripts/fold-changes.ps1 -Item 99    # fold one file
```

For each file it puts the changelog entry at the top of `## Unreleased`, takes the letter after the highest one in
`docs/test-plan.md` (`A`..`Z`, then `AA`, `AB` and so on), substitutes it for `@LETTER@`, appends the section to the
end of `docs/test-plan.md`, and `git rm`s the file. It prints each letter it chose. A malformed file stops it before
anything is written. With nothing pending it prints so and exits 0.

## Worker rules

**77c. Merge before you report.** Before reporting done, merge `claude/main` into your branch, resolve any conflicts
yourself, and pass your targeted checks on the merged result. A conflict found by the merger costs a round trip that
you can do in place, with the context you hold.

**77g. One report per batch.** The merger sends ONE report to the orchestrator for a whole batch, and nothing per
merge. Workers report to the merger, not the orchestrator.
