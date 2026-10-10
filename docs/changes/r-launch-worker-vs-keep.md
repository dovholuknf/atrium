## Test plan

## @LETTER@. A work card is exited when its branch merges

### @LETTER@1. Worker launched with atrium_launch_worker

1. From a director, launch a worker with `atrium_launch_worker` in a worktree on a claude/* branch.
2. Open the card details.
3. Let the worker commit and call `atrium_done`.
4. Merge its branch into claude/main with `merged_cull_grace` set to `off`.

**Expected:** the details show `lifetime work`. After the merge atrium exits the runner. The card and the worktree
are still there.

### @LETTER@2. Worker launched with atrium_launch

1. Launch a worker with `atrium_launch`, let it report done, and merge its branch into claude/main.

**Expected:** the runner keeps running and the details show no lifetime. Atrium never exits it because of the merge.
