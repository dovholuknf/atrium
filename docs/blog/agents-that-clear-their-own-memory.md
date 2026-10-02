# Agents that clear their own memory

Series: Supervision and terminals. Status: idea. Audience: people running long-lived agents.

**Hook.** Directors ran at three or four times their context threshold until atrium learned to capture a handoff, clear, and wake them itself.

**Angle.** Long-running agents need a context lifecycle the supervisor owns, not a habit they are asked to keep.

**Rests on:** new-context cycle, context ceiling, autocompact. See `docs/blog/inventory.md`.

## Story beats

1. The handoff file, `/clear`, and a wake: the manual cycle.
2. Why it lagged: a cycle needs the card idle, and busy directors rarely are.
3. Automatic at a threshold, and an enforced ceiling for tagged cards.
4. Deploys that wait for a cycle in flight.
5. `--autocompact` at the limit plus 10%.

## Screenshots and demos

- a context-burn line crossing the threshold
- a handoff file (redacted)

## Sources

- docs/runtime/auto-new-context-design.md
- changelog/runtime/2026-09-30-r-029.md
- changelog/runtime/2026-09-30-r-director-ceiling.md
- commit 16488230
- the orchestrator's factory evaluations of 2026-09-29 and 2026-09-30 (off-repo, on the hub machine; paraphrase only)

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
