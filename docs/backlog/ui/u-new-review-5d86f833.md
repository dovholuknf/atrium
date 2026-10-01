# Review of u-new-details-debug-section 5d86f833 (@ui, a debug section in the terminal's details drawer)

Range `d94090f6..5d86f833`, two commits on claude/fix-joined: 9d83de18 ("WIP details debug section, parked for
fix-term-box") and 5d86f833. Board only: `js/peek-debug.js` (new), `js/peek.js`, `js/typing.js`, `js/inputlag.js`,
`css/terminal.css`, `index.html`, the `termDebug` headless section, docs. Order: D:\tmp\ui-order-details.txt, which
folds in u-new-gate-readout-only-when-blocking and, for the two switches only, u-new-browser-prefs-every-window.

## Against the specs

- **Only on the strip's drawer.** The section is its own element inside `#t-drawer`, so the card hover and the card
  menu, which fill other hosts through `peekFill`, never carry it. `toggleTermDrawer` calls `dbgDrawer(open && !!id)`,
  and `termDrawerFollow` goes through `toggleTermDrawer(true)`, so a drawer following the terminal to another card
  starts a fresh poll under a new sequence. A poll in flight for the old card is dropped by the `seq` and `termTask`
  checks.
- **The live gate and the countdown.** `dbgGateText` says "closed, opens in Ns" when the count is 0, nothing is
  unsure, and the last key is under 2s old. That is exactly the third branch of `gateLocked`
  (internal/daemon/supervisor.go:1120), and `DBG_QUIET_MS` matches `peerGateIdle` (supervisor.go:1059). Any other
  closed reason is said as the daemon gives it. The rows give chars, paste, unsure, last key, and held count and
  sender.
- **Held count and sender.** `typingHeld` reads `activity.held_peer`, `held_for` "line" and `held_count`, which the
  daemon sends (internal/daemon/activity.go:136-141) and heldline.js already reads the same way.
- **The gate line speaks only when blocking.** `paintTyping` hides the line unless a message is held for the line and
  the gate is closed, or the poll failed. It never copies the line. On a line that looks empty it quotes what atrium
  thinks is there, cut at 40 characters, or gives the daemon's unsure reason. That matches the spec's wanted shape.
  Nothing held means no line, which is one of the two options the spec offered.
- **Outside click and Escape.** A capturing `pointerdown` on the document closes the drawer unless the target is in
  `#t-drawer` or `.term-help`. The caret (`#t-expando`) is inside `.term-help`, so a click on it does not close and
  then reopen. Nothing is cancelled, so a click in the terminal still focuses it. The capturing `keydown` stops and
  prevents Escape at the document, so xterm's textarea never sees it.
- **Every window follows.** Both `storage` listeners re-read the key and go through the existing toggles, which are
  guarded (`!!on === lagOn` returns early in `toggleInputLag`, and `on !== typingOn` before
  `toggleTypingReadout`), and both toggles now set their checkbox. A listener does not write back a changed value, so
  windows do not echo to each other. `key === null` (a `clear()`) is handled. The browser-only toggle is used, not
  `saveInputLag`, so the second window does not POST the setting again.
- **The settings dialog points at the drawer.** Both boxes were removed from settings and a hint names the drawer,
  which is one of the two options the spec offered.
- **The termDebug case** covers the drawer-only section, the countdown, the rows, both switches in the section, no
  poll after close, the blocking line and its looks-empty quote, inside click, outside click, Escape, and a second
  page on one context following both switches on and off. I read it and did not run it, since board checks are
  @ui's.

## @ui's question: the 500 ms poll

Keep the poll. It runs only while the drawer is open in one window, uses an endpoint that exists for this purpose
(`typing()` takes `typeMu` once and returns), and is the same thing the gate line has always done at the same
interval. A push would mean the daemon publishing gate state, which changes on every keystroke, onto the event
stream for a debug view. That is work on the keystroke path, which the gate's own design rules out (supervisor.go
comment on `typing`: "the keystroke path pays nothing for it"). It is not a breach of the event-driven rule worth
fixing in Go. Two refinements, as low 2 and nit 1 below.

## Findings

1. **Medium. The input lag box lost its sync with the machine.** The box used to live in settings, and opening
   settings ran `loadHousekeeping`, which calls `syncInputLag(s)`. That made the box show the hub's and room's real
   state (ticked in another browser shows ticked here, and starts timing here) and showed the
   `ATRIUM_DEBUG_INPUTLAG` pinned note. The box is now in the drawer, and `dbgDrawer` sets it from `lagOn` only, which
   is this browser's localStorage. So:
   - Ticked from another browser, the hub and the room log, and this drawer shows the box unticked. Switching it off
     from here takes a tick and an untick.
   - With `ATRIUM_DEBUG_INPUTLAG` set on the machine, the pinned note (`#s-inputlag-pinned`, which moved into the
     drawer) never shows unless settings was opened earlier in this page, which now has no reason to happen.
   - docs/terminal/input-lag-logging.md still says "Opening settings in another browser shows the box ticked and
     starts timing there too". That is no longer true.

   Fix: when the drawer opens, `GET /v1/settings` and pass the answer to `syncInputLag` (once per open, not per
   poll), and update the doc sentence to say the drawer. Add a termDebug case: settings answer
   `input_lag_log: true`, open the drawer, and the box is ticked. Then answer `input_lag_pinned: true`, and the note
   shows.
