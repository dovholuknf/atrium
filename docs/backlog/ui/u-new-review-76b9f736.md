# Review of the paste spinner pair: 76b9f736 (@ui board) and 6ca9e81f (@runtime room)

Reviewed by @review, 2026-09-30, from `git diff 76b9f736^ 76b9f736` and `git diff 6ca9e81f^ 6ca9e81f`. The room half
is room side and the board half is hub side. They land together, then a room deploy. At 6ca9e81f, `go vet
./internal/daemon/` passes and `go test -count=3 -run 'TestAPasteWithAnID|Attach' ./internal/daemon/` passes. Board
and headless checks are @ui's, and I ran none.

## What holds

- **The room answers after the write, never before.** `in-done` goes out only once `writeOperatorInput` (or the
  timed form) returned without error. A failed write returns and closes the socket, so no `in-done` claims a paste
  that did not land. The answer goes to the sending socket only, and never to the other panes. A frame with no `id`
  gets nothing, and the test locks that in, along with the order and the whole paste in the pty.
- **Old and new mix both ways.** An older room ignores the unknown `id` field. An older board never sends one, so it
  never receives a text frame it would write into the terminal. The board tells rooms apart per socket
  (`sock.pasteAck`), not by version, and a socket that has not answered keeps the old guesses.
- **The box always comes down.** It comes down on the room's word for this paste's id, on the socket closing
  (`pasteEnd("the socket closing")`, the path a failed write takes), on a terminal switch, on the too-big refusal,
  and on the 20 second cap. An `in-done` for a replaced paste marks the socket and ends nothing.
- **Order is kept.** Keys typed behind a paste wait in `pasteHeld` and leave after the frame. A switch before the
  frame leaves drops the paste and what was typed behind it, with a toast. Before, only a big paste had this path.
- The frame size check moved inside `go`, so it counts the frame with its `id`, and it counts in bytes for a big
  paste. A small paste's `s.length` stays far under the 4 MB cap.

## Findings

### Nit

1. The room writes `in-done` from the reader goroutine, and `c.Write` waits behind the output loop's writes. During
   an output burst, the next input frame is read late by the length of one output write. This happens only on a frame
   that carries an id, which is a paste, so it is not worth changing.
2. On a socket that has answered, only `in-done` or the cap ends the box. A 4 MB paste over a slow overlay can pass
   20 seconds, so the box drops before the write lands. Nothing is lost, since held keys leave after the frame, not
   after `in-done`. The cap is older than this change.

ROOM DEPLOY OK 6ca9e81f 76b9f736
