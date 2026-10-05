# f-new-reports-channel-any-room. A reports and handoff channel that works from any room

Status: built 2026-10-04, hold lifted (see `docs/rnd/reports-channel-design.md`). Filed by the orchestrator 2026-10-01, from clint, gap G13 of docs-deps. Owned by @fabric.

## What is missing

`notes/director-reports.md` and the untracked `docs/backlog` files live on sg4 only. Directors on m1mini report by
say and cannot see items filed there. Only the held `rnd-new-backlog-in-atrium` spike touches it.

## Why it is needed

A director on another room cannot read the backlog or leave a report where the orchestrator looks, so items filed on
sg4 are invisible to them.

## Depends on it

- every director on m1mini
- room handoff

## Done looks like

- A stated place for director reports and for new backlog files that every room can read and write, through the hub
  and not through a shared checkout.
- A director on m1mini can list the items filed on sg4 and file one that sg4 sees.
- The interim rule until then is written down, so nobody assumes a file on sg4 reached the others.
- Builds on the hub items table where it can, per the `rnd-new-backlog-in-atrium` note.

## Decided by @fabric, 2026-10-05

clint's standing order is that directors answer technical and product questions. The questions that stood open:

- **Import: yes.** `atrium backlog import <dir>` reads `docs/backlog/<dept>/*.md` once and upserts each by its id. The
  id is the file name, the title the first heading, the status the `Status:` line, the department the folder and the
  body the whole file. QUEUE.md, NIGHT-*, HANDOFF* and the README, HISTORY and REVIEWER-NOTES files are skipped. Run
  it again and an item that has not moved changes nothing. It is never run against the live hub from a test.
- **The files are not retired now.** The files in git stay the source of truth. The hub copy is a mirror that import
  refreshes, until the orchestrator says otherwise.
- **Ids.** An existing id is kept verbatim. An item filed through the hub with no id gets `<dept prefix>-<n>`, n one
  above the highest numeric id of that prefix the hub holds (f, r, u, m, rnd, and t and review for the two other
  folders). Ids are unique in the table.
- **Status that follows the card.** A card launched with the tag `item:<id>` is linked to that item. The item goes
  `in-progress` with the card at launch, `built` on the card's `done` report, and `blocked` or `incomplete` on that
  report. It fits the hub's launch and report paths with a column and one route, so it is built.
- **Per-director queue:** not now.
