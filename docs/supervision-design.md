# Supervision: owning the runner

Design for the piece that lets atrium spawn a runner under a pseudo terminal, watch it, stop it, and let a human
attach to it from a browser. Stage 7 of `docs/architecture-v2.md`.

Written before the code, so the decisions can be argued with rather than reverse engineered.

## Why this exists

Window mode, which ships today, opens a real terminal and hands the session to it. The wrapper process exits
immediately. That is fine for starting work and useless for everything after:

- **Terminate does not work.** There is no process atrium can signal, because the pid it briefly held belonged to
  a wrapper that is already gone.
- **Liveness cannot be checked.** The reaper asks the operating system whether a pid still exists. With no pid it
  correctly refuses to guess, so a launched runner sits in `running` forever.
- **Attach is impossible.** Nothing is captured, so there is nothing to show.
- **Shutdown cannot wait for anything**, because atrium is not the parent.

Owning the process fixes all four at once. It is one change with four payoffs, which is why it is worth doing
before anything else on the list.

## What is being built

A supervisor that, for a harness whose launch mode is `pty`:

1. Spawns the runner under a pty in the requested working directory, with the scrubbed environment window mode
   already builds.
2. Records the real pid on the card, so terminate and the existing liveness reaper start working with no changes
   to either.
3. Streams output into a bounded per task ring buffer.
4. Serves that buffer, and subsequent output, over a WebSocket so a browser terminal can attach and type back.
5. Notices the process exiting and marks the card dead with the exit code.

## Decisions

### A card holds two terminals, and only one of them is a runner

The runner's, and one plain shell in the same working directory. The shell is what a wedged agent needs: the
terminal on the card belongs to the agent, so when it stops answering and the question is `git status` there is
nowhere to type it.

They are kept in two maps on the supervisor and the separation is the design, not an implementation detail.
`supervisor.get` is asked fifteen times across the reaper, the park, shelving, actions, messages and the exit
recorder, and every one of those means the process doing the work. A shell in that map would have been stopped
by the park, reaped for liveness, and, when closed, would have marked its card dead.

So a shell has no card status, writes no event, is never resumed or retried, and does not survive its card. It
carries `ATRIUM_TASK_ID` so a hook can say which card it is in, and deliberately not `ATRIUM_AGENT_NAME`,
because anything started from it would otherwise file activity as though the agent had done it.

Two rather than any number. A list of terminals needs naming, ordering and a picker, and the case being solved
is singular.

### Capture is not interpretation

Output is captured for a human to read. It is not parsed to infer status for any runner that reports its own,
which is the rule already established in `docs/architecture-v2.md`. A cooperative runner announces `needs-input`
through the permission hook and the session hook. Guessing at its output would add a second, worse signal for the
same fact, and the two would disagree.

### The buffer is bounded and in memory

A ring buffer per supervised task, sized in bytes rather than lines, because a single line of a progress bar can
be enormous. When it wraps, the oldest output is gone.

This loses history, and that is the right trade. The durable record of what happened is the `event` table, which
holds status changes, prompts, permissions and their decisions. The buffer exists so that attaching shows enough
recent context to make sense of what is on screen, not so that a transcript can be replayed. Writing every byte a
runner emits to SQLite would grow without bound and buy very little.

### One reader, many attachers

The supervisor owns the only reader of the pty. Attachers subscribe to a fan out, exactly as the SSE bus already
does for events. A slow attacher is dropped rather than allowed to block the reader, because a stalled reader
would eventually block the runner itself.

### Input is not fanned out

Any attacher can write to the pty. Nothing arbitrates between two attachers typing at once, and nothing needs to:
this is one person's tool, and two browser tabs typing into the same terminal is the same situation as two hands
on one keyboard.

### Shutdown waits, then stops asking

On shutdown the daemon closes the agent listener, as it does now, and then gives supervised runners a short
grace period to exit on their own before closing their ptys. A runner mid-edit deserves the chance to finish
writing a file. It does not deserve to hold the daemon open indefinitely, so the grace period is bounded and the
daemon says what it is waiting for while it waits, the same way the existing shutdown narrates itself.

### Exiting explicitly

Any build that owns a pty calls `os.Exit` rather than returning from `main`. See the ConPTY section of
`docs/architecture-v2.md`: returning normally after a pty teardown leaves the process with status 127, which
would make every clean shutdown look like a failure to a service manager.

### One terminal, several windows, one size

A pseudo terminal has a single size and a session can have several viewers: the board on this machine, a
popped-out window, and whoever holds a share.

**The smallest attached viewer decides.** Every viewer reports its own size on attach and on every resize, the
daemon keeps them per attachment, and the pty gets the smallest width and the smallest height. A viewer that
detaches gives its constraint back.

Passing each resize straight to the pty is the obvious implementation and it is wrong in a way that is hard to
read as a size problem. The runner wraps its output for the size it was last told. A second viewer at a
different width draws those already-wrapped lines against its own grid, so the symptom is torn text,
duplicated status lines and rows that never clear, in the window that did NOT do anything. Dragging a shared
window resizes somebody else's terminal.

