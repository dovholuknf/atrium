# Twice fixed, twice reverted: rebuilding scrollback through a screen model

Series: Supervision and terminals. Status: idea. Audience: terminal emulator people.

**Hook.** Colour codes stacked six deep in one cell, a grid that grew instead of scrolling, and conhost losing ten lines on a height flip.

**Angle.** Tests written beside a fix can agree with the fix's misunderstanding.

**Rests on:** screen-model replay, height hold, repaint loss report. See `docs/blog/inventory.md`.

## Story beats

1. The flattener and what it got wrong.
2. Declared fixed on tests written alongside, reverted, twice.
3. Replaying through a screen model, with `replay_flat` left as the escape hatch.
4. Later: conhost losing ten rows on a quick height flip, and a half-second height hold.
5. Measuring the loss instead of guessing: the repaint loss report.

## Screenshots and demos

- before and after scrollback captures
- the loss report

## Sources

- CHANGELOG.md (2026-09-14 and 2026-09-28 entries)
- changelog/terminal/2026-09-29-74b.md
- docs/backlog-2.md item 74
- commits 53b728cb, ee9d0b54

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
