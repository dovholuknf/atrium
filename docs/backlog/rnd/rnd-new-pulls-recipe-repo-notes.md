# rnd-new-pulls-recipe-repo-notes. Review memory wired into the pulls recipe

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G7 of docs-deps. Owned by @rnd.

## What is missing

`review/16` designs repo notes, resident per-repo reviewer state. The pulls recipe's prime has no input for them, so
each PR starts from zero.

## Why it is needed

Repeat repos pay the full read cost every time, and what a past review learned about a repo is not used.

## Depends on it

- `review/16` stage 2 (still waits on clint's three questions)
- the pulls P3 cost targets on repeat repos

## Done looks like

- A short design: where the notes live, how the recipe's prime loads them, how a finished review writes back, and who
  may edit them.
- The size bound so notes do not eat the context the cost target saves.
- A named seam in the recipe, so `review/16` stage 2 plugs in without reworking P3.
- Design only. A build item follows once clint answers the stage 2 questions.
