# u-popout-notify. The bell in a popped-out window speaks for that window only

Status: design, @ui, 2026-09-30. clint's ask via the orchestrator: a popped-out window (he keeps the orchestrator
popped out) shows the notification bell, and its toggles must apply to that window, which means that card, and never
change the main board's settings.

## What is there today

- The drawer's **turn off** writes `atrium.notify.off` in `localStorage`. Every window of the browser reads it on
  every ask (`notifyIsOff` in `js/notify.js`), so turning it off in a pop-out turns the whole board off. That is the
  bug clint is describing.
- The **sound** button writes `atrium.sound` (`alerting` prefs), the same shared key, so muting in a pop-out mutes
  the board.
- A popped-out card is announced by its own window only (`announce` drops `poppedOut` cards on the main board). So a
  pop-out already owns that card's alerts, which is what makes a per-window switch meaningful.

## The design

1. **In a pop-out, the bell drawer's toggle is the card's, not the board's.** It writes
   `atrium.notify.off.card:<bare card id>` in `localStorage`. Keyed by the card, not the window, so closing and
   reopening the pop-out keeps the choice. The main board never reads this key.
2. **A pop-out is silent when either switch is off.** The board-wide switch still holds in a pop-out (the master
   switch means everything, item 79). The drawer in a pop-out says which: "off for this window" or "off for the whole
   board (change it on the board)". The pop-out cannot turn the board-wide switch on or off.
3. **The sound button in a pop-out mutes that card only**, key `atrium.sound.card:<bare card id>` (`{"muted": bool}`).
   A muted pop-out still toasts and still sends its desktop notification, as the board's mute does.
4. **Permissions follow the same rule as the master switch.** The master switch silences permissions too
   (`NOTIFY_OFF_SILENCES_PERMISSIONS`), so the per-window switch does as well, and the drawer's tip says so. The request
   is still listed in the drawer and on the board's perms view, and the growler (when it lands) is state, not an
   interruption, so it still shows.
5. **When the pop-out closes, the card goes back to the board**, which announces it under the board's own settings.
   The per-card keys are not applied there. A per-card switch on the main board is a separate idea and is not built.
6. **The gear's notification settings are not shown in a pop-out**, only the drawer toggle and the sound button. The
   gear stays the board's.
7. **Everything else is unchanged:** the toast log still records every alert in the pop-out while it is off, and a
   `storage` event repaints the bell when another window flips the board-wide switch.

## Tests (headless)

`popoutNotify`: in a pop-out (`#term=<id>`), turn off writes the card key and not `atrium.notify.off`. The main board's
bell stays on. The pop-out holds back its card's toast, sound and desktop notification and still logs them. The
board-wide switch off silences the pop-out too, and the pop-out drawer says so and cannot flip it. Mute in a pop-out
leaves the board's sound on. Reopening the pop-out keeps its switch. Rerun notifyOff, toastStays, toastsTop.

## Open questions

None for clint. Named, not built: a per-card switch on the main board.
