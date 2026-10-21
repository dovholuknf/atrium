## Test plan

## @LETTER@. A runner's git token survives a room restart

### @LETTER@1. Reattached runner fetches

1. With `pty_host` on, launch a worker from a room and note that `git fetch hub` works in its terminal.
2. Restart the room (`atrium stop`, then start it) and let the worker reattach.
3. Run `git fetch hub` and `git push hub <branch>` in the same terminal, without relaunching. `atrium_git_push` for the same card also works.

**Expected:** both are accepted, not refused with "answers a card with its atrium token".

### @LETTER@2. An ended card stays refused

1. End the card, restart the room, then replay its old token against the forwarder.

**Expected:** refused.
