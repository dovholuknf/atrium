# termFollowBottom flake (macOS, board-2, run 42115737402)

## PROVEN

- The error `Cannot read properties of undefined (reading 'onmessage')` comes from `window.__say`, where
  `sock = __socks.filter(open and url matches).pop()` is undefined when no matching socket is open yet.
- The test called `__say` after a fixed 50ms idle following `attachTask("fb-a")`, and after a fixed 700ms in `__goto`.

## INFERRED

- On a loaded macOS runner the page had not opened the attach socket within those waits. Not run here: this
  machine has no playwright, so the fix is INFERRED and unexecuted (syntax checked only).

## Fix

`termFollowBottomSection` now waits with `waitForFunction` for an open socket for the card before each `__say`
that follows an attach (fb-a at open, fb-b and fb-a in the back step). No timeout was widened.
