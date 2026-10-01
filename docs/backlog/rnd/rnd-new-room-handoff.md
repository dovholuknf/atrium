# rnd-new-room-handoff: move a card, and the work it holds, from one room to another

Status: DESIGN WRITTEN, docs/rnd/room-handoff-design.md, at @review. HIGH priority. Owned by @rnd (design). Filed by the orchestrator 2026-10-01, from clint: "atrium
should support room handoff better".

## What happened on 2026-10-01

clint asked for the five directors to move from claude-sg4 to m1mini. There is no move, so each director did it by
hand:

1. Commit a handoff into its `QUEUE.md`.
2. Make a worktree on m1mini over ssh.
3. Launch a successor with `POST /v1/launch` and `X-Atrium-Room: m1mini`, copying title, why, tags, theme and model
   from its own card.
4. Check the successor answers, move the alias, and exit the old card.

Then the move broke on git. The m1mini clone had no route to sg4 or sg3, so a director there could not read a worker
branch on sg3 or land on `claude/main`, which only exists in sg4's checkout. The orchestrator held the move halfway,
leaving two directors on each machine, and clint read that as a lack of follow through. The landing workaround (merge
onto `claude/landing` on m1mini, the hub collects it, sg4 fast-forwards) was made up on the spot.

Other things the hand move lost or got wrong:

- Cards on m1mini show `github/dovholuknf/atrium-worktrees` as their repo, the worktree folder, not the repo.
- The old card's conversation stays behind. The successor starts cold from a handoff the director wrote.
- Workers that report to a director had to be told its new card by hand.
- `send-directors.sh` and the orchestrator's notes still pointed at dead card ids.

## Wanted from the design

- One verb, `atrium move <card> <room>` and an MCP tool, that does steps 1 to 4 and refuses up front when the
  destination cannot do the card's job (git route, toolchain, repo present). The check comes before anything moves.
- What carries: alias, pin and pin order, tags, theme, sound, overrides, the harness, and the link from the old card
  to the new one so history stays connected.
- The conversation: carried (copy the transcript and resume) or cold start from a handoff. Say which, and when.
- Anything addressed to the old card (says, reports, worker parents) follows to the new one.
- How it relates to stage 3 of `docs/rnd/git-sync-design.md` (the hub merge queue). A move that needs stage 3 to be
  useful says so.
- Fold in `docs/backlog/runtime/r-new-move-card-between-rooms.md` (same machine, written 2026-09-30) or say why
  they stay apart.

## Done means

A design, reviewed by @review, that lets the orchestrator say "move the directors to m1mini" and get either all of
them moved, working, and able to land, or a refusal before anything moved.
