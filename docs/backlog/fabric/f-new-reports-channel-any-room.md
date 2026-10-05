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

## Open questions for clint

- Import. Should the hub read the existing `docs/backlog/**` and `QUEUE.md` files in once, and then are the files
  retired or kept as an export? Until answered, both exist and the interim rule in the design doc applies.
- Ids. The filer still makes up the id. Should atrium give them, and what happens to the existing `u-new-...` names?
