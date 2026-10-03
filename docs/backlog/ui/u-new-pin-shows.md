# u-new-pin-shows: a pinned card is never removed by a hide pill

Filed from the desktop board's terminals list, 2026-10-02: a pinned card on a remote room did not show after a hub
reload.

## What and why

A pin says "always here". The `hide inactive` pills (agents, subagents) still removed a pinned row: a pinned subagent
went under the subagents pill, which defaults ON, and a pinned cold agent went under the agents pill. A card that was
pinned after losing its subagent tags is the case the report started from.

## What changed

- `sessionHiddenBy` (`js/terminal-list.js`) returns false for any pinned row. A pinned row that has exited is still
  drawn cold. Neither pill removes it.
- The pill's counts (`agentHideable`, `subHideable`, `agentHidden`, `subHidden`) never count a pinned row, so
  `subagents (1)` means one row is out of view. The bucket heading reads a plain count while any row is pinned;
  a folded bucket keeps that count on its header.
- Comments in `js/terminal-list.js` and the old hide-strip test, which asserted the opposite, now say and assert this.

## What was not changed

- The `!t.offline` filter in `renderTermList`. A card from a room that is not answering is not a terminal row, pinned
  or not.

## Finding the original cause

A headless run (`HEADLESS_ONLY=pinShows`) serving the reported row shape (runner `opencode`, `needs-input`, supervised,
pinned, no tags, a Windows home directory as worktree, `room~uuid` id) drew it in every pill, grouping and sort
state. It vanished only with `offline` true, with an `origin:agent` tag under the default-on subagents pill, or cold
under the agents pill. The last two are fixed here. If the row still goes missing, check the browser for an offline
flag on the row or a folded `*pinned` entry in `atrium.folded`.

## Test

`HEADLESS_ONLY=pinShows`, plus the hide strip in `core2` (`HEADLESS_UNITS=core,core2`) and `termBox`.
