# m-new-deploy-queue. A deploy queue, made visible

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G16 of docs-deps. Owned by release (no
running owner, see review-new-orphaned-owners).

## What is missing

A list of the landed shas that still need a deploy, and which kind each needs, a room deploy or a hub deploy. Twenty or
more landed items wait behind the pause. Only the deploy-ready pill and the HANDOFF list them, and the runtime handoff
lists seven by hand.

## Why it is needed

Fine while the pause holds. When it lifts, whoever deploys has to rebuild the list from git log and can miss one.

## Depends on it

- the 378 acceptance replay
- the f-pulls-hub lows (A4)
- the enrolled_at check after the hub deploy
- the deps board half (D14)
- the f-022 deploy

## Done looks like

- One generated list: sha, item, and whether it needs a room deploy, a hub deploy or both, from what is live on each
  versus claude/main.
- It reads what each room and the hub report as their running sha.
- Shown where the deploy-ready pill already points, and printed into HANDOFF at deploy time.
