# Decoupling viewer size from the shared PTY

## The two complaints

clint is unhappy with two things about attaching a terminal.

1. A PREAMBLE on every attach: `[atrium] ---- everything above is history, <how>. live from here ----`, plus the
   older width-mismatch banner that listed every column width the scrollback was drawn at. The width banner is
   already gone from source. The history/live preamble is not, and it fires on every reattach.
2. RESIZING ONE CONSOLE CHURNS THE OTHERS. Several browsers can watch one session. Today the pty is resized to the
   smallest attached viewer on every attach, detach and drag, so one window changing size reflows everybody else's
   screen, and each change lays a width mark in the ring.

This note is about the second. The first is a straight removal.

## The constraint that cannot be wished away

One session is one process on one pseudo terminal, and a pty has ONE size. A terminal user interface does not emit
text and leave the wrapping to the reader. It asks how wide the terminal is and composes for that number: hard
line breaks at the column it was told, boxes drawn to it, absolute cursor moves to positions it worked out itself.
Those bytes go into the ring exactly as sent.

So two viewers of different sizes cannot both get a perfectly wrapped stream from one pty. The bytes were composed
for one width. Replay them into a wider grid and the breaks land a third of the way across and paragraphs sit on
top of each other. This is why the ring records the width WITH the bytes, and it is why full decoupling, where a
viewer's size never touches the pty at all, is not free.

## Options weighed

### (a) A fixed canonical pty size, each pane renders it locally

The pty holds a canonical size that no viewer resize ever changes. A wider pane letterboxes (renders the narrower
stream with a right margin). A narrower pane scrolls horizontally or reflows in xterm.

This is TRUE decoupling: one console's resize only changes that console's local render, and no other viewer is ever
touched. The cost is the narrow pane. A raw-mode TUI like the claude prompt garbles when reflowed, so a phone on a
120-column session would show torn boxes with no way to fix it, because the pty will not follow it down. The BRIEF
names this outcome for (a): "letterbox or scroll".

### (b) The pty tracks the smallest viewer, but moves only when the size every viewer must fit actually changes

Keep the model where the pty fits its narrowest reader, because a shared raw-mode TUI has to be readable by the
smallest window on it. But resize the pty ONLY when the agreed smallest size actually changes. Kill the resize that
today fires on every attach, detach and drag even when the agreed size did not move.

## Decision: (b)

Full decoupling (a) is impossible to do WELL for a shared raw-mode TUI, because the narrow reader either garbles or
forces the pty down, and there is no third answer with one pty and one byte stream. So the honest goal is not to
pretend the coupling is gone. It is to remove the CHURN, which is a different thing and is entirely removable.

The churn is not the coupling. The coupling is that the narrowest reader sets the width. The churn is that today
the pty is resized on every viewport event regardless of whether the agreed size changed, so a `pty.Resize` to the
size it already is still raises SIGWINCH and every viewer repaints. That is what makes one console's drag flicker
the others even when it changed nothing binding.

The fix is a change-guard. Compute the agreed size (smallest of the attached viewports, unchanged), read the size
the pty is already at, and resize only when they differ. Then:

- A viewer WIDER than the current width drags around freely. It is never the smallest, the agreed size does not
  move, and no other viewer is touched. This is the case clint is complaining about, and it is now free.
- A viewer attaching or detaching without changing the smallest lays NO width mark and triggers NO repaint.
- A viewer that is genuinely the narrowest still moves the pty, because it must: the others cannot read a width
  their pane cannot show. This is the one resize the BRIEF sanctions, "only resize when it would otherwise be
  unusable".
- A lone viewer resizing itself still works exactly as before, because with one viewer the smallest IS that
  viewer, so its own drags always change the agreed size and take effect.

On detach the same guard applies: the pty grows back only when the viewer that left was the binding one, and the
ring already merges the two marks when nothing was drawn in between, so a window popped out and closed again costs
no scrollback.

Rows ride along with cols in the same agreed size and the same guard, so a shorter pane no longer forces a repaint
on everyone unless it is genuinely the shortest.

## What this does NOT solve, said plainly

A phone that is narrower than every desktop on the same session still pulls the pty down to phone width while it is
attached, and the desktops reflow to that width. That is the coupling, not the churn, and it is inherent to one
shared TUI. A deliberate operator control ("render this pane at my size, leave the pty alone") would be option (a)
applied to one pane on request, and it is future work, not this change. The default stays readable-for-everyone.

## Interaction with the decoupled phone panes and multi-pane echo

- DECOUPLED PHONE PANES (board, already landed). The phone terminal is now its own surface at a stable size, and
  opening or closing the session switcher does not resize it. That means the phone sends its size ONCE on attach
  rather than on every list interaction, so under this change it lays at most one width mark and never churns the
  desktops from switcher chrome. The two changes pull the same way: the board stopped generating spurious resize
  events, and the daemon stopped acting on the ones that change nothing.
- MULTI-PANE INPUT ECHO (`docs/multi-pane-input-design.md`, PARKED). Keystroke fan-out is display-only and size
  independent, so this change does not touch it. It is worth noting that with viewers now more often at different
  local sizes, the echo remains what its own note already says it is: a legible approximation of what a peer is
  typing, drawn at wherever that peer's cursor sits, not a faithful mirror of the app's line editor. Nothing here
  makes that better or worse. Stdin is still written exactly once.

## The preamble removal

The history/live divider written under the replayed backlog is deleted. The BRIEF's instruction is "can we just
ditch this whole preamble?", and the answer is yes. A boundary marker had one job, to stop the first live redraw
reading as corrupted history, and in practice it read as noise on every reattach. Silence is better. The honest
"this is not the start of the session, the buffer overwrote older output" note stays, because it answers a real
question (is my scrollback short because the session is short, or because the ring is too small) and it is not a
wall of width numbers.

## Hub-only vs room-side

Everything here is ROOM-SIDE and PARKED. `setViewport`, `dropViewport`, the ring's size accessor and the attach
preamble all live in `internal/daemon`, which owns the pty and the attach socket. It needs a planned room restart
and must not be deployed into the live room. There is no board (hub) piece: the board already stopped sending
spurious resizes when the phone panes were decoupled, and it needs no change to benefit from this.
