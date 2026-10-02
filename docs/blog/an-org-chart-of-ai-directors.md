# An org chart of AI directors

Series: The software company: directors and workers. Status: idea. Audience: people designing multi-agent systems.

**Hook.** Five resident directors, workers per item, one merger, a reviewer that gates every commit, and one human.

**Angle.** The structure that made overnight runs safe was review and a single writer of main, not more agents.

**Rests on:** aliases, control MCP, worker tool set, report-to, merged cull. See `docs/blog/inventory.md`.

## Story beats

1. The departments: ui, runtime, fabric, rnd, review, merge.
2. One merger; about twelve batches a night, none bad.
3. Directors review their workers, and reading across directors caught a race.
4. Workers see six control tools; directors see all.
5. Flattening it would only save money by doing less.

## Screenshots and demos

- the board grouped by director
- a commit graph of one night

## Sources

- docs/rnd/factory-shape.md
- docs/backlog/README.md
- changelog/fabric/2026-09-30-f-021.md
- the orchestrator's factory evaluations of 2026-09-29 and 2026-09-30 (off-repo, on the hub machine; paraphrase only)

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
