# Two ways a hub knocked its rooms off

Series: Rooms, the hub and federation. Status: idea. Audience: people building hub-and-spoke systems.

**Hook.** One slow room stalled every attach because the hub waited for all rooms to answer; later a frame-order race kept rooms off for minutes.

**Angle.** In a fan-out, wait for the first answer that can be right, not for everyone.

**Rests on:** hub/room link, attach proxy. See `docs/blog/inventory.md`.

## Story beats

1. The attach flicker, worse with each room added.
2. Cause: the proxy waited for every room before forwarding an attach for a globally unique id.
3. Fix: forward on the first room that claims it.
4. The welcome race: a hook's frame arrived before the hub's welcome, and the room redialled every five seconds.
5. Proving both on an in-process repro, never on the live board.

## Screenshots and demos

- the flicker (recording)
- a sequence diagram of the race

## Sources

- changelog/fabric/2026-09-29-attach-welcome-race.md
- internal/link
- the factory status of 2026-09-21 (off-repo; paraphrase only)

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
- **Check before drafting.** These figures come only from off-repo sources (the factory status, evaluations or log). Confirm each against those files first: the attach flicker's cause and fix (the 2026-09-21 status).
