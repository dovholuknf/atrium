# Room handoff: `atrium move`, a card and its work from one room to another

Status: design by @rnd, 2026-10-01. @review OK at b56ca32f (docs/backlog/rnd/rd-new-review-4ea18bd7.md), with its M3 conditions and lows folded in. Nothing built. Backlog:
`docs/backlog/rnd/rnd-new-room-handoff.md` (HIGH, from the 2026-10-01 director move), with
`docs/backlog/runtime/r-new-move-card-between-rooms.md` (same machine, 2026-09-30) folded in.

## 0. The answer

One verb, `atrium move <card>... <room>`, and one hub MCP tool, `atrium_move`. **The hub drives it, because it is the
only thing that sees both rooms, and it keeps a move record so a restart resumes or undoes it.** It runs in three
phases:

1. **Check everything, move nothing.** Every card on the list is checked against the destination: idle, a clean tree,
   the repo and a git route, the toolchain, the harness and its login, env key names, lean agents, room cap, and,
   for any card with a worktree, a landing route unless `--no-land` is given. One failure refuses the whole list,
   and nothing has changed. The check is a forecast: every row that can change is checked again where it is used.
2. **Prepare every card, reversibly.** For each card: wait for idle, **freeze** it (input refused, everything sent to
   it into one queue), check it is clean again, capture a handoff, carry the branch, the conversation, the project
   memory and the card's atrium files, park the old runner, launch the successor with `resume` and every carried
   field, and check that it answers. A failure on any card undoes every card: the successors are ended, the old
   cards are unparked and unfrozen, and their queues are replayed on A.
3. **Cut every card over**, only when every card is prepared: the successor takes the alias, the old card gets a
   `moved_to` link and forwards its queue by id with an ack, children are re-pointed, and the old card is marked
   done. Each step is idempotent and retried from the record.

At no point are two live sessions on one conversation: the old runner is parked before the successor resumes.

**The conversation is carried by default.** The successor resumes a copy of the old transcript. A handoff is
written first in any case, so `--fresh` (start cold from it) is always there, and it is the fallback when the
transcript cannot be resumed. **Anything sent to the old card follows it**: says, reports and `report_to` go
through one lookup that follows `moved_to`, and the old card's children are re-pointed.

**A director move is only useful with a landing route.** Code reaches the destination through the hub (git-sync
stage 1, built). Landing does not. Until git-sync stage 3 (the hub merge queue), the check refuses a card with a
worktree unless the destination has the interim landing route of section 6, a hub op with four rules written in.
That is the half the 2026-10-01 move was missing.

The same-machine case of r-new-move-card is this flow with the git and transcript steps skipped, because the cwd and
the home folder are the same.

## 1. What is there today

- **Nothing in code links two cards.** No `moved_to`, successor or supersedes field. `docs/rnd/multi-room-design.md`
  says plainly at :91 "A card cannot move between rooms", and lists what a move would need.
- **The hand move** (r-new-move-card, `move.sh`) works between two rooms on one machine: read the card, exit it, launch on B with
  `resume`, then PATCH the fields. It loses `pin_order` (only `POST /v1/tasks/pin-order` sets it, `api/api.go:490`),
  the history, and any link.
- **`docs/fabric/f-004-two-rooms-design.md` section 3** is a cross-machine move script, accepted and not built:
  check idle and clean, carry the branch through a narrow hub op, exit, copy the jsonl over ssh, launch with
  `resume`, mark the old card `moved to B~<id>` in its recap, tell the launcher. This design takes its steps and
  makes them one hub operation with no ssh. Its note that the session id is the one from the card's LAST
  SessionStart, not the stored `resume_id` that lags after a `/clear`, holds here.
- **Cross-room launch**: MCP `atrium_launch room=` (`link/control_mcp.go:1365`) posts `/v1/launch` with
  `X-Atrium-Room`. The room's `LaunchRequest` (`daemon/launch.go:22`) accepts `resume`, `task_id`, `report_to` and
  `repo`, but the MCP path sends none of them. That is why the hand move could not resume and the cards on m1mini
  show the wrong repo.
