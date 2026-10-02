# The board filled with ghosts

Series: Cards, not agents. Status: outline. Audience: people building dashboards over agent sessions.

**Hook.** Adopting every session a ledger had ever seen made hundreds of cards waiting on a human who could not answer them; now a narrow 'adopt' is wanted again.

**Angle.** Seeing a session and being able to talk to it are different things, and a board of the first kind is noise.

**Rests on:** SessionStart/SessionEnd hooks, the abandoned ledger adoption, the wanted `atrium adopt`. See `docs/blog/inventory.md`.

## Story beats

1. The idea: read the session ledger and show everything.
2. The result: hundreds of 'waiting on a human' cards nobody could reach.
3. The replacement: a session joins when it acts, through hooks.
4. The return: an operator's own session started outside atrium, unreachable from the phone, found by hand.
5. What a narrow adopt looks like: one pid, one session, asked for by name.

## Screenshots and demos

- a drawn board full of ghosts
- the hook that brings a session in

## Sources

- docs/architecture-v2.md (Abandoned)
- website/docs/story.md
- docs/backlog/rnd/rnd-new-adopt-session.md
- the factory log of 2026-10-01 and 10-02 (held, off-repo for now; paraphrase only)

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
