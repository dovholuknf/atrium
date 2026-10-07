# "ConPTY has no reattach", until a spike showed it does

Series: The daemon and the halt. Status: idea. Audience: Windows and terminal engineers.

**Hook.** A limit written into four docs as fact turned out to be false, and a pty host now outlives the daemon.

**Angle.** Test the 'impossible' before designing around it.

**Rests on:** pty host (off by default), rolling restart stage 0. See `docs/blog/inventory.md`.

## Story beats

1. The belief: a supervised runner can never outlive the daemon, because ConPTY cannot be reattached.
2. Where it was written: README scope, the architecture's open risks, the backlog's out-of-scope.
3. The spike: a detached process owning the ConPTY, reattached with no byte lost.
4. `atrium ptyhost`, ring replay, shipped behind a setting that is still off.
5. What the docs still say, and why stale rules in docs are their own bug.

## Screenshots and demos

- a diagram of daemon, pty host and runner
- a restart with the terminal still scrolling

## Sources

- docs/rnd/rolling-restart-design.md
- docs/terminal/ptyhost-protocol.md
- docs/rnd/rolling-restart-design.md
- README.md (Scope)
- commit 555b85d5

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
