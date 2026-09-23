# wrapup.md -- orchestrator wave-boundary checklist

Run this when a wave of doers is done and the queue is empty, right before a `/clear`. The point is to move every
piece of live scheduler state onto disk so a fresh session comes back at full capability with no warm context. Do the
steps in order. Do not `/clear` until step 7 says the boundary is clean.

A "wave" is one batch of doer work. A "boundary" is the moment no doer has un-integrated work outstanding.

## 1. Confirm the queue is actually empty

Call `atrium_peers`. The boundary is clean only when every `claude/*` doer either:

- has reported its SHAs and I have integrated them, or
- is idle with nothing outstanding.

If any doer is still `running` or has reported work I have not integrated, STOP. This is not a boundary. Finish the
integration first.

## 2. Integrate every reported HUB-ONLY SHA

For each SHA a doer reported as HUB-ONLY and I have not yet landed:

```
cd D:\worktrees\claude\atrium\orchestrator
git cherry-pick -x <sha>
bash scripts/check-board.sh        # exit 0 required; the skin-reconcile test is flaky, rerun once to confirm
go build -o build.claude\atrium2.exe ./cmd/atrium2
```

Then deploy hub-only (see step 3). ROOM-SIDE SHAs are NOT integrated-and-deployed here: they are recorded in step 4.

## 3. Deploy hub-only and verify the build id

```
pwsh -File C:\Users\claude\.atrium2\scripts\deploy-hub-only.ps1
```

Read the tail of the deploy log and confirm `hub up build <id>`. The build id changes only when board assets changed
(Go-only changes keep the same id). Do not deploy while a doer is mid-run or while clint is actively typing.

## 4. Record any PARKED room-side SHAs

Every SHA a doer reported as ROOM-SIDE / PARKED goes into `docs/parked-room-changes.md`, with what it does and which
doer produced it. This is the one piece of state that outlives a wave and lives nowhere else. If it is not written
here, a fresh session will not know a room restart is owed.

## 5. Journal any decisions

If clint made a product or architecture decision this wave, append it to `docs/decisions-log.md`. If he answered a
design question, append it to `docs/interview-log.md` so it is never re-asked.

## 5b. Write FACTORY-STATUS.md

`D:\worktrees\claude\atrium\orchestrator\FACTORY-STATUS.md`, untracked: the live build id, what is merged but not
live, workers in flight, what clint owes (his commits, answers), filed bugs not yet dispatched, and the next `sa`
number. Cold start reads it first.

## 6. Cull validated doers (only if clint validated them)

Only after clint has said the work is good, recycle idle validated doers with `atrium_exit` to free launch slots
(cap is 10 concurrent). Never cull a doer whose work clint has not confirmed.

## 7. Improve the runbook from what this wave taught

Before committing, evaluate this file and `docs/cold-start.md` against what actually happened this wave. Fold in the
learnings so the next run is smoother. Ask:

- Did a step mislead, go stale, or get skipped because it was wrong? Fix the wording.
- Did I hit a gotcha this wave that is not written here (a flaky test, a deploy trap, a state that lived nowhere)?
  Add it.
- Did clint correct how I run a step? Encode the correction here so I do not repeat it.
- Did a stable path, script, or command change? Update it.

Keep it tight: this is an operating checklist, not a diary. Prune steps that stopped earning their place.

## 8. Commit the docs and declare the boundary clean

```
cd D:\worktrees\claude\atrium\orchestrator
git add docs/parked-room-changes.md docs/decisions-log.md docs/interview-log.md docs/wrapup.md docs/cold-start.md
git commit -m "orchestrator: wrap-up wave -- park <n> room-side, decisions journaled"
```

Report a one-line wave summary to clint: what shipped hub-only, what is parked, live build id. Then it is safe to
`/clear`. The next session boots from `docs/cold-start.md`.
