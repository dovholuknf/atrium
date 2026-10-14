# Launch dialog: who holds a link, the room picked, the covered terminal

What this branch adds on top of main's one-call open (u-005, u-006):

- **Pick.** A room chosen by name in the dialog sends `pick: true` to `/v1/open`. A link another room holds then goes on in
  the room picked and its claim moves there (`Take` on the claim ask), unless that room has a card live on it.
- **Live card.** When the holder has a live card the open launches nothing and answers `live`, `held_by` and the card. The
  board toasts "already running on <room>" and brings that card up, in its own window if it is popped out.
- **Who holds it.** The dialog asks the hub (`/_hub/pr-claims/holder`) and says so under the link when another room holds
  it, and whether go brings a live card up or moves the claim. Hub only; elsewhere the ask is a 404 and nothing is said.
- **Covered terminal.** The visible terminal drops to xterm's DOM renderer while the dialog is open and takes its GPU context
  back on close. Measurements and the part that is left are in `docs/backlog/ui/launch-dialog-notes.md`.

## Decisions made merging main

- **No clone_adopt question.** This branch had the dialog ask a one-time yes before adopting a clone the operator made.
  Main since decided a pasted link adopts such a clone without asking (`9be67392b`), so the question, `clone_adopt`, the
  `AdoptError` and the retry that carried the yes are dropped. Main's behaviour stands.
- **No least-busy room.** Main's `linkRoomDefault` (the room already holding the repo, else the last launched on) replaces
  this branch's `leastBusyRoom`. The holder note runs after it and on every change of the room box.
- **`pick` only on a hand-picked room.** It is sent when the room box is showing, not when the board worked the room out
  itself (`linkRoomFor`), so an automatic placement never moves another room's claim.
- **The dialog is already shut by the time `live` answers**, so the live branch does not close it again.

## Open for clint

- The 1 to 2 second delay on the ROOM and RUNNER dropdowns is not reproduced headless. The fix is partial (see the notes);
  the rest needs a measurement on a machine with a real GPU.
