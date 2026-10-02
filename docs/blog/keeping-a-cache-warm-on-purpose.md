# Keeping a prompt cache warm on purpose

Series: Cards, not agents. Status: idea. Audience: heavy Claude Code users.

**Hook.** Coming back to a big session after lunch cost a noticeable share of a week's limit, so atrium refreshes idle caches with forked resumes and stops at break-even.

**Angle.** A cache miss on a large context is a real cost, and paying a little to avoid it is arithmetic, not magic.

**Rests on:** cache keep-alive, idle parking. See `docs/blog/inventory.md`.

## Story beats

1. What a cold resume of a large context costs.
2. A guarded forked headless resume that touches the cache without touching the conversation.
3. Stopping at break-even, and only for cards a human uses.
4. Idle parking: a card idle two hours keeps its resume id but loses its process.
5. The 5% write rule that was dropped.

## Screenshots and demos

- the cache state on each card
- a keep-alive cost line

## Sources

- docs/runtime/cache-keepalive-design.md
- changelog/ui/2026-09-30-u-032.md
- docs/rnd/keepalive-policy-design.md
- commit 81492df1

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