- **The wrong repo label**: `InferRepo` (`store/store.go:470`) takes the second segment after `github`, which is
  `atrium-worktrees` for every m1mini worktree. The stored `Repo` from `ReadGitInfo` would be right, but a launch
  that sends no `repo` and a worktree it cannot read leave it empty.
- **A say to an exited card** is `undeliverable` (`daemon/park.go:177`, `nosession.go:36`). Nothing is held and nothing
  is redirected. **A report** goes to `launcherOf` (`daemon/a2a.go:243`): `report_to`, then `spawned_by_id`, then the
  handle. `SetLineage` is write-once. `SetLauncher` overwrites, and its only caller is the relaunched-director path
  in `reportto.go:77`.
- **Aliases** are unique per room among live cards (`store/alias.go:90`). Across rooms they are not, and
  `resolvePeer` (`control_mcp.go:702`) errors when two rooms match, which a done card that still holds the alias on A
  can cause.
- **Git**: git-sync stage 1 is built and live (`internal/gitsync`, `link/git_hub.go`). The hub holds a bare repo,
  rooms sync `claude/main`, and the hub collects rooms' `claude/*`. Stage 2 (a worktree per card) and stage 3 (the
  merge queue, needing clint's word) are not built. `claude/landing` is not in code. It was made up on 2026-10-01.
- **Room requirements**: `atrium.requirements.yaml`, `POST /v1/preflight` and `room-check` are built
  (`docs/fabric/room-requirements-design.md` section 7). Section 6 of that doc says matching a card to a room is the
  next step, and the check here is that step.
- **Context capture**: `daemon/newcontext.go` writes `HANDOFF.<alias>.md` with an `atrium-capture:` line, and is
  built. A move uses it as is.

## 2. The check: refuse before anything moves

`atrium move` and `atrium_move` run every check for every card first, and answer with the full list of failures.
Nothing is frozen, exited, launched or written until the whole list passes. `--check` runs the check alone.

| # | Check | How | Refusal says | Checked again |
| --- | --- | --- | --- | --- |
| 1 | Idle | status not running, not mid new-context (`newcontext.go` state) | `busy: <card> is mid-turn` | after the freeze (3.2) |
| 2 | Clean tree | `git status --porcelain` on A, through A's daemon | `dirty: <n> files`. `--wip` commits them as `WIP: move to <room>` | after the freeze (3.2) |
| 3 | Destination up | B attached, not retiring (`startsNothing`, `link/proxy.go:1153`) | `room <B> is not attached` or `retiring` | before each launch |
| 4 | Harness | B has the card's runner row, and `POST /v1/preflight` `runner_auth` passes | `<B> has no runner <r>` or `not logged in` | before each launch |
| 5 | Repo and git route | B's hello says it takes git syncs, the hub's `git_repos` holds the card's repo, and B's clone is at `claude/main` | `<B> cannot get <repo>` | at the branch carry |
| 6 | Toolchain | B's preflight against the repo's `atrium.requirements.yaml` | each failing check by name | |
| 7 | Env | every key in the card's `LaunchEnvKeys` is present in B's room env (`env_present`, names only) | `<B> lacks env <KEY>` | |
| 8 | Lean kit | every `atrium:agent:<n>` and `atrium:skill:<n>` tag exists on B | `<B> has no agent <n>` | |
| 9 | Room cap | B's running count plus the whole list fits its cap (5) | `<B> is full` | before each launch |
| 10 | Conversation | the transcript of the last SessionStart is readable on A and under the hub's transfer cap | `transcript too large`. `--fresh` skips it | at the carry |
| 11 | Landing route | every card with a git worktree, unless `--no-land`: B has section 6's route | `<B> cannot land: no landing route` | |
| 12 | Who asks | the operator, the orchestrator, or the card's own launcher | `not yours to move` | |

Row 11 fails closed. It is not keyed on a tag, which an untagged director would slip past. A worker that never lands
is moved with `--no-land`, and the mover says so.

