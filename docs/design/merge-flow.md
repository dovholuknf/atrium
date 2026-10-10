# Merge flow: roles, item states, who moves claude/main

Status: design, 2026-10-10. Source: `docs/interviews/INTERVIEW-merge-flow-2026-10-09.md` (its 27 decisions are settled,
cited here as D1..D27). Backlog item `r-rethink-worker-merge-flow`. This document settles the interview's "open for the
R&D agent" list and splits the build into phases.

There is no AGENTS.md in the tree. The rules followed are the ones in `docs/how-atrium-works.md` and in the comments of
`internal/hubstore/schema.go`: hub state lives in `internal/hubstore`, migrations go at the END of the slice and every
statement tolerates already being applied, hub tools are in `internal/link` (`control_mcp.go`, `backlog_mcp.go`), and git
rules are in `internal/gitsync` (`receive_rules.go`).

## The shape in one paragraph

Atrium holds the flow as rows on the hub: who holds each role, which stage each backlog item is at, which sha of main
has passed which check, which room runs which build. Where git can answer a question, atrium asks git and never asks an
agent. Where only a person or an agent knows, one tool call records it with who said it. After a hub restart nothing is
re-derived from conversation (D13). The hub already refuses every push to `claude/main` and `main` from a card
(`receive_rules.go`, `isMainRef`); the change is that the recorded merger may move `claude/main` and only by
fast-forward.

## Roles, as atrium state

A role is a row in a new table `flow_role (role, room, card, since, set_by)`. One holder per role, except `tester`, which
may have several (D15). Roles: `orchestrator`, `merger`, `tester`, `director` (optional, D21). clint is not a role row:
clint is the operator, and the operator's authority comes from the hub's local-operator check, as it does today.

- **Set by**: the operator, or the current holder handing the role on. `atrium_flow role set merger <card>`. A holder
  is a (room, card) pair taken from the caller's identity headers, the way `atrium_backlog` takes `by` and `room`.
- **Restart**: rows are on the hub, so they survive a hub restart. A card id can outlive its process (a card is a
  durable record), so a role keeps pointing at the same card after a room restart. When the holder's card is `dead` or
  gone, `flow role get` says `holder: gone`, and the operator or the orchestrator names a new one. Atrium never picks one
  itself: a silent election is how two mergers appear.
- **Why not a card tag or a setting**: a setting is one string per name with no history. The role needs `since` and
  `set_by` for the audit line, and the merge gate reads it on every push to `claude/main`.

The chain is linear and one hop is one handoff (D21, D22): orchestrator → [director] → worker → merger → tester →
deploy → orchestrator. The "deploy" step is not a role in phase 1 to 4: the merger holds it (see "Open points").

## Backlog item states

The existing `backlog_item.status` has a CHECK constraint (`open, held, in-progress, built, blocked, incomplete, done,
dropped`) that SQLite cannot alter. Those statuses stay as they are: they answer "is anyone on this". The merge flow
adds a second, separate record, `flow_item (item_id, stage, ...)`, one row per item that is in the flow, so nothing
that reads `status` changes.

Stages, in order (D14): `idea`, `designed`, `implementing`, `implemented`, `merged`, `tested`, `deployed`,
`pushed-by-clint`. Two more rows hang off an item without being stages: `blocked` stays the existing item status, and
`dropped` too. An item may skip `designed` (a small fix goes `idea` to `implementing`).

| Stage | Who says so | How atrium knows |
| --- | --- | --- |
| idea | filer | the item exists |
| designed | **reported**: a director or the orchestrator | `flow item set <id> designed` with a pointer to the doc. Optional stage. |
| implementing | **reported**, but the launch does it | when the orchestrator launches a card for the item (`card` is already linked on the item), atrium moves it. No extra call. |
| implemented | **git** | the item records a `branch`; the hub has `refs/heads/<branch>` and its tip is not yet in `claude/main`. The sha is stored. The worker's `atrium_done sha` is the same fact seen from the other side. |
| merged | **git** | the recorded sha (or the branch tip when it was pushed) is an ancestor of `claude/main`, OR every commit of the branch has a patch-equivalent in `claude/main` (`git cherry`). The patch test is what makes a rebase, a conflict fix or a re-sign still count as merged. |
| tested | **reported, keyed by sha** | a `flow_check (sha, name, result, by, at)` row says "these tests passed on main at sha S". An item is tested when a passing check exists on a sha that contains the item's merge point. It is derived, never typed per item (the interview's Q5/Q6 point: tested belongs to a sha of main). |
| deployed | **reported per room, observed where possible** | a `flow_deploy (room, sha, at, by)` row per room. A room's reported `version` (already in `room.version`) is the cross-check. An item is deployed on room R when R's deployed sha contains the item's merge point. "deployed" for the item is shown as `n of m rooms`. |
| pushed-by-clint | **git** | the item's merge point is reachable from, or patch-equivalent to a commit of, hub `main` (the mirror of `origin/main`, advanced only by the operator). Re-signing rewrites shas, so patch equivalence is the test, not ancestry. |

