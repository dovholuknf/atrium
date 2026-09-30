# u-new-review-5e68b83f. Ready-alert spam fix: review

Status: open. Filed by @review 2026-09-30. Read-only review of 5e68b83f (merge of `claude/u-ready-spam`), which
makes a ready alert wait for 5 seconds of quiet, ring once per wait, and say nothing in the focused window that shows
the card. Owned by @ui.

Two of the three hold as written:

- **Once per wait.** The rung memory is keyed on bare id plus `waiting_since` (`internal/api/web/js/notify.js`,
  `rungKey`), and the room keeps `waiting_since` across a `needs-input` to `needs-permission` flip
  (`internal/store/tasks.go:700-704`). So a card that leaves the waiting set for a pass and comes back is the same
  wait and stays quiet, which is the 17-rings case. A reload re-baselines through `rememberRung` and does not ring.
- **5 seconds of quiet.** `holdWaits` holds every fresh wait for at least `quietMs()` from when it was first held,
  `fireWaits` drops a wait that ended while held, and activity restarts the clock. Activity is a task event whose
  `activity` changed (`settings-spine.js`, `hearActivity`) and, in the window attached to the card, pty output
  (`terminal-links.js`, `feedReadyQuiet`).

## 1. Medium. The focused window is silent only when it is the only window deciding

The new filter in `announce` drops a ready alert when THIS window is focused and shows the card. Every other open
window runs its own `alerting` with its own copy of that rule, and in those windows `focusIsHere()` is false. So:

- a second board tab, unfocused, on the kanban view: it announces the wait, `play` rings in it, and `notify` case 2
  ("you are looking at another atrium window") posts `win-toast` to the focused window. The focused window then toasts
  about the card it is showing, because the `win-toast` handler calls `toast` without asking `termWatching`.
- a pop-out of the card, unfocused, while the board is focused on the terminals view with the same card attached:
  the pop-out owns its card's alerts, so it rings and forwards the toast to the board.

Two windows open is the ordinary case on sg4 (the board plus pop-outs). So "no alert in the focused window that shows
the card" holds only when no other window of the board is open.

Fix: the `win-toast` receiver drops a `waiting` or `looksidle` toast whose `taskFor` it is watching, and logs it. For
the sound, a window that is focused and watching a card says so on `soloBus` beside `win-focus`, and `announce` drops
that card when the focused window claims it, before `play`.

## 2. Low. Without the terminal attached, quiet means only that the card's badge stopped changing

In a window that does not have the card's terminal attached, the only activity it hears is a task event whose
`activity` changed. An agent printing steadily inside one tool call does not change its badge, so it reads as quiet
and the alert fires on time while the screen is still moving. The code names the fix already (`quietFor`, "the ONE
PLACE to feed the room's real pty-output time once it is published"). Listed so the room field gets a backlog item.

## Tests

Not run here. Board and headless checks are @ui's (`scripts/test-board-headless.js` gained 157 lines in this merge).
A case for finding 1 needs two documents on one `BroadcastChannel`: the focused one attached to card X in the
terminals view, the unfocused one on the kanban. X goes ready, and the focused one must neither toast nor hear a
sound.

19e2f840 (`u-popout-notify`, per-window switches) merged right after and touches the same file. It does not change
either finding.
