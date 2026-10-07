# Auto mode for the next hour, and who said yes

Series: The permission chain and auto mode. Status: idea. Audience: people who run Claude Code with permissions.

**Hook.** Approve everything for an hour, except what a never rule forbids, and keep a record of whether you, a rule, or auto said yes.

**Angle.** Auto mode is safe enough when it is scoped in time, bounded by never rules, and attributed honestly.

**Rests on:** auto mode, standing rules, decision log, hub-held board-wide auto. See `docs/blog/inventory.md`.

## Story beats

1. Per card, board-wide, or for the next hour.
2. Standing always and never rules; most specific wins; a tie goes to block.
3. The attribution bug: board-wide auto recorded as 'you', fixed to `global-auto`.
4. 'What did it do?': the review after an auto hour.
5. Importing Claude Code's own allow and deny lists (134 rules on the first run).

## Screenshots and demos

- the auto pill and its countdown
- the decision log with three kinds of yes

## Sources

- docs/runtime/auto-mode.md
- changelog/fabric/2026-09-29-f-008.md
- changelog/runtime/2026-09-30-r-034.md
- docs/archive/architecture-v2.md

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
