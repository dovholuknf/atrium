# m-new-release-0-0-1. Release 0.0.1: plan, docs site refresh, publish

Status: READY, prepared by agents. The publish is clint's, by hand. Plan: `docs/release/release-0-0-1-plan.md`. First step
for clint is the full gate on a machine that can run it. The deploy queue is merged and deployed.

Was HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G11 of docs-deps. Owned by release (no
running owner, see review-new-orphaned-owners).

## What is missing

Release 0.0.1 lives only in `notes/OWED.md` row R2. There is no backlog item for the plan, the docs site refresh or
the publish (`packaging.md`, "Publishing for the first time").

## Why it is needed

The release is the goal the rest of the backlog serves, and one row in a notes file is easy to lose.

## Depends on it

- the far-backlog one-line install and brew tap

## Done looks like

- A plan: what must be live and passing first (the deploy queue, a clean gate), the version and tag, and who runs each
  step.
- The docs site refreshed against main.
- The first publish done by clint's hand, following `packaging.md`. Agents do not push or publish.
- OWED.md row R2 points here.

## Decided by @fabric, 2026-10-05

Clint's standing order is that directors answer technical and product questions.

- **Version and tag:** `0.0.1`, tag `v0.0.1`, on the claude/main sha the orchestrator names at release time.
- **Signing:** unsigned for 0.0.1. `checksums.txt` is published with the assets. Signing is a later item, filed as
  `docs/backlog/release/m-new-release-signing.md`.
- **Channels:** GitHub Release, the scoop bucket and the docs site. The brew tap and the one-line install stay in the
  far backlog.
