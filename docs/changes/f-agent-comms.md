## Test plan

## @LETTER@. A launched worker ends with one message

Uses a launcher and a worker on a room linked to a hub. Run the worker from a directory that is not a git checkout for
the artifact step.

### @LETTER@1. A commit on a hub branch is accepted from any directory

1. As the worker, commit in a worktree, then push the branch to the hub (`claude/<item>-w`) with `atrium_git_push`.
2. Change the worker's shell to a directory that is not the repository and call `atrium_done` with the sha.

**Expected:** the call is recorded and the launcher is told once. A sha no hub branch has is still refused.

### @LETTER@2. An artifact ends a worker with no commit

1. As the worker, write `report.md` in a plain directory and call `atrium_done` with `artifact` set to its path.
2. Call it again with a path that does not exist, then with both `sha` and `artifact`.

**Expected:** the first is recorded and the launcher reads `done <path>`. The other two are refused and record nothing.

### @LETTER@3. One message per end

1. As the worker, `atrium_say` the launcher `blocked: need a token`, then call `atrium_blocked`.
2. In a new turn call `atrium_done`, then `atrium_say` `done <sha>`.

**Expected:** the launcher gets one message each time. The second say answers `dropped` with a note saying the end was
already made. The `atrium_say` tool description points at `atrium_done` and `atrium_blocked`.
