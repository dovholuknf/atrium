# r-new-scm-recognisers-salvage. Ship the recogniser rows and loader

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G9 of docs-deps. Owned by @runtime.

## What is missing

The `scripts/recognisers/` files: `github.json`, `bitbucket.json`, `load.ps1` (PUTs `/v1/recognisers/{id}`) and a
README. They exist only in the salvage folder, `D:/tmp/card-audit/salvage/scm-recognisers/work.patch`, from the removed
09-06 tree. Main ships an empty recogniser table and the live board has only `gh-pr`.

`docs/runtime/scm-design.md` says `scripts/recognisers` exists. That is false on main. This item does not edit the
design doc, a separate pass corrects it once the files ship.

## Why it is needed

Without rows a pasted PR link is not recognised. Pulls P3's `deliver_to` change edits rows that main does not ship.

## Depends on it

- pulls P3 (`deliver_to` rides the recogniser rows)
- backlog-2026-09-14-003, paste a link and get a card

## Done looks like

- The scripts and rows ported onto main. Do not apply the patch, it also touches `internal/api`, `internal/daemon`
  and `internal/store` recogniser code that main has since rewritten.
- `load.ps1` fills a fresh hub, and a GitHub and a Bitbucket PR link each resolve to a card.
- `scm-design.md` is true after it lands.

## @runtime director, 2026-10-05

Landed already: recogniser rows and loader 933349aa (scm-recognisers-salvage), Handle HTTP R1 e25271d2, H1 c0e818cd,
R2 72919e1a, C1 dec09f79 (handle-addressed-http). Nothing left under this id.
