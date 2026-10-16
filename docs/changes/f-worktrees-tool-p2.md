## Test plan

## @LETTER@. atrium_worktrees audits worktrees in one call

### @LETTER@1. The audit

1. From a director session, call `atrium_worktrees` with `action: "audit"` in a repo that has several worktrees.
2. Make one worktree dirty with a modified file and put an untracked `BRIEF.md` in another.

**Expected:** a header `claude/main <sha8> from hub, <n> worktrees` (or `from local` where the hub does not sync the
repo), then one line per worktree. The dirty one reads `dirty 1`, the other `clean +BRIEF`. A worktree atrium did not
make reads `keep: not atrium's`, the main checkout reads `keep: main checkout, protected branch`, and a worktree whose
directory is gone reads `(missing)` and `removable: prune`.

### @LETTER@2. Narrowing and refusals

1. Call it with `prefix` set to a folder, then with `paths` naming one worktree, then with both.
2. Call it with `repo: "host/org/repo"` for a repo the room has no clone of.
3. Call it with `action: "remove"`.

**Expected:** the first two narrow the lines, both together are refused, an unknown repo is refused with `this room has
no clone of host/org/repo`, and `remove` answers that it is not built yet. A worker session does not list the tool.
