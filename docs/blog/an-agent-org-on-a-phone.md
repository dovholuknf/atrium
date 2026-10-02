# Running an agent org from a phone

Series: Cards, not agents. Status: idea. Audience: mobile and agent-UI people.

**Hook.** Most decisions now come from a phone, often as one word, so the phone page had to make one word enough.

**Angle.** A phone is where a human answers, so every question must carry its context and a default.

**Rests on:** the /m page, phone terminal, replies API, changes API. See `docs/blog/inventory.md`.

## Story beats

1. /m: a 'needs you' list, a card thread, approve and deny, per-turn diffs.
2. Watching a terminal without resizing it: pinch, pan, a key bar, a message box for dictation.
3. The first mobile round its own commit called a disappointment.
4. Answers by number and in one word, and what that asks of the questions.
5. Code review on the phone.

## Screenshots and demos

- phone screenshots of /m
- the phone terminal with the key bar

## Sources

- changelog/ui/2026-09-29-u-024.md
- changelog/terminal/2026-09-29-t-003b.md
- docs/backlog/ui/mobile-design.md
- commit 27330dfc

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