So atrium reads three stages from git (`implemented`, `merged`, `pushed-by-clint`), derives two from sha-keyed records
(`tested`, `deployed`), and takes exactly two on someone's word (`designed`, and the verdict of a test run). An agent that forgets a tool call
cannot stall an item at a git stage: `flow item get` computes the stage every time it is asked. The stored `stage`
column is a cache that only moves forward, and the computation can raise it, never lower it, except through the reset
(see below).

## Who moves claude/main

Today `isMainRef && !p.Operator` refuses every card's push to `main` and `claude/main`. New rule, in `decide`
(`receive_rules.go`):

- `main`: unchanged, operator only. It is the mirror of `origin/main`.
- `claude/main`: allowed for the operator (unchanged) and for the pusher whose (room, card) is the recorded `merger`,
  and for nobody else. The update must be a fast-forward (`old` is an ancestor of `new`), checked inside the
  repository lock the receiver already holds. A non-fast-forward by the merger is refused with a sentence; only the
  operator, or atrium's own reset (below), moves it backwards.
- The merger works in its own worktree, merges the branch there and pushes `claude/main` to the hub. This is D17's (b):
  atrium checks the role and the fast-forward, and the hooks stay strict, because the merger pushes through the same
  `atrium_git_push` door every worker uses. The git guard on the worker side is not loosened.
- The hub reads the recorded merger through a `Hub.Merger func(ctx) (room, card string, ok bool)` hook, nil meaning
  "nobody", which is today's behaviour. `internal/link` wires it to `hubstore`.

Each accepted move writes an audit line and, in phase 3, advances the stage of every item whose sha it now contains.

## Between clint's fetch and clint's push: no merge is ever lost (D19, D20)

**Recommendation: keep the name `claude/main` and reset it, but make the reset replay instead of overwrite, and keep a
ref to the old tip so nothing is ever unreachable.** Not a dated branch.

Why not a dated branch (`claude/main-2026-10-09`): its one advantage, that nothing moves backwards, is bought with
every worker, hook and tool learning today's name, and the hub already shows the cost (70 `claude/*` branches and no
cleanup). Atrium recording "the current main is X" fixes the lookup but not the clutter, and a force-free reset is
possible without it.

The procedure, an *epoch* per clint push:

1. clint fetches `claude/main` at `abc`. He tells atrium (or the fetch is observed: the hub knows the tip it served; the
   tool is `flow epoch fetched <sha>`). Atrium records `flow_epoch (n, fetched_sha, state=open)`. This does NOT freeze
   `claude/main`. The merger keeps merging (the rule is that no work stops, and the interview rejected a freeze).
2. clint merges, re-signs and pushes to GitHub, then the hub's `main` mirror advances to the re-signed tip `R`
   (operator push, as today).
3. Atrium **reconciles**, it does not reset blindly. It computes the set of commits on `claude/main` that are not in
   `R` by patch equivalence (`git cherry R claude/main`). Commits with an equivalent in `R` are the re-signed copies of
   `abc`'s history: drop them. Commits without one are the work landed after the fetch: they must survive.
4. The new `claude/main` is `R` plus those commits replayed in their original order (cherry-pick, by the merger, in
   its worktree, because a replay can conflict and the merger owns conflicts, D12). The merger pushes the result to a
   branch of its own, not to `claude/main`. Because `R` is not an ancestor of the old tip, the move is not a
   fast-forward, and the hub's pre-receive hook refuses every non-fast-forward push. So the move is not a push: the
   merger calls `atrium_flow epoch_close {branch, expect}` and the hub moves `claude/main` itself, under the
   repository lock, when `claude/main` is still at `expect`, the branch has `R` under it, and every commit of the old
   tip has its change on the branch (`git cherry`). That leaves the hook untouched.
5. Before the move atrium writes `refs/atrium/epochs/<n>` in the hub repository pointing at the old tip. A ref in that
   namespace is never pushed or served to rooms; it exists so that no commit is ever unreachable, and so that a mistake
   is recoverable by hand. It is the answer to "never lost": the worst case is a commit that has to be found, never a
   commit that is gone.

Worker branches in flight rebase onto the new `claude/main`; `git rebase` drops the duplicates by patch id, as the
interview observed. The merger, not the worker, rebases the ones that block (D12).

This is phase 4. Phases 1 to 3 do not move `claude/main` backwards, so they cannot lose anything.

## Deploys, per room (D9, D10)

A deploy is "put sha S on room R". Atrium decides per room, not per fleet:

