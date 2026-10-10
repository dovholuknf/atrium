# f-close-stash-default

Stash is the default answer when a close finds work only a worktree has, and the hub clears old stashes.

- The close dialog (`closeCardNow` in `js/sharing.js`) puts the focus on the chosen answer, stash when `can_stash` is
  true. Enter presses "close it" with it. Without `can_stash` the dialog says "This room cannot stash to the hub", offers
  keep and delete, and keep is chosen. The terminals menu's clean up question lists delete before the default so Enter
  takes stash, or "keep it" when the room cannot stash.
- Enter in any dialog now PRESSES the last button instead of answering for it, so a button that holds (the close that
  shows its steps) runs. Before, Enter on the close dialog shut it without closing the card.
- `atrium close --yes` gives a worktree with no `--keep`, `--stash` or `--delete` the answer stash, or keep when the
  room cannot stash. It says which it chose.
- The hub's collect timer (`gitsync.Hub.Start`) runs `ExpireStashes`: every `refs/heads/stash/*` older than
  `git.stash_retention_days` (hub setting, default 30, 0 never) is deleted. Age is the later of the commit's committer
  date and the push log's time for the ref, so a stash of an old clean commit is not expired as it lands. Each expiry
  is written to the hub log and the audit log (`git-stash-expired`) with the repo, ref, card and sha, so
  `git branch <name> <sha>` brings it back until gc. Nothing outside `refs/heads/stash/` is touched.
- The setting needs no migration, it is a `hub_setting` row that reads 30 when absent. It is shown and set by
  `atrium rooms git settings --stash-retention-days N` and in `GET/PUT /_hub/git/settings` as `stash_retention_days`.
- The close result and the card's close event carry each stash's ref and `expires` day. The room does not hear the hub's
  setting, so `expires` assumes the default 30 days.

Pictures: `img/close-stash-default-before.png` and `-after.png` (focus on keep, then on stash), and
`img/close-stash-default-cannot-before.png` and `-cannot-after.png` (a room that cannot stash).

## Test plan

## @LETTER@. A close stashes by default

### @LETTER@1. Enter stashes

1. Close a card whose worktree has uncommitted changes, from the board, on a room that can stash.
2. Look at the dialog, then press Enter.

**Expected:** the stash choice is selected and focused, the card closes, and the toast names `stash/<card>/<branch>` and
the day it goes. Delete was never selected.

### @LETTER@2. A room that cannot stash

1. Close such a card on a room whose `git.push` is none.

**Expected:** the dialog says the room cannot stash, offers keep and delete only, keep is selected, and Enter keeps the
worktree.

### @LETTER@3. The CLI

1. Run `atrium close <card> --yes` with no `--keep`, `--stash` or `--delete`.

**Expected:** it says it is stashing each worktree, and the result lists the stash with the day it is removed.

### @LETTER@4. The hub clears old stashes

1. On a test hub, run `atrium rooms git settings --stash-retention-days 1` and wait for the next collect pass.
2. Look at the hub log and the audit log.

**Expected:** a stash older than a day is gone and each removal names the repo, ref, card and sha. A younger stash, and
every other branch, is still there. With `--stash-retention-days 0` nothing is removed.
