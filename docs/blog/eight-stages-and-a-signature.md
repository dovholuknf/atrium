# Eight stages and a signature

Series: The hub forge and the change lifecycle. Status: idea. Audience: people letting agents prepare PRs.

**Hook.** A change is walked through only when the human approved it hunk by hunk on a screen, and the agent cannot push early because the walls are credentials, not instructions.

**Angle.** Enforce the order with what the agent cannot hold, not with what it is told.

**Rests on:** change lifecycle design, hub forge. See `docs/blog/inventory.md`.

## Story beats

1. The ask: walk me through it before anything is pushed.
2. Stages computed from evidence, never set by the session.
3. Red and green per new test, and why a build failure is not red.
4. The walls: the hub refuses, rooms hold no forge credential, the PR tool refuses, a Stop hook blocks once.
5. Signing on the human's machine, and the second round.

## Screenshots and demos

- the ladder on a card
- the walkthrough screen mockup

## Sources

- docs/rnd/change-lifecycle-design.md
- docs/backlog/rnd/rd-new-review-45687063.md
- docs/rnd/hub-forge-design.md

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