Row 7 checks names in B's room env only. A card launched with a per-card env value (`LaunchEnv`, whose values never
leave A) gets B's room value for that name, or none. The move does not reproduce per-card values.

## 3. The move

### 3.1 The move record

The hub writes one record per card before it touches anything: `{move id, list id, card, from, to, step, new id,
queue acks, started, updated}`, in the hub store. Every step below writes its result before the next starts. At hub
start, each unfinished record is resumed from its step if it was in the cut-over (phase 3), and undone if it was
not. Every step is idempotent by move id: the successor's launch uses a `task_id` derived from the move id, so a
repeat returns the same card; writes on B overwrite; a re-sent say carries its id and B drops a duplicate.

A frozen card also carries a lease (default 30 minutes, renewed by the hub while the move runs). If the hub is gone
past the lease and no cut-over began, A undoes the card on its own: unparks it, unfreezes it and replays its queue.
A hub that comes back to such a record ends the successor on B. The self-undo and the hub's `moved_to` set (3.3
step 1) are one transaction on A: once the lease has run out a `moved_to` set is refused, and once `moved_to` is set
the self-undo is refused, so the two can never both happen.

### 3.2 Phase 2: prepare every card

For each card in the list, in order:

1. **Wait for idle.** If the card is mid-turn, wait up to `--wait` (default 10 minutes) for the turn to end, then
   refuse the whole list and undo any card already prepared. `--force` stops the turn, which leaves mid-task, as an
   exit always does.
2. **Freeze.** From here to the cut-over the old card takes no turn. Its terminal input is refused, as a new-context
   cycle already refuses it (`terminal-links.js:1859`). Says, reports, stop and context notices, nags and restart
   wakes all go to one freeze queue, each with its id, through `launcherOf`, `notifyLauncher` and the say path alike.
   Nothing is typed in.
3. **Check again.** Idle (the freeze landed on an idle card) and clean (row 2, or `--wip` commits now). A failure
   undoes the list.
4. **Capture.** The context capture writes `HANDOFF.<alias>.md` in the cwd, as a new-context cycle does. It is the
   cold-start fallback and the successor's read-me either way.
5. **Carry the branch.** The tree is now final. The hub collects A's `claude/<branch>` (stage 1, built), and a narrow
   op pushes it to B's clone, allowed only for a card with a live move record (f-004 step 2). B makes the worktree at
   the same name. On one machine the cwd stays and this step is skipped.
6. **Carry the conversation and the files.** A reads the jsonl of the card's last SessionStart, the project memory
   folder (`~/.claude/projects/<encoded cwd>/memory/`), and the untracked atrium files in the cwd (`BRIEF.md`,
   `HANDOFF.*.md`). The hub streams them to B. B writes them only through `internal/safepath` and an allowlist of
   names: the jsonl under its own encoded cwd, the memory folder beside it, and the two file patterns into the new
   worktree. No ssh and no keys. On one machine, with the same home folder, this step is skipped.
7. **Park the old runner.** A parks the card as an idle park does: the process exits, and the card stays, frozen,
   resumable. From here only the successor can run the conversation, so the `ResumeBusy` fork cannot happen across
   machines.
8. **Launch and check.** B launches the successor through `/v1/launch` with `task_id` from the move id, `resume` (or
   none with `--fresh`), `cwd`, `repo` (the fix for the label), harness, model, effort, args, title, why, tags (the
   lean tags among them), theme, sound, icon, overrides, `peer_typing`, gated and auto-approve, `report_to`, the old
   card's lineage, and `moved_from = A~<old id>`. Its first prompt is one paragraph: you moved from A to B, your
   worktree is now `<path>`, paths in your history under `<old path>` are now under it, read `HANDOFF.<alias>.md`,
   and answer this with `atrium_say`. The hub waits for that say (default 3 minutes). The successor holds no alias
   yet and is told not to act on anything until the cut-over note. It is launched gated with nothing
   auto-approved, whatever the old card had. The carried gate and auto-approve settings are applied at 3.3 step 6.

