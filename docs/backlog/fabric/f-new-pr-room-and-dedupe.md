# f-new-pr-room-and-dedupe. Which room runs a PR, and one row per PR across rooms

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G4 of docs-deps. Owned by @fabric.

## What is missing

Providers, checkouts and the `gh` login are per machine. The hub only proxies `/v1/prs` to one room, and intake
dedupes on source plus external id per machine. A review-requested source that runs on two rooms makes two rows and
two runs of the same PR.

Nothing decides which room runs a PR, and nothing makes one row per PR across rooms.

## Why it is needed

Double cost for one review, two cards with different findings, and no rule for where a PR belongs when only one room
has the checkout or the login.

## Depends on it

- the pulls P3 source door
- the pulls E2E run off sg4
- room handoff, when a PR card moves

## Done looks like

- A rule for choosing the room: the one with the checkout of that repo and a working forge login, with a stated
  tiebreak.
- The hub dedupes on source plus external id across rooms, so a second room's row folds into the first.
- A source enabled on two rooms yields one row and one run, shown with the room that holds it.
- Tests with two fake rooms report the same PR.
