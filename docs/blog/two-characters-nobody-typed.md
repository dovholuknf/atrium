# Two characters nobody typed

Series: Supervision and terminals. Status: idea. Audience: terminal and automation people.

**Hook.** Clicking into a terminal sent a focus report, the typing gate counted it as typing, and messages waited on an empty line for nine minutes.

**Angle.** To know whether a human is mid-line, track the line, not the bytes.

**Rests on:** typing gate. See `docs/blog/inventory.md`.

## Story beats

1. Why atrium waits for an empty line and two seconds of quiet before typing.
2. `ESC [ I` counted as `[` and `I`.
3. A worker idle for nine minutes on a message that never went in.
4. Tracking line text, ignoring focus and mouse reports.
5. Acks for pastes, so the room knows a paste landed.

## Screenshots and demos

- the byte trace from the terminal cog
- the gate's state

## Sources

- docs/backlog/terminal/33.md
- commit 118d6e58
- changelog/runtime/2026-09-30-r-paste-done.md

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
