## Test plan

## @LETTER@. A room's agent files stay off the system drive

### @LETTER@1. The Windows grant

1. On a Windows room with a work drive, follow the Windows block in `docs/room-accounts.md`.
2. As the room's account, create a folder at the root of the work drive, then one under the agent folder.

**Expected:** the root is refused, the agent folder works, and a Claude Code session writing under it raises no
"could not be determined" prompt.

### @LETTER@2. Caches and scm_root

1. Set `scm_root` and the npm cache as the new section says.
2. Open a pull request link and run `npm ci` in its worktree.

**Expected:** the worktree, `node_modules` and the npm cache are all on the work drive, and nothing new is under the
account's profile on C:.
