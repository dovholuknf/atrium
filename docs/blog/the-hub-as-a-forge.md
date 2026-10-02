# The hub as a forge, without a mirror

Series: The hub forge and the change lifecycle. Status: idea. Audience: self-hosted git people.

**Hook.** The hub owns main and takes pushes with plain git rules; everything else passes through live to the room that has it, and fails when that room is off.

**Angle.** No mirror means no stale copy and no second truth; offline means offline.

**Rests on:** hub git store, repos tab, hub forge rev 2. See `docs/blog/inventory.md`.

## Story beats

1. What 'act like GitHub' meant to the human.
2. The store and plain git rules: no force, no deletes, per-card branch ownership.
3. Pass-through to the owning room.
4. Cards push to the hub only, never to origin.
5. Case-only ref collisions on case-insensitive filesystems.

## Screenshots and demos

- the repos tab
- clone URLs

## Sources

- docs/rnd/hub-forge-design.md
- docs/backlog/fabric/f-new-hub-receive.md
- docs/backlog/runtime/r-new-hub-remote.md

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
