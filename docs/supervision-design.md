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

5. **Resize authority.** Several attachers with different window sizes cannot all be right. Last writer wins is
   simplest and will occasionally reflow someone else's terminal.
