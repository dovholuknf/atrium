# The hub that was supposed to hold nothing

Series: Rooms, the hub and federation. Status: idea. Audience: distributed-systems people.

**Hook.** From 'federate in the client' to a hub that owns integration branches, documents and a git store, one decision at a time.

**Angle.** A principle eroded in steps, each step justified, and the end state is the opposite of the start.

**Rests on:** hub/room split, decisions 11 and 19, hub documents, hub git store. See `docs/blog/inventory.md`.

## Story beats

1. Federation v1: no central aggregator.
2. v2: a forum that holds nothing.
3. Decision 11: the hub gets a store; decision 19: the hub owns integration branches.
4. Hub documents, then a hub git store.
5. Was it wrong to start with 'nothing'?

## Screenshots and demos

- the architecture as drawn at each stage

## Sources

- docs/rnd/federation-design.md
- docs/rnd/federation-design-v2.md
- docs/decisions.md 11 and 19
- docs/fabric/hub-room-plan.md

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
