## Test plan

## @LETTER@. One protocol for a worker's finish, report and stop

### @LETTER@1. A worker ends with done and one line

1. Launch a worker with a brief and read its first prompt.
2. Have it call `atrium_done` with a sha and a line of 5 words, then with a line of 21 words.

**Expected:** the prompt ends with "Then stop. Say nothing more." and names `atrium_done` for a stop. The launcher is
typed `done <sha>: <line>`. The 21 word line is refused and nothing is recorded.

### @LETTER@2. A stop answered with the same done is silent

1. Let a worker finish with `atrium_done`, then send it "stop" as `needs`.
2. Have it answer with `atrium_done` and the same sha.

**Expected:** the launcher gets no second notice and no "ended its turn without reporting".

### @LETTER@3. A resident card stays open

1. Launch a worker with `resident: true`.
2. Read its `atrium_done` description, call it, and try `atrium_cull` on it.

**Expected:** the description starts "Report that this task is finished". The card stays open after the done, is not
exited, and the cull is refused.

### @LETTER@4. launcher_told is honest

1. Have a worker call `atrium_done` while its launcher is an orchestrator.

**Expected:** the tool answers `launcher_told: "queued"` and the done is typed into the orchestrator's terminal at its
turn end. A launcher on another room answers `"relayed"`.

### @LETTER@5. A launched card pushes its launch branch

1. Have an orchestrator push `claude/x`, then launch a worker on a worktree of `claude/x`.
2. Have the worker push `claude/x` to `hub`.

**Expected:** the push lands and the worker owns the branch. A push of `claude/y` by the same worker is refused.
