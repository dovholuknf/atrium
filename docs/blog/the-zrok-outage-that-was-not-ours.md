# The zrok outage that was not ours

Series: Overlays and zrok. Status: idea. Audience: zrok and OpenZiti users.

**Hook.** Every share request answered 500 with an empty body, and the work could not be proven, so the repo got an upstream-ready report.

**Angle.** When a dependency is down, write the report the upstream would want, and keep building behind a flag.

**Rests on:** zrok board share, reserved names. See `docs/blog/inventory.md`.

## Story beats

1. Reserved shares built, and every share failing.
2. Public and private alike; the body empty.
3. The report, written to be filed.
4. Isolating a failed share so it never takes the local board down.
5. What proved it later.

## Screenshots and demos

- the failing request (redacted)

## Sources

- docs/fabric/zrok-share-500.md
- docs/fabric/overlays.md

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
