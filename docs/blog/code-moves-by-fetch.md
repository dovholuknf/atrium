# Code moves by fetch, never by push

Series: The hub forge and the change lifecycle. Status: idea. Audience: people moving code between agent machines.

**Hook.** Agents may not touch remotes, so the hub collects rooms' branches and serves main read-only, and a reviewer's verdict is a git trailer.

**Angle.** Make the transport read-only and put the verdict in git, where a patch-id can match it.

**Rests on:** hub git sync, deploy-ready, verdict trailers. See `docs/blog/inventory.md`.

## Story beats

1. Why agents may not push.
2. The hub collecting `claude/*` from rooms.
3. `Atrium-Verdict` trailers matched by `git patch-id --stable`.
4. Deploy-ready hung the live hub: a lock held across a 60-second git pass.
5. Bounded at five seconds.

## Screenshots and demos

- git log with a verdict trailer
- the deploy-ready pill

## Sources

- docs/rnd/git-sync-design.md
- docs/decisions.md 19
- changelog/runtime/2026-10-01-r-deploy-ready.md
- changelog/runtime/2026-10-01-r-deploy-ready-bound.md

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
