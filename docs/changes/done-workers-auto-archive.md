Cause: the sweep archived only `dead` cards, so a done worker card had no timer and stayed until `atrium archive-workers` was run by hand.

## Test plan

## @LETTER@. Done worker cards archive themselves

### @LETTER@1. A finished worker leaves the board after a day

1. Launch a worker with atrium_launch_worker and let it finish, so its card is done.
2. Wait past `archive_workers_after` (default 86400 seconds), or set it to 60.

**Expected:** the card leaves the board and is still in history. Its worktree and branch are untouched. A pinned card, a
director, the orchestrator and a card you launched yourself stay.

### @LETTER@2. Off

1. Set `archive_workers_after` to `off`.

**Expected:** done worker cards stay until `atrium archive-workers` is run.