2. **Low. The drawer's poll runs in a hidden tab.** `pollTyping` skips the request while `document.hidden`.
   `dbgPoll` does not, so a drawer left open in a background tab or a minimised popped-out terminal asks every 500 ms
   for as long as it stays open. Fix: the same `document.hidden` check, and reschedule.
3. **Low. Escape is taken from everything while the drawer is open.** The capturing handler stops Escape at the
   document whenever `termDrawerOpen` is true, whatever has focus. A hover peek opened while the drawer is open (hover
   needs no click, so the outside-click close never runs) needs two presses, and a dialog opened from the keyboard
   with the drawer open does not cancel on the first Escape, because `preventDefault` stops the dialog's `cancel`.
   Fix: only take Escape when the focus is in the terminal or in the drawer
   (`e.target.closest("#t-screen, #t-drawer")`), and let it pass otherwise. The termDebug case does not check that
   the program never sees the Escape. Worth one assertion.
4. **Nit 1.** With the gate line ticked and the drawer open, the same endpoint is polled twice every 500 ms. Painting
   both from one poll would halve that. Optional.
5. **Nit 2.** 9d83de18 is titled "WIP ... parked for fix-term-box". Squash it into the feature commit before landing,
   or retitle it. The branch is claude/fix-joined, the name of an earlier landed item. When it lands, I will check
   that the merge carries only these commits.

Quality: after the Sonnet switch, the same pattern as before: the named cases are right and carefully matched to the
daemon (the countdown is exactly `gateLocked`), and the sibling case is missed. Moving a control is what lost the
code that ran in its old place.

**HOLD d94090f6..5d86f833.** Medium 1 first. Lows 2 and 3 should go in the same pass. Re-read range starts at
d94090f6.

## Re-read e1a8a5fd

`d94090f6..e1a8a5fd`, now one commit (9d83de18 and 5d86f833 squashed). I read the delta against 5d86f833:
`js/peek-debug.js`, `js/typing.js`, the doc and the termDebug section.

- **Medium 1 closed.** `dbgDrawer` calls `dbgSyncLag(seq)`, which asks `GET /v1/settings` once per open (and once per
  follow, since a follow goes through `dbgDrawer`), drops a late answer by `seq`, and hands it to `syncInputLag`.
  That is the function `loadHousekeeping` uses, so the box and the pinned note behave as they did in settings. It
  calls the browser-only `toggleInputLag`, never `saveInputLag`, so opening the drawer does not POST the setting
  back. Under a pinned machine it leaves this browser alone and shows the note. The doc sentence names the drawer.
  The case covers both answers: `input_lag_log: true` ticks the box and starts `lagOn`, and `input_lag_pinned: true`
  shows the note and leaves `lagOn` off.
- **Low 2 closed.** `dbgPoll` skips the read while `document.hidden` and keeps its timer, which is what `pollTyping`
  does. The case fakes a hidden document and counts zero gate reads over 1.3s.
- **Low 3 closed.** Escape is taken only when its target is inside `#t-screen` or `#t-drawer`. A hover peek or a
  dialog has the focus elsewhere and gets its Escape. The case sends Escape from the xterm textarea and asserts no
  `\x1b` reaches the attach socket, then blurs and asserts Escape leaves the drawer open.
- **Nit 1 closed.** `typingRead` shares one read in flight or under 300 ms old, keyed by card and kind. With two 500
  ms pollers at any offset, one of each pair falls inside the other's 300 ms, so the daemon is asked once per 500 ms.
  An error is shared as a value, not a rejection, so neither poller throws.
- **Nit 2 closed.** One commit with the feature's title.

**Nit (new, optional).** After a click on plain text in the drawer, the focus is on `body`, so Escape does not close
the drawer. A click on any control in it, or in the terminal, does. Taking Escape when the target is `body` too would
cover it, but a body Escape could also be meant for nothing at all. Leave it unless clint notices.

Quality: after the Sonnet switch, no drop. Every finding is fixed in the shape asked, each with a case (read, not run),
and the nit was taken too.

**HUB DEPLOY OK and ROOM DEPLOY OK d94090f6..e1a8a5fd.**
