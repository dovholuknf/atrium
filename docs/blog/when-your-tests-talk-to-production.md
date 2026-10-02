# When your tests talk to production: test runs posting cards to the live board

Series: The daemon and the halt. Status: idea. Audience: anyone whose tests run on the same box as the real thing.

**Hook.** Tests read the machine's shared address file and reported to the live room, twice, until a test guard sealed them off.

**Angle.** A tool installed machine-wide is reachable from its own test suite unless you cut the path on purpose.

**Rests on:** internal/testguard, ATRIUM_LOCATION hand-down. See `docs/blog/inventory.md`.

## Story beats

1. Cards named `002` and `atrium-reopen-*` appearing on the real board.
2. How: hooks in test processes found the live room through a shared file.
3. An older cousin: tests deleting the running daemon's address.
4. The guard: test binaries clear every `ATRIUM_*`, a sealed home, agents never really started.
5. The general rule for tools that supervise themselves.

## Screenshots and demos

- the stray cards on the board
- the guard's env list

## Sources

- changelog/runtime/2026-09-30-r-test-guard.md
- changelog/runtime/2026-09-30-r-test-isolation.md
- internal/testguard
- commit ac976cde

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