If any card fails at any step, the hub undoes every card in the list, newest first: it ends the successor on B,
unparks the old card on A (the ordinary resume of its own transcript), unfreezes it, and replays its freeze queue
there in order.

### 3.3 Phase 3: cut every card over

Only when every card in the list is prepared. For each card, in this order, each step retried from the record:

1. **A sets `moved_to = B~<new id>`** on the old card. From now on its freeze queue forwards, it does not replay.
2. **B takes the alias.** The hub sets it on the successor while the old card still holds it on A. `resolvePeer`
   (`control_mcp.go:702`) is changed to prefer, when two rooms hold one alias, the card that another's `moved_to`
   names. So a say by alias reaches the successor from this moment, with no gap. Then pins, `pin_order` and the pin
   group, through B's pin-order endpoint.
3. **Forward the queue.** A sends its freeze queue to B through the hub relay, each say with its id. B delivers in
   order, dedups by id, and acks each one. A drops a say only on its ack. The freeze queue stays open, and anything
   that arrives for the old card up to the end of step 5 is forwarded the same way.
4. **Re-point children.** The hub calls `SetLauncher` for every child whose `spawned_by_id` is `A~<old id>`, on any
   room.
5. **Close the old card.** A releases its alias with `alias_note` "moved to B~<id>", marks the card done, and keeps
   its handle reserved while `moved_to` is set, so no later card on A takes it.
6. **Tell.** The successor is told it is live, and the carried gate and auto-approve settings are applied. The hub posts one line to the old card's launcher and to the mover:
   `<alias> moved A~<old> to B~<new>`.

If the successor dies after it answered and before step 1, the record undoes the card as in 3.2, and the list's other
cards are undone with it. After step 1 the cut-over only goes forward: a dead successor is a card on B with a resume
id, and the hub resumes it by the ordinary reopen path, then finishes the steps.

## 4. What carries, and what does not

| Part | Carries | How |
| --- | --- | --- |
| Alias | yes | taken on B first, released on A at the end (3.3) |
| Pin, pin order, pin group | yes | B's pin-order endpoint, the group as its tag |
| Tags, theme, sound, icon, overrides, peer typing | yes | the launch body |
| Harness, model, effort, args, lean agents and skills, gate settings | yes | the launch body. Lean is rebuilt from the tags, as a resume does |
| Env | names only | row 7. A per-card value is not reproduced. B's room value is used, or none |
| `report_to`, lineage | yes | the launch body. `SetLineage` stays write-once and is set once, at launch |
| The conversation | yes, by default | the transcript, 3.2 step 6. `--fresh` starts from the handoff |
| Project memory | yes | `~/.claude/projects/<encoded cwd>/memory/`, with the transcript |
| `BRIEF.md`, `HANDOFF.*.md` | yes | with the transcript, through safepath and a name allowlist |
| Committed work | yes | the branch, 3.2 step 5, after the freeze |
| Uncommitted work | no | refused, or `--wip` commits it after the freeze |
| History, events, recap | linked, not copied | the old card stays `done` on A with `moved_to`. The new card's `moved_from` opens it through the hub |
| The process and the pty | no | the old runner is parked, a new one resumes on B |

**The link.** The new card has `moved_from`, and the old card has `moved_to`. The board shows "came from A~<id>" on
the new card and "moved to B~<id>" on the old one. Each opens the other through the hub. A card moved twice is a
chain.

## 5. Anything addressed to the old card follows it

One rule covers all of it: **every card lookup goes through one function that follows `moved_to`.** It follows at
most 8 hops, keeps the ids it has seen, and answers `move chain loops at <id>` rather than spin. A to B and back to A
is a normal chain, ending at the live card.

- **Says.** `sayGate` on A, for a card with `moved_to`, forwards through the hub relay to the end of the chain and
  answers `forwarded: moved to B~<id>`, so the sender learns the new address. A `name@A` or `A~<id>` that a script
  holds keeps working.