The cost is unused margin in the larger window. That is the trade every multiplexer makes, and it is the right
one: a margin is legible and a mis-wrapped screen is not.


## Open questions for review

1. **Buffer size.** Fixed per task, or a global budget divided among live runners? A fixed size per task is
   simpler and can be wrong in both directions: too small for a chatty build, wasteful for twenty idle sessions.

2. **What happens to a supervised runner when the daemon dies unexpectedly.** Its pty closes, so the runner
   probably dies with it, and work in flight is lost. Window mode has the opposite property: the runner survives
   atrium entirely. That is a real argument for keeping window mode rather than treating pty mode as its
   replacement, and it should be stated in the harness configuration rather than discovered.

3. **Whether attach should be able to send a signal**, such as an interrupt, distinctly from typing a control
   character into the stream. A browser cannot press ctrl-c the way a terminal does.

4. **Whether a supervised runner still needs the permission hook.** It does, because owning the terminal says
   nothing about which tool calls are about to happen. Worth confirming rather than assuming.

5. ~~**Resize authority.**~~ Answered, after last writer wins shipped and turned out to be worse than
   "occasionally reflow someone else's terminal": it made the other viewer's screen unreadable rather than
   merely differently sized. See "One terminal, several windows, one size" above.

### A terminal belongs to the daemon that opened it

The supervisor holds a pty per runner in the operator's own logon session, so a card and its terminal are not
equally portable. Any daemon that can read the row can draw the card. Only the daemon that opened the pty can
attach to it.

That is why `atrium preview` starts PASSIVE. It opens a copy of a database full of fixtures it must not spawn,
and the terminals it CAN offer are only the ones started against that copy. It is also the item on the list in
`docs/preview-design.md` that has no answer at all for two active daemons sharing state: the other four are
lease problems, and this one is an operating system fact.

### Retained output is only meaningful at the width it was written at

The size rule above fixes everything a runner draws from the moment a viewer attaches. It does nothing for what
is already in the buffer, and that is the second half of the same bug: a session launched with nobody watching
produces an hour of output composed for the width it was launched at, and then somebody attaches a wide browser
window. The pty is resized, so new output is correct, but the daemon replays the retained bytes first and those
carry hard line breaks at the old column count and absolute cursor moves worked out for it. Replayed into a
wider grid they overwrite each other. The screenshot of that is a two hundred column window holding sixty
column text, diffs on top of themselves, and two thirds of the window empty.

**The width is recorded with the bytes.** Every change of the agreed size leaves a mark in the ring buffer at
the stream position where it took effect. An attaching viewer is sent the trailing run of output that was
composed at the width the terminal is at now, and nothing older. If anything older was left out, the terminal
says so in one dim line before the replay.

Three things follow, and each was a choice:

- **The trailing run only, never a splice.** A window dragged narrow, wide and narrow again leaves two readable
  stretches with an unreadable one between them. Joining the two would join text across a hole.
- **Columns, not rows.** Width decides how bytes were composed. A height mismatch moves a repaint up or down and
  the runner's next draw corrects it, so keying on height as well would throw history away to buy very little.
- **The size is read before the backlog is written.** The viewer's size arrives as its first frame, which is
  after the upgrade, so an attach waits briefly for it before deciding what to replay. Replaying first and
  resizing afterwards is what produced the unreadable screen, and no amount of care about the buffer fixes it.

Nothing extra is needed to fill the screen after a drop. Output is only ever dropped when the width just
changed, a width change resizes the pty, and a terminal user interface repaints itself when told its new size.

The alternatives were considered and are worse. Replaying nothing whenever the width has ever changed throws
away history that renders perfectly. Fixing the size at launch so it can never change is the cheapest and is
wrong the moment somebody drags a window, which is the thing that started this. A launch size IS chosen, but for
a different reason: a terminal opened at whatever its platform defaults to has a width nothing recorded, and
recording the width of the first byte only works if that width is known.

### A snapshot has to start somewhere it is safe to start

The write cursor is a byte offset and knows nothing about what is at it. Once the buffer has wrapped, the oldest
retained byte can be the middle of an escape sequence or the middle of a multi byte rune, and a width mark lands
between two arbitrary reads of the pty with the same problem. A severed escape loses its introducer and arrives
as printable characters typed onto the screen. A severed rune renders as a replacement character and can eat
what follows it.

A snapshot that does not begin at the true start of the stream therefore begins after the first line ending it
finds. A line feed cannot appear inside either an escape sequence or a multi byte rune, so it is a boundary that
can be found without parsing. **Losing a line beats shipping a broken escape.** A snapshot with no line ending in
it at all is megabytes of one line redrawing itself, which has no safe starting point and is about to be redrawn
again, so nothing is sent.

Fixed on the way out rather than on the way in. The buffer has to keep taking bytes as fast as a runner produces
them, and cannot afford to parse them.

