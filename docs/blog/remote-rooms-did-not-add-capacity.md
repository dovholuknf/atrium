# Remote rooms did not add capacity

Series: Rooms, the hub and federation. Status: outline. Audience: people scaling agents across machines.

**Hook.** The plan was more machines, more workers; one remote room ran a stale build, a trust dialog ate two workers, and the one finished branch could not get home.

**Angle.** Capacity across machines needs code to move between them first; until then remote workers are a cost.

**Rests on:** rooms, launch caps, room build check, git sync. See `docs/blog/inventory.md`.

## Story beats

1. The plan to raise the cap with remote rooms.
2. m1mini on an old build; a status message typed into a trust dialog answered it with 'exit'.
3. sg3's worker finished, and its branch waited on a human fetch.
4. What it took: a room build check, `when: done` messages, git sync over the hub.
5. The local cap never bound anyway.

## Screenshots and demos

- the rooms tab
- a drawing of the branch stuck on sg3

## Sources

- the orchestrator's factory evaluations of 2026-09-29 and 2026-09-30 (off-repo, on the hub machine; paraphrase only)
- docs/rnd/git-sync-design.md
- changelog/fabric/2026-09-30-f-019b.md
- docs/rnd/room-to-room-access-spike.md

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