- **Reports.** `launcherOf` resolves `report_to` first (`currentLauncher`, `daemon/a2a.go:250`), then
  `spawned_by_id`. Both answers go through the lookup, so a child whose `report_to` names the old card by handle or
  id follows the chain too. The re-point of 3.3 step 4 only fixes the `spawned_by` fallback, which `SetLauncher`
  writes. It makes the common case one hop, and the lookup catches the rest.
- **`report_to` by handle.** It resolves by stored id where one exists. The old handle stays reserved on A while
  `moved_to` is set (3.3 step 5), so a later stranger cannot take it.
- **Scripts and notes.** The orchestrator's `send-directors.sh` held card ids, which went dead. The fix is outside
  atrium: address directors by alias through the hub, which follows the move.

## 6. Landing from the destination, and git-sync stage 3

A worker's code needs no landing. Its director reads the branch the hub collected. A director's code does: it is
merged onto `claude/main`, which today is only writable in sg4's checkout. On 2026-10-01 that is what held the move
halfway.

- **With git-sync stage 3**, the hub merge queue: a director on any room requests a landing, and the hub lands it in
  order with checks. A moved director can land from anywhere. That is the full answer, and it needs clint's word
  (git-sync section 11).
- **Until then, the interim route**: the `claude/landing` improvised on 2026-10-01, made into a hub op enabled per
  room, with four rules written into it:
  1. **One writer per repo.** The op takes a hub lock per repo for the whole landing, so two directors, on one room
     or two, cannot land at once. Every m1mini worktree shares one clone and one `claude/landing`, and the lock is
     what stops them racing on it.
  2. **Fast-forward only, then reset.** The hub moves `claude/main` only by fast-forward to the landing tip. After
     each landing, the room's `claude/landing` is reset to the new `claude/main`, so a refused or stale commit never
     rides into the next landing.
  3. **A mechanical verdict check.** Before the fast-forward, every non-merge commit in `claude/main..landing` must
     be covered by an `Atrium-Verdict` OK range (the trailer `internal/deployready` already reads), or be a verdict
     commit itself, or touch only `docs/backlog/*/QUEUE.md` (the queue exemption). Anything uncovered refuses the
     landing and names the commits. It is not the orchestrator's eye.
     - Design and doc reviews get their own trailer kind, `Atrium-Verdict: doc-ok <base>..<tip>`, which @review writes
       from now on. Without it this rule would refuse every doc commit, this design included.
     - A merge commit is accepted only if its tree equals `git merge-tree` of its parents, so an evil merge cannot
       carry unreviewed content.
     - A trailer is text, and anyone on an unsigned room can write one. A verdict counts only from a commit that
       touches only review files (`docs/backlog/*/rd-new-review-*.md`, `docs/backlog/review/**`). That makes a
       forged verdict visible in the diff, not impossible. Signing @review's commits is what makes it a guarantee.
  4. **One room at a time.** Under the lock, a room's landing is cut from the current `claude/main`. A branch that
     is no longer a fast-forward is refused, not rebased by the op. Its director rebases, which changes the SHAs, so
     @review re-stamps the verdict for the new range before it lands.

  A room "has a landing route" (check row 11) when that op is enabled for it.

**The move does not need stage 3. A director move needs one of the two.** Without either, the check refuses, which is
better than the halfway state of 2026-10-01.

## 7. Same machine, and the rolling restart

r-new-move-card's case, two rooms on one machine, is section 3 without 3.2 steps 5 and 6. The branch, the transcript
and the memory are already where B can read them. f-011's rolling restart (`docs/rnd/rolling-restart-design.md`) is this case done at
each card's quiet moment, so it calls the same move, and its hazard of a second room taking the hooks is answered
there, not here. The two items stay one design with one primitive. f-004 section 3's script is replaced by the hub
op.

## 8. Stages

### M1. The room side. @runtime. About 2 days.

- `moved_to` and `moved_from` columns, and the freeze: terminal input refused, and says, reports, notices, nags
  and wakes into one queue with ids, with a lease.
