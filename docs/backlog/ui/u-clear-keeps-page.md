# u-clear-keeps-page

A clear (Claude Code's clear and compact, and new-context on Ctrl+Alt+N) sends erase-display, and xterm blanked the rows in place, so the page that was showing never reached the scrollback.

## What landed

`keepPage` in `internal/api/web/js/terminal.js`, called from a CSI `J` handler for params 2 and 3 and from the new-context key.

- It scrolls the rows up to the last one with text off the top, so no blank lines go into history. xterm 5.5 has no public call for it and a handler cannot `write`, so it uses `_core._inputHandler`; if that is missing the clear behaves as before.
- 3 is swallowed without a push, so a lone 3 leaves the rows and history alone; the usual 2 then 3 has already kept the page. 2 proceeds to xterm's own erase over rows that are already blank.
- Not on the alternate buffer, not with a scroll region set, not for J 0/1, and not when the page is the one pushed last.
- Ctrl+Alt+N pushes a copy and puts the rows back, so the screen is not blanked while the session writes its handoff.

## What Claude's repaints send

The captures under `internal/daemon/testdata` (three scrollbacks of 67 to 112 KB and the frame files) hold one `ESC[2J` each, never `ESC[3J`, and tool and spinner frames repaint with cursor moves and line erases. The daemon's own test frames do prefix a frame with `ESC[H ESC[2J`, so a TUI that does that per frame would push every changed frame; the same-page guard stops only identical ones. Worth watching on a real session; the fallback is to limit the push to a 2J that is followed by a 3J.

## Replay

The room's ring holds the raw bytes, and the replay is the history flattened (erases dropped, so the cleared page is there as plain lines) followed by the current screen, which starts with `ESC[2J`. That 2J goes through the same handler, so the last page of replayed history is kept instead of blanked. `clearKeepsPage` writes the same stream twice and asserts the same history. No room change.

## Not done

`reselect` fails on landing already (`reading 'rooms'` of undefined), unrelated.