- A room with no running work (no card `running`, `needs-input` is not work) gets it at once.
- A room with work waits. The existing room deploy hold (`docs/rnd/room-deploy-hold-design.md`) is the mechanism that
  makes a held card's next call refuse, and it already exists. The flow adds only the queue: `flow_deploy_want (room,
  sha)`, one row per room, replaced when newer work lands (D10: re-evaluate, do not stack).
- When the wanted sha advances while a room waits, atrium re-asks "do tests need to run for this delta?" (below). If
  not, the want is just updated and the deploy happens when the room is idle.
- Atrium records the outcome (`flow_deploy`), and does not itself restart anything in phases 1 to 4. The build and the
  restart keep going through the existing path (`restart_atrium`, the hub restart gate). This design stores the state
  and answers "which rooms are behind"; the actuator is a follow-up so that no phase here can restart a room.

## The 1-2 minute sanity gate (D27)

No full suite as a gate. A `sanity` check is a named command per repository, recorded in the item's room spec / a
hub setting (`flow.sanity` = list of argv, default `make build` plus `go vet ./...` on the changed packages), with a
hard timeout of 120 seconds. It runs on the merger's machine or on a tester the merger picks, and records a
`flow_check` row for the sha. It decides one thing: whether the merger may advance `claude/main` and whether a room may
be deployed. It is `fail`/`pass`/`timeout`, and `timeout` is treated as `fail` for the deploy and as a warning for the
merge. Bigger suites are optional, named checks that may be run on a tester room (UI on sg3, Go on m1mini, D15) and
recorded the same way; nothing blocks on them. Phase: follow-up, the rows exist from phase 1.

## Open points, one recommendation each

1. **Reset vs dated main (19).** Reset with replay and an epoch ref; see above. Reason: no new name for every tool to
   learn, and nothing unreachable.
2. **Work landed between fetch and push (20).** Reconcile by patch equivalence and replay; refuse a non-fast-forward
   `claude/main` unless it consumes an open epoch's reconcile list. Reason: the one rule, "no merge is lost", becomes a
   check the hub makes rather than something the merger remembers.
3. **Git versus reported (Q6).** Table above. Three from git, two derived from sha-keyed records, two on someone's
   word. Reason: an agent forgetting a call can never leave an item in a wrong git stage.
4. **Branch cleanup.** When an item reaches `pushed-by-clint`, its branch is a candidate for deletion on the hub and in
   the room mirrors. Recommendation: atrium LISTS the candidates (`flow gc` dry-run, phase follow-up) and the operator
   confirms; it deletes nothing on its own, because a deletion is the one step here that cannot be undone from git
   (an epoch ref saves `claude/main`, not a work branch). Reason: 70 branches is an annoyance, a wrongly deleted branch
   is lost work.
5. **Does the merger keep tests and deploys (15, 16).** Yes for now, as written in D16: the merger runs the sanity
   gate and records deploys, and may hand a chunk to a tester role. Reason: one linear chain, fewest hops, and the
   `tester` role row exists so the split later costs a handoff and not a redesign.
6. **Nudge and wrapped messages (23, 25).** Out of scope here: other items own the turn-end nudge and the
   untrusted-message wrapping. This design adds no new automatic message: every state change is a tool call by the one
   who does the step, and atrium sends nothing on its own (D22).
7. **Director.** Optional per item; recorded as a role row and as a `designed` report. No new mechanism.

## Phases

Built, in this order, one commit each (migration `0014_merge_flow`, then `0015_flow_epoch`):

1. **Roles and item stages, the rows and the tool.** Migration `0014_merge_flow` (`flow_role`, `flow_item`,
   `flow_check`, `flow_deploy`), `hubstore/flow.go`, the HTTP routes under `/_hub/flow`, the control tool `atrium_flow`
   (full class): `role get/set`, `item get/set`, `check add`, `deploy add`. Stages are reported here, with the order
   and the "only forward" rule enforced.
2. **The merge gate.** `Hub.Merger`, `decide` lets the recorded merger move `claude/main` by fast-forward only,
   everyone else as before, audit lines. Wired in `internal/link`.
3. **Stages from git.** `gitsync.Store` answers `implemented`, `merged` (ancestor or patch-equivalent) and
   `pushed-by-clint` against the hub repository; `flow item get` computes them and advances the cache; derived
   `tested` and `deployed` from the sha-keyed rows.
4. **Epochs: nothing lost between fetch and push.** `flow_epoch` (migration 0015), `epoch`, `epoch_open` and
   `epoch_close` on `atrium_flow`, the `git cherry` plan, the `refs/atrium/epochs/<n>` ref, and the hub-made move
   described above (`gitsync/flowepoch.go`).

Notes on what was built. Stages from git need to know which hub repository to read: the hub setting `flow_repo`
(`atrium_flow repo github.com/dovholuknf/atrium`); unset, a stage is what was reported. `claude/main` pushed by the
merger is checked against `Hub.Merger`, which `atrium_run` wires to the single `merger` role row (none, or more than one,
is nobody). `epoch_close` is checked against the caller's card, resolved from its session name, as every tool here does:
a claim, not a proof, like `X-Atrium-Agent` everywhere.

Follow-ups (not built here): the deploy actuator and its per-room queue; the sanity-gate runner and its setting; the
`flow gc` branch cleanup; tester dispatch across rooms; the director-as-first-merger idea (D26); board UI for stages.
