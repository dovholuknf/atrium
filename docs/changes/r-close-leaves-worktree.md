# r-close-leaves-worktree: a close frees what it says it frees

clint closed openziti/ziti-console #967 on sg3 and found three things wrong. This is the one fix pass.

## What changed

1. **Worktree not freed, reported freed.** `git worktree remove` dropped the worktree and its tracked files, and the
   ignored `node_modules` stayed on disk (about 0.41 GB). atrium recorded freed with 26 MB, the size from an earlier
   measure that never saw the ignored tree. Now `removeWorktree` (`internal/api/close.go`) checks the path
   after git is done. When it is still there, `removeLeftover` (`internal/api/worktreeleft.go`) deletes the rest with
   the `\\?\` long-path form on Windows (`longPath`), and the row is freed only when the path is gone. A path that is
   still there stays live with an error naming how many entries are left and the first few. The size is measured again
   just before removal, ignored files included.
   The leftover delete is guarded: it runs only when git no longer lists the path as a worktree of the repo, never on a
   path under three components deep, and never on a path that holds the repo. Files still never leave a card through
   anything but git and the inventory's own path.
2. **Pulls row never changed.** `js/pulls.js` read only `state`, so an archived row kept its red failed words and
   retry, log, move and close. A row with `archived_at` now reads `closed`, says `closed <when> ago`, draws no actions,
   and carries `data-state="closed"`. The `open` filter drops an archived row at once, including one an event carried.
3. **Still on the terminals list.** A close now adds the tag `atrium:closed` to the card. The terminals tab leaves out a
   card with that tag and no session. The attached one stays, like every other hide there.

## Decision: show stopped does not list closed cards

`docs/changes/card-menu.md` and the card menu interview (Q on stop) define show stopped as the list of cards you
stopped so they can be resumed. A close frees the worktree, branch and overlay, so a closed card has nothing to resume.
Listing it would offer a resume that starts an agent in a folder that is gone. So a closed card is not on the list by
default and not with show stopped on. If the card gets a session again (it is supervised), the tag no longer hides it.

## Tests

- `internal/api/close_leftover_integration_test.go`: a close of a card whose worktree holds an ignored `node_modules`
  past 300 characters leaves no folder and counts its bytes, `removeLeftover` removes such a tree and refuses a path
  git still lists, and a closed card carries the tag.
- `scripts/check-pulls-closed.js`: a closed row reads closed with when, offers nothing, and leaves the open filter.
- `scripts/test-board-headless.js`, section `termMenu`: a pinned closed card is not listed with show stopped off or on.

## Test plan

## @LETTER@. Closing a card frees everything

### @LETTER@1. Ignored folders go with the worktree

1. Open a PR card on Windows and run `npm install` in its worktree.
2. Close the card from the terminals menu.

**Expected:** the worktree folder is gone from disk, `node_modules` with it, and the close lists the full size. If
something cannot be removed the row stays live and says how much is left.

### @LETTER@2. Pulls row

1. Close the card of a failed review, with the pulls filter on all.

**Expected:** the row reads closed with when, has no retry, log, move or close, and is gone from the open filter.

### @LETTER@3. Terminals list

1. Close a card, then turn show stopped on.

**Expected:** the closed card is not listed in either case.