- One card lookup that follows `moved_to` (8 hops, cycle check), used by `sayGate`, `currentLauncher` and the
  `spawned_by` fallback alike. The old handle stays reserved while `moved_to` is set.
- Forwarding the freeze queue by id, with dedup and ack on the receiving side.
- Launch accepts `moved_from`, `repo`, `pin_order`, the old card's lineage, and a `task_id` that makes a repeat
  launch return the same card.
- `InferRepo` reads a `<repo>-worktrees/<name>` folder as `<repo>`, so the label is right even without `repo`.
- The pieces of section 3 that A and B each run (freeze, check again, capture, read the transcript, memory and
  files, write them through safepath and the allowlist, park, launch, mark, undo), as idempotent room routes the hub
  calls.
- **Acceptance:** two rooms on one machine. A throwaway worker with a child moves A to B through the room routes,
  driven by hand. A say sent during the freeze arrives once on the new card. A say to `A~<old>` after the move
  arrives with `forwarded`. A child whose `report_to` is the old card's handle reports to the new card. Typing into
  the old card during the freeze is refused. An undo after the launch leaves A as it was, queue replayed. The pin
  order is the same, and the label shows `dovholuknf/atrium`.

Useful alone: it replaces `move.sh` for the same-machine case, and fixes the label.

### M2. The hub verb. @fabric. About 3 days. After M1.

- `atrium_move` on the hub and `atrium move` in the CLI: the check of section 2, then section 3 driven end to end.
- The move record in the hub store, resumed or undone at hub start, and the lease renewal.
- `resolvePeer` prefers the card a `moved_to` names when two rooms hold one alias.
- The narrow branch-carry op (f-004 step 2) and the transcript and file stream through the hub.
- **Acceptance:** a throwaway worker on sg3 commits, moves to m1mini, resumes its conversation, commits again, and
  reports to its launcher on a third room. A move with one failing check moves nothing and names the check. The hub
  is restarted once mid-prepare (the move is undone) and once mid-cut-over (the move finishes).

Useful alone: any card moves between machines with no ssh.

### M3. A list, and the landing route. @fabric, with the orchestrator. About 1 day. After M2.

- `atrium move` takes a list, or `--tag role:director`: checked as a whole, every card prepared, then every card cut
  over (section 3).
- The interim landing route of section 6, as a hub op enabled per room, with its four rules, until stage 3.
- **Acceptance:** "move the directors to m1mini". Either all five move, answer, and one lands a doc commit through
  the route, or one check is made to fail and nothing moves. A third run makes card 3's launch fail, and cards 1
  and 2 are undone. A landing with an uncovered commit is refused and names it.

Useful alone: the 2026-10-01 move, in one call.

## 9. Questions for clint

1. **The conversation: carried or cold?** Carried by default (the successor resumes a copy of the transcript), with a
   handoff always written so `--fresh` is there. Or always cold from the handoff, which is cheaper on context but
   loses the thread? **Default: carried.**
2. **Landing until stage 3.** Make the improvised `claude/landing` route the rule (section 6), or approve git-sync
   stage 3 now, so that a moved director lands through the merge queue? **Default: the interim route now, and stage 3
   on its own schedule.**
3. **The old card: left done, or archived?** **Default: done, with `moved_to`,** so its history stays one click away.
   The normal cull archives it later.
4. **A busy card.** Wait for idle up to 10 minutes, then refuse. Or refuse at once? `--force` is there either way.
   **Default: wait 10 minutes.**
5. **Who may move a card.** The operator, the orchestrator, and the card's own launcher (so a director can move its
   workers). Or the operator and the orchestrator only? **Default: all three.**
6. **A list is all or nothing.** Every card in a list stays frozen until the whole list is prepared, so five
   directors are frozen together for a few minutes, and one failure undoes all five. Or move card by card, where a
   failure stops the list and reports which cards moved? **Default: all or nothing,** which is what "move the
   directors, or refuse before anything moved" asks for.
