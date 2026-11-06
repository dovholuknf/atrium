# Composer stays put

## Cause

The new-context panel (`#t-ctxbox`) is one element shared by every terminal, and nothing tied it to a card.

1. The x did not stay closed. `composeDismiss` closed the panel, then `composeStop` ran `composeNoEntry(null)`, which
   cleared `composeDismissedFor`. A stream row of the same cycle still in flight (held) then reopened the panel. A
   failed DELETE did the same.
2. It stayed over another card. Switching terminals never touched the panel, and `composeNoEntry` leaves an open panel
   alone when it holds text, so it was drawn over a card with no cycle.
3. It opened unasked. The room answers every refused `in` frame with `in-refused`, and `takeInRefused` opened the panel
   for any of them. xterm sends frames by itself (focus `ESC[I`/`ESC[O` on blur and show, mouse reports, answers to
   the runner's queries), sent quiet, so they reach the room during a cycle and were refused like typing.

## Change

- `internal/api/web/js/compose.js`: `composeDismiss` queues the typed text on the card, closes at once and keeps the
  card dismissed after the stop (`composeDismiss`). `composeHide` queues the text and takes the panel down when its
  card is not the shown one (`composeNoEntry`) or the terminal is left. `composeAsked` and `composeOperatorAt`: a
  refusal opens the panel only within 3s of the operator's own non-quiet input and not on a dismissed card.
  `composeQueue` is the old message POST of `composeSend`. A text that could not be queued is kept in `composeDrafts`
  and put back when the card shows it again.
- `internal/api/web/js/terminal-debug.js` `clearTermPane`: calls `composeHide` on every switch away.
- `internal/api/web/js/terminal.js` `keepShow`: calls `composeBar`, so a kept terminal shows its panel on return.
- `internal/api/web/js/terminal-links.js` `takeInRefused`: returns quietly unless `composeAsked()`.

## The test

`composerStaysPut` in `scripts/test-board-headless.js` (listed in `scripts/test-buckets.json`): the x closes the panel
and queues the text, a stale row does not reopen it, switching away hides it and queues the text on its own card,
switching back shows it and a card with no cycle shows none, a refused auto report does not open it and a refused typed
key does. The x check failed before the `composeDismissedFor` fix.

## How clint verifies

On a deployed room, open the board in a browser.

1. Start a new context on card A and open A's terminal. The panel "starting a new context" appears over A only once the
   cycle holds typing. Do not touch the keyboard: click around the terminal and switch focus to another window and back.
   It must not open by itself.
2. Type something in the panel, press x. The panel closes and stays closed for the rest of the cycle. The typed text is
   sent to A when the cycle ends.
3. Start another new context on A. While the panel shows with text typed, switch to card B's terminal. B shows no
   panel. Switch back to A: the panel is there, and what was typed was queued on A.
4. In the panel press "let me type" and Escape on another cycle: both still end the cycle.
