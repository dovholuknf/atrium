# Review: u-clear-keeps-page (e45b841e..d6e70c94) and u-tray-head (e45b841e..b323bead), m1mini, 2026-10-02: OK

Two small @ui changes, clint's asks (not held by the pause), JS and CSS only. Unsigned.

## u-clear-keeps-page d6e70c94: OK

- **The approach.** `keepPage` pushes the visible rows into scrollback by `n` line feeds from the last row, where
  `n` runs to the last non-blank row, so no blank lines go into history. It goes through `_core._inputHandler`
  because a `write` from a handler lands after the chunk. If the private API is missing, it does nothing, and a clear
  behaves as before. That is a fair trade on a vendored xterm 5.5.
- **The guards.** It skips the alternate buffer (full-screen apps), a set scroll region, J 0 and J 1, and a page
  equal to the last one pushed (the repaint guard). `termPushed` resets on open and on new-context. Ctrl+Alt+N
  pushes a copy and restores the rows, cloned and copied back.
- **Replay.** A re-attach replays through the same handler, so history after a reconnect matches what was seen live.
  No room half is needed.
- **Tests.** `clearKeepsPage` fails with the handler disabled, and `termWear` and `bootClean` pass. I read them and
  did not run them, per the board rule.

Lows:
- **L1, a lone `ESC[3J` blanks the visible screen.** The handler calls `keepPage` for 3 as well as 2. ED 3 means
  "clear the scrollback", and never touches the visible rows. So a `3J` sent on its own (`tput E3`, or some tools on
  a resize) scrolls the visible page into history and leaves the screen blank, where a real terminal would leave it
  alone. In the usual `2J` then `3J` pair the page is already blank by the `3J`, so it is harmless there. Fix: for 3,
  swallow (`return true`) without calling `keepPage`. Test: write a page, send only `ESC[3J`, and expect the rows
  still on screen.
- **L2, say what swallowing 3J means.** A user running `clear` to wipe something they printed now keeps it in the
  board's history. The room's own scrollback already keeps the raw stream, so this adds no exposure. But the
  changelog should say "clear no longer empties the board's scrollback".

## u-tray-head b323bead: OK

One rule set in notify.css: the head wraps (`flex-wrap`, a row gap), the title keeps `flex: 1 1 8.5em`, and the
buttons are `flex: none` and a little tighter. No width can put one on another. CSS only. bootClean was still
running on @ui's side when this was read, which is fine for a style change.

Closed: none
Open: L1 (lone 3J), L2 (changelog line), both in u-clear-keeps-page

Verdict: OK e45b841e..d6e70c94 and OK e45b841e..b323bead, hub-ok and room-ok.

Quality: after the Sonnet switch, careful work on a fiddly part. The private-API use is guarded and explained, and
the repaint guard was thought of before it was needed.

## Re-read of u-clear-keeps-page at 31b6a8b0 (2026-10-02): OK e45b841e..31b6a8b0

- **L1, closed.** The J handler swallows 3 (`return true`) without calling `keepPage`, and 2 still pushes the page
  and then lets xterm erase. `clearKeepsPage` gains a lone-3J case (the rows stay on screen and history is 0), and the
  2J+3J pair still keeps the page.
- **L2, closed.** The changelog says the board's history survives a clear.

Closed: L1, L2
Open: none

Verdict: OK e45b841e..31b6a8b0, hub-ok and room-ok.
