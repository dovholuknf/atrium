# A zombie card resumed another room's conversation

Series: The daemon and the halt. Status: idea. Audience: people building session supervisors.

**Hook.** The orchestrator moved to a new room; on the next restart its old card came back on the same conversation, and messaged its living self.

**Angle.** Reopening everything after a restart is a feature until 'everything' includes cards a person meant to close.

**Rests on:** restart reopens what was open, asked-exit is sticky. See `docs/blog/inventory.md`.

## Story beats

1. A restart reopens what was open, so nobody has to.
2. 09-30: the orchestrator moved rooms and exited its old card; a room deploy restarted it anyway.
3. Two cards on two rooms now resumed one conversation, and one asked the other for a new context.
4. The overcorrection: keep done cards down at boot, and a wind-down leaves every card done, so the next restart opened nothing.
5. The fix: record an explicit asked exit, and only that keeps a card down.

## Screenshots and demos

- a timeline drawing of the two cards
- the asked-exit migration

## Sources

- docs/backlog/runtime/r-new-reopen-resumes-exited-card.md
- changelog/runtime/2026-09-30-r-exit-asked.md
- changelog/runtime/2026-09-30-r-review-exit-asked.md
- docs/runtime/reload-design.md

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
