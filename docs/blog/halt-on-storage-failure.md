# Stop taking work when the database fails, and when that rule halted every room

Series: The daemon and the halt. Status: idea. Audience: people building daemons and supervisors.

**Hook.** atrium halts when its store fails, which is right, until a unique-name clash counted as a store failure and stopped every agent.

**Angle.** A safety rule needs a precise trigger: 'storage failed' and 'the database said no' are different events.

**Rests on:** the halt, the health event, constraint errors never halt. See `docs/blog/inventory.md`.

## Story beats

1. The rule: on a storage failure the agent listener closes and stays closed, and the board stays up to say why.
2. r-040: a hook named a session after its directory, collided with a finished card's name, and the constraint halted the room.
3. A sibling: an event for a card that did not exist halted a fixture's first start.
4. The fix: a constraint refusal goes back to the caller; only real storage failure halts.
5. And it sat unshipped for seven hours, because a room restart needs the room idle (see the restart post).

## Screenshots and demos

- the board's halted banner
- the health event in the audit tab

## Sources

- docs/how-atrium-works.md
- docs/review/cr48-2d0ac78-ada747b.md
- changelog/runtime/2026-09-30-r-test-isolation.md
- changelog/runtime/2026-09-29-r-017.md
- the orchestrator's factory evaluations of 2026-09-29 and 2026-09-30 (off-repo, on the hub machine; paraphrase only)

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
