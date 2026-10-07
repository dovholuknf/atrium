# From chat window to kanban: atrium's first version answered the wrong question

Series: The daemon and the halt. Status: idea. Audience: people building agent tooling.

**Hook.** v1 was a TUI broker whose hub forgot everything by rule; v2 had to reverse that rule to become useful.

**Angle.** A design rule that sounded principled (restart equals reset, the hub holds nothing) was the reason the tool did not help.

**Rests on:** v1 Mode A broker, v2 SQLite task board. See `docs/blog/inventory.md`.

## Story beats

1. June: a TUI where each session long-polled a `submit` MCP tool, and an amnesiac hub.
2. What the operator actually needed: to know which session wanted him, after a restart.
3. 09-01: v2 as a task tracker with live agents, SQLite, an append-only event log per card.
4. The TUI rewrite that was planned, then abandoned; deleting Mode A and Mode B outright.
5. Lesson: the shape of the state decides the product.

## Screenshots and demos

- the v1 TUI next to the v2 board (drawn mockups, as the website does)
- the commit graph by day

## Sources

- website/docs/story.md
- docs/archive/architecture-v2.md (staged migration, Abandoned)
- docs/archive/one-atrium-plan.md
- commits fc4ccd0a, fdc5c8e6, c739bdc7, a8afe39b

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
