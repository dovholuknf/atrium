# rnd-new-backlog-in-atrium: atrium holds the backlog, not markdown files

Status: not started. HIGH priority, after `rnd-new-room-handoff`. Owned by @rnd (design). Filed by the orchestrator
2026-10-01, from clint: "atrium should drive backlog instead of files".

## How it works today

- One markdown file per item under `docs/backlog/<dept>/`, plus a `QUEUE.md` per director that says what is next.
- Item ids are made up by whoever files them (`u-new-...`, `r-new-...`, `rnd-new-...`), and renumbered later by hand.
- The status line at the top of each file goes stale. The memory "backlog status lines are stale" exists because a
  director was given an item that had already landed.
- Reports land in `notes\director-reports.md`, a file on sg4 that a director on m1mini cannot write. On 2026-10-01
  that broke the moment directors moved rooms.
- Untracked item files pile up in the main checkout (17 untracked `docs/backlog` files on 2026-10-01) until someone
  commits them, so a director on another machine cannot see an item filed an hour ago.
- Nothing links an item to the card working it, the branch, the @review verdict or the landing commit, except prose.

## Wanted from the design

- Items as rows the hub holds: id, department, title, body, priority, status, who filed it and from whom, created and
  changed times. Ids are given by atrium, not made up.
- Status that follows the work instead of being typed: an item with a card is in progress, an item whose card landed
  is done, with the card, branch, verdict and landing commit linked.
- A queue per director in atrium: order, next, waiting on whom. Readable and writable by MCP tool and by
  `atrium backlog ...`, from any room, through the hub.
- A board view: the backlog per department, drag to reorder, file an item from a screenshot.
- Reports as rows too, so they reach the orchestrator from any room.
- What stays a file: designs (`docs/<dept>/*-design.md`) and changelogs that ship with the code are reviewed in
  git. Say where the line sits, and whether the hub can export the backlog to markdown for a repository that wants it.
- Migration: read the current `docs/backlog/**` and `QUEUE.md` files in once.
- Relation to the inbox (`docs/runtime/intake-design.md`): an inbox item is a suggestion, and a backlog item is
  accepted work. Same table or two? Say which.

## Done means

A design, reviewed by @review, where clint or the orchestrator files an item in one call from any room and every
director sees it at once, with no file to commit.
