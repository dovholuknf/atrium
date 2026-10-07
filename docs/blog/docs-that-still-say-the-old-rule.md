# Docs that still say the old rule

Series: Supervision and terminals. Status: idea. Audience: maintainers of fast-moving projects.

**Hook.** The README promises peer messages are never typed and ptys never outlive the daemon; both changed.

**Angle.** When agents write most of the code, the docs drift faster than any human notices.

**Rests on:** peer bus typed delivery, pty host. See `docs/blog/inventory.md`.

## Story beats

1. Rule one: no prompt injection, peers only queue.
2. Overruled: the pty is shared, so a peer types when the line is free.
3. Rule two: ConPTY cannot reattach (it can).
4. Where the old rules still live.
5. A check for stale rules, as a proposal.

## Screenshots and demos

- side-by-side doc lines

## Sources

- README.md (Scope)
- docs/backlog.md (out of scope)
- docs/archive/architecture-v2.md
- docs/runtime/agent-messaging.md

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
