# m-new-release-0-0-1. Release 0.0.1: plan, docs site refresh, publish

Status: PLANNED, hold lifted 2026-10-04. Plan: `docs/release/release-0-0-1-plan.md`. Waiting on clint's answers below and on
a green gate. The deploy queue is merged and deployed.

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

## Open questions for clint

- Version string: is it `0.0.1`, tag `v0.0.1`? (The site already says 0.0.1, `packaging.md` shows `v0.1.0`.)
- Signing: ship the Windows MSI and macOS pkg unsigned for 0.0.1?
- Channels: GitHub Release plus scoop plus the docs site only, no brew tap?
