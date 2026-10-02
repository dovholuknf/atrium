# "Tested at abc1234, 3 commits ago"

Series: The hub forge and the change lifecycle. Status: idea. Audience: people who ask agents 'is it ready?'.

**Hook.** Every fact about a branch is pinned to a sha the room stamps, so a session stops answering 'yes, it's tested' from memory.

**Angle.** A fact without a sha is a rumour.

**Rests on:** change record design. See `docs/blog/inventory.md`.

## Story beats

1. The question a session could not answer without rebuilding it.
2. One record per change, facts append-only.
3. Seen versus reported, and who may write which.
4. Four lines: tested, reviewed, pushed, open.
5. The inventory handed to the next session.

## Screenshots and demos

- the four lines on a card

## Sources

- docs/rnd/change-record-design.md

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
