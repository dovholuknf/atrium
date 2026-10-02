# Working notes leaked into a public branch

Series: The software company: directors and workers. Status: idea. Audience: people running agents in public repos.

**Hook.** A director's handoff and plan files were tracked, and a merge carried them into the public history.

**Angle.** Agents write notes constantly; in a public repo, every note is a publication unless the repo refuses it.

**Rests on:** gitignore for working notes, merge-check guard, held commits. See `docs/blog/inventory.md`.

## Story beats

1. HANDOFF and PLAN files merged into main.
2. They were removed from the tree.
3. The guard: git now ignores HANDOFF files. Ignoring PLAN files, and a merge check that refuses working notes, are still proposals.
4. The later rule: held material gets its own commits, never mixed with a doc meant to land.
5. Quoting the operator: paraphrase in public, keep his words off-repo.

## Screenshots and demos

- the ignore list

## Sources

- the orchestrator's factory evaluations of 2026-09-29 and 2026-09-30 (off-repo, on the hub machine; paraphrase only)
- .gitignore (HANDOFF.md, HANDOFF.*.md), commit 870d533e

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
- Do not name the commits that carried the files, and do not say what the files held. Whether to rewrite that history is clint's call (INDEX question 4).
