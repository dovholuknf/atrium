# Shared multi-pane input: keystroke fan-out

## What already works

One runner, one pty. Every attach subscribes to the runner's output over its own websocket
(`runner.subscribeSized`), so a claude response is fanned out to every pane by `runner.fanout`. Input from any
pane reaches the one shared pty stdin through `runner.Write`, called from the attach reader in `attach.go` on an
`{"t":"in"}` frame. Both panes already drive the same session.

## The gap

In-progress keystrokes are not mirrored to the OTHER pane. An interactive app in raw mode (the claude prompt)
reads stdin and redraws its own input line with cursor-relative escapes. The pane you type in updates because
the app repaints it. The other pane only updates when the app emits a redraw that reaches the output stream,
which for a line still being typed it often does not do until submit.

## The fan-out point

The one place the operator's keystrokes arrive is `attach.go`, the `case "in":` arm of the reader goroutine. It
does two things today: `run.noteOperatorTyped(bytes)` then `run.Write(bytes)`. The fan-out is a third step in the
same arm: hand the same bytes, display-only, to the peer attaches of this runner.

Delivery reuses the existing output path. Peers are the runner's `watchers` channels, the same channels
`fanout` writes runner output to. A new `runner.echoToPeers(bytes, self)` writes the bytes to every watcher
EXCEPT the writer's own output channel, so the fanned bytes land in each peer's write loop and are rendered by
its terminal like any other output.

## The hazards this guards against

DOUBLED CHARACTERS is the real one. In cooked mode, or under any app that echoes its input back to the output
stream, the typed byte already reaches every pane through normal output fan-out. Echo the same byte display-only
on top of that and each peer shows it twice. A bare shell doubles every character typed.

CURSOR AND LINE-EDIT DESYNC is the second. A raw-mode app owns the input line and moves the cursor with
relative escapes. Injecting the raw keystroke into a peer's stream puts a character on that peer's screen at
wherever its cursor happens to sit, which is not where the app would have drawn it. Backspace, arrow keys and
paste do not survive this at all. The echo is a legible approximation of what a peer is typing, not a faithful
mirror of the app's own line editor.

Because of both hazards this MUST NOT be an unconditional echo.

## The guard: a mode, default OFF

Fan-out is gated by a per-runner flag, `runner.echoPeers`, default false. When it is off, `echoToPeers` returns
without touching a channel and behaviour is exactly what it is today. When it is on, keystrokes from any attach
are echoed display-only to the peer attaches of that runner.

Per-runner rather than per-attach because the doubling hazard is a property of the runner, not the viewer: a
bare shell doubles for everyone, a raw-mode agent that repaints on submit is the case this helps. The operator
turns it on for a terminal they know is a raw-mode agent, and it applies to every pane on that terminal at once.

Selection is a new control frame on the attach socket, `{"t":"echo","on":true|false}`, which sets the flag on
the shared runner. The default is OFF, so nothing changes for any existing attach or any runner nobody has opted
in. The HUB-side board sends this frame from a per-terminal toggle (see split below).

## Stdin is written exactly once

The fan-out is a DISPLAY echo to peer websockets only. The bytes still reach the pty exactly once, through the
single `run.Write` call that was always there. `echoToPeers` never calls `Write` and never touches the pty, so
turning the mode on cannot double what the app receives, only what peers see.

## Hub-only vs room-side

- ROOM-SIDE (PARKED): everything in `internal/daemon`. The `echoPeers` flag, `echoToPeers`, the `{"t":"echo"}`
  control frame and the call site in `attach.go` all live in the room daemon, which owns the subscribe/attach
  machinery. This needs a planned room restart and must not be deployed into the live room.
- HUB-ONLY: the board toggle that sends `{"t":"echo","on":...}` and any per-pane UI state. Board JS ships to the
  hub without a room restart. It is inert until the room-side piece is live, because an unrecognised control
  frame is ignored by today's daemon.
