Docs: none, the mirror rule is written into docs/fabric/git-sync-design.md and AGENTS.md by this change

## Test plan

## @LETTER@. A merge worker lands claude/main by fast-forward

### @LETTER@1. Fast-forward lands and the checkout follows

1. As the recorded merger, run `git push hub <sha>:claude/main` where `<sha>` descends from the hub's `claude/main`.
2. Wait for the next mirror pass (30 seconds).

**Expected:** the push is accepted. The orchestrator's checkout has `claude/main` at `<sha>`, fast-forwarded when it is
checked out clean, or its ref moved when it is checked out nowhere. The hub's `claude/main` stays at `<sha>`.

### @LETTER@2. Anything else is refused

1. Push a sha that does not descend from the hub's `claude/main` to `claude/main`.
2. Push to `main`, and push a tag.

**Expected:** each is refused with a sentence that says why, and nothing moves.

### @LETTER@3. A dirty or diverged checkout is left alone

1. Land a fast-forward while the checkout's `claude/main` worktree has an uncommitted change, then again with a local
   commit on `claude/main` that the hub lacks.

**Expected:** the checkout is not touched, the hub's `claude/main` does not go back, and the hub log and the mirror's
error on the board say the checkout is not in step until it is.
