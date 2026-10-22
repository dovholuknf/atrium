Docs: none, docs/fabric/hub-forge-design.md and docs/release/packaging.md are fixed on the branch

## Test plan

## @LETTER@. Atrium's clones no longer carry the origin push guard

### @LETTER@1. A new clone has no origin pushurl

1. Call `atrium_git_clone` for a repository not yet in the scm folder.
2. Run `git config --get-all remote.origin.pushurl` in the clone.

**Expected:** nothing is printed. `git config atrium.clone` is `made`, and `git remote get-url hub` is the room's
forwarder.

### @LETTER@2. An old clone loses the guard on the next sync

1. On a clone in the room's git root, run `git config remote.origin.pushurl atrium-refused://origin-push`.
2. Sync it from the hub (or call `atrium_git_clone` for it again).

**Expected:** `git config --get-all remote.origin.pushurl` prints nothing, and the sync reports nothing about origin.
The operator's own `git push origin <branch>` from any worktree of the clone no longer fails with
`'remote-atrium-refused' is not a git command`.

### @LETTER@3. The operator's own pushurl survives

1. On a clone, run `git config remote.origin.pushurl git@github.com:me/r.git`.
2. Sync it.

**Expected:** `git config --get-all remote.origin.pushurl` still prints `git@github.com:me/r.git`.

### @LETTER@4. Agents still push only to the hub

1. In a card, run `git push origin <branch>`.

**Expected:** the PreToolUse hook refuses it, the same as before this change.
