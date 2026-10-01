# Room handoff: `atrium move`, a card and its work from one room to another

Status: design by @rnd, 2026-10-01, for @review and then clint. Nothing built. Backlog:
`docs/backlog/rnd/rnd-new-room-handoff.md` (HIGH, from the 2026-10-01 director move), with
`docs/backlog/runtime/r-new-move-card-between-rooms.md` (same machine, 2026-09-30) folded in.

## 0. The answer

One verb, `atrium move <card>... <room>`, and one hub MCP tool, `atrium_move`. **The hub drives it, because it is the
only thing that sees both rooms.** It runs in two halves:

1. **Check everything, move nothing.** Every card on the list is checked against the destination: idle, a clean tree,
   the repo and a git route, the toolchain, the harness and its login, env key names, lean agents, room cap, and,
   for a card that lands work, a landing route. One failure refuses the whole list, and nothing has changed.
2. **Move each card**, in order: carry the branch, wait for idle, capture a handoff, carry the conversation and the
   card's untracked atrium files, launch the successor with `resume` and every carried field, check that it answers,
   then cut over: alias, a two-way link between old and new, forwarding, children re-pointed, the old card exits.
   Until the cut-over, a failure undoes itself: the successor is ended and the old card never knew.

**The conversation is carried by default.** The successor resumes a copy of the old transcript, and a handoff is
written first in any case, so `--fresh` (start cold from that handoff) is always available and is the fallback when
the transcript cannot be resumed. **Anything sent to the old card follows it**: says, reports and `report_to` go
through a `moved_to` link, and the old card's children are re-pointed.

**A director move is only useful with a landing route.** Code reaches the destination through the hub (git-sync
stage 1, built). Landing does not. Until git-sync stage 3 (the hub merge queue) exists, the check refuses to move a
card that lands work unless the destination has the interim landing route of section 6. That is the half the
2026-10-01 move was missing.

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
Nothing is exited, launched or written until the whole list passes. `--check` runs the check alone.

| # | Check | How | Refusal says |
| --- | --- | --- | --- |
| 1 | Idle | status not running, not mid new-context (`newcontext.go` state) | `busy: <card> is mid-turn` (or `--wait <dur>`, section 3) |
| 2 | Clean tree | `git status --porcelain` on A, through A's daemon | `dirty: <n> files`. `--wip` commits them as `WIP: move to <room>` |
| 3 | Destination up | B attached, not retiring (`startsNothing`, `link/proxy.go:1153`) | `room <B> is not attached` or `retiring` |
| 4 | Harness | B has the card's runner row, and `POST /v1/preflight` `runner_auth` passes | `<B> has no runner <r>` or `not logged in` |
| 5 | Repo and git route | B's hello says it takes git syncs, the hub's `git_repos` holds the card's repo, and B's clone is at `claude/main` | `<B> cannot get <repo>` |
| 6 | Toolchain | B's preflight against the repo's `atrium.requirements.yaml` | each failing check by name |
| 7 | Env | every key in the card's `LaunchEnvKeys` is present on B (`env_present`, names only, values never leave A) | `<B> lacks env <KEY>` |
| 8 | Lean kit | every `atrium:agent:<n>` and `atrium:skill:<n>` tag exists on B | `<B> has no agent <n>` |
| 9 | Room cap | B's running count plus the list fits its cap (5) | `<B> is full` |
| 10 | Conversation | the transcript of the last SessionStart is readable on A and under the hub's transfer cap | `transcript too large`. `--fresh` skips it |
| 11 | Landing route | only for a card that lands work (tag `role:director` or `lands`): B has section 6's route | `<B> cannot land: no landing route` |
| 12 | Who asks | the operator, the orchestrator, or the card's own launcher | `not yours to move` |

A list of cards is checked as a whole: the cap counts all of them, and one failure refuses the whole list. That is
the "move the directors to m1mini, or a refusal before anything moved" of the backlog item.

## 3. The move, one card at a time

The hub runs these steps for each card in order. Steps 1 to 6 change nothing a reader of the old card can see. Step 7
is the cut-over.

1. **Hold.** A marks the card `moving` (a state, not a status). New says to it are queued, not typed in. The card
   keeps working, because a hold is not an exit.
2. **Carry the branch.** A commits nothing on its own. If `--wip` was given it makes the WIP commit. The hub collects
   A's `claude/<branch>` (stage 1, built) and a narrow op pushes it to B's clone, allowed only for a card being
   moved (f-004 step 2). B makes the worktree at the same name. On one machine the cwd stays and this step is skipped.
3. **Wait for idle.** If the card went busy after the check, the move waits up to `--wait` (default 10 minutes) for
   the turn to end, then gives up and releases the hold. `--force` exits mid-turn, which leaves mid-task, as an exit
   always does.
4. **Capture.** The context capture writes `HANDOFF.<alias>.md` in the cwd, as a new-context cycle does. It is the
   cold-start fallback and the successor's read-me either way.
5. **Carry the conversation and the files.** A reads the jsonl of the card's last SessionStart and the untracked
   atrium files in its cwd (`BRIEF.md`, `HANDOFF.*.md`). The hub streams them to B, which writes the jsonl under its
   own encoded cwd (`~/.claude/projects/<B's cwd, encoded>/<id>.jsonl`) and the files into the new worktree. No ssh
   and no keys. On one machine, with the same home folder, this step is skipped.
6. **Launch and check.** B launches the successor through `/v1/launch` with `resume` (or none with `--fresh`), `cwd`,
   `repo` (the fix for the label), harness, model, effort, args, title, why, tags (the lean tags among them), theme,
   sound, icon, overrides, `peer_typing`, gated and auto-approve, `report_to`, the old card's lineage, and
   `moved_from = A~<old id>`. Its first prompt is one paragraph: you moved from A to B, your worktree is now
   `<path>`, and paths in your history under `<old path>` are now under it. Read `HANDOFF.<alias>.md`. Answer this
   with `atrium_say`. The hub waits for that say (default 3 minutes). No answer means the move failed: B ends the
   successor, A releases the hold, and the queued says are typed in on A as if nothing happened.
7. **Cut over**, once the successor has answered:
   - A sets `moved_to = B~<new id>` on the old card, releases its alias with `alias_note` "moved to B~<id>", and exits
     it. The old card is left `done`, not archived. Its history, recap and events stay on A, readable through the
     link.
   - B sets the alias on the successor, and `pin`, `pin_order` and the pin group through the pin-order endpoint.
   - The hub re-points every child whose `spawned_by_id` is `A~<old id>`, on any room, with `SetLauncher`, so its
     next report goes straight to the new card.
   - A does not type the says it queued during the hold. It forwards them to the new card through the hub relay,
     exactly once, and drops them from its own queue.
8. **Tell.** The hub posts one line to the old card's launcher and to the mover: `<alias> moved A~<old> to B~<new>`.

## 4. What carries, and what does not

| Part | Carries | How |
| --- | --- | --- |
| Alias | yes | released on A at the cut-over, set on B |
| Pin, pin order, pin group | yes | B's pin-order endpoint, the group as its tag |
| Tags, theme, sound, icon, overrides, peer typing | yes | the launch body |
| Harness, model, effort, args, lean agents and skills, gate settings | yes | the launch body. Lean is rebuilt from the tags, as a resume does |
| Env values | no | names are checked (row 7). B uses its own values. Values never leave a room |
| `report_to`, lineage | yes | the launch body. `SetLineage` stays write-once and is set once, at launch |
| The conversation | yes, by default | the transcript, section 3 step 5. `--fresh` starts from the handoff |
| `BRIEF.md`, `HANDOFF.*.md` | yes | with the transcript |
| Committed work | yes | the branch, step 2 |
| Uncommitted work | no | refused, or `--wip` commits it |
| History, events, recap | linked, not copied | the old card stays `done` on A with `moved_to`. The new card's `moved_from` opens it through the hub |
| The process and the pty | no | a new runner resumes on B, so the move waits for idle |

**The link.** The new card has `moved_from`, and the old card has `moved_to`. The board shows "came from A~<id>" on
the new card and "moved to B~<id>" on the old one. Each opens the other through the hub. A card moved twice is a
chain, and every lookup follows the chain to its end.

## 5. Anything addressed to the old card follows it

One rule covers all of it: **a lookup that lands on a card with `moved_to` follows it.**

- **Says.** `sayGate` on A, for a done card with `moved_to`, forwards through the hub relay to the new card and
  answers `forwarded: moved to B~<id>`, so the sender learns the new address. A `name@A` or `A~<id>` that a script
  holds keeps working until it is fixed. A new say queued during the hold is carried as in step 7.
- **Reports.** `launcherOf` follows `moved_to` when the launcher it finds is a moved card. The re-pointing in step 7
  makes that one hop for every known child, and the rule catches a child the hub could not reach then.
- **`report_to`** names a card. It resolves through the same lookup, so it follows too.
- **Scripts and notes.** The orchestrator's `send-directors.sh` held card ids, which went dead. The fix is outside
  atrium: address directors by alias through the hub, which follows the move. The design says so for the
  orchestrator to change.

**A two-room alias clash cannot happen.** The old card releases its alias before the new one takes it, so
`resolvePeer` never sees two.

## 6. Landing from the destination, and git-sync stage 3

A worker's code needs no landing. Its director reads the branch the hub collected. A director's code does: it is
merged onto `claude/main`, which today is only writable in sg4's checkout. On 2026-10-01 that is what held the move
halfway.

- **With git-sync stage 3**, the hub merge queue: a director on any room requests a landing, and the hub lands it in
  order with checks. A moved director can land from anywhere. That is the full answer, and it needs clint's word
  (git-sync section 11).
- **Until then, the interim route**, which is what was improvised on 2026-10-01, made into a hub op: a room may push
  one branch, `claude/landing`, cut from `claude/main`. The hub collects it like any `claude/*`. The orchestrator, or
  later the hub, fast-forwards `claude/main` to it when it is a fast-forward and @review has cleared it. A room
  "has a landing route" (check row 11) when that op is enabled for it.

**The move does not need stage 3. A director move needs one of the two.** Without either, the check refuses, which is
better than the halfway state of 2026-10-01.

## 7. Same machine, and the rolling restart

r-new-move-card's case, two rooms on one machine, is steps 1, 3, 4, 6, 7 and 8. The branch and the transcript are
already where B can read them. f-011's rolling restart (`docs/rnd/rolling-restart-design.md`) is this case done at
each card's quiet moment, so it calls the same move, and its hazard of a second room taking the hooks is answered
there, not here. The two items stay one design with one primitive. f-004 section 3's script is replaced by the hub
op.

## 8. Stages

### M1. The room side. @runtime. About 2 days.

- `moved_to` and `moved_from` columns, and the `moving` hold.
- `sayGate` forwards for a moved card, `launcherOf` follows `moved_to`, and the old alias is released at the mark.
- Launch accepts `moved_from`, `repo`, `pin_order`, and the old card's lineage.
- `InferRepo` reads a `<repo>-worktrees/<name>` folder as `<repo>`, so the label is right even without `repo`.
- The pieces of section 3 that A and B each run (hold, capture, read the transcript and files, write them, launch,
  mark), as room routes the hub calls.
- **Acceptance:** two rooms on one machine. A throwaway worker with a child moves A to B through the room routes,
  driven by hand. A say to `A~<old>` arrives on the new card with `forwarded`. The child's next report reaches the
  new card. The pin order is the same, and the label shows `dovholuknf/atrium`.

Useful alone: it replaces `move.sh` for the same-machine case, and fixes the label.

### M2. The hub verb. @fabric. About 3 days. After M1.

- `atrium_move` on the hub and `atrium move` in the CLI: the check of section 2, then section 3 driven end to end.
- The narrow branch-carry op (f-004 step 2) and the transcript and file stream through the hub.
- **Acceptance:** a throwaway worker on sg3 commits, moves to m1mini, resumes its conversation, commits again, and
  reports to its launcher on a third room. A move with one failing check moves nothing and names the check.

Useful alone: any card moves between machines with no ssh.

### M3. A list, and the landing route. @fabric, with the orchestrator. About 1 day. After M2.

- `atrium move` takes a list, and `--tag role:director`, checked as a whole (section 2).
- The interim landing route of section 6, as a hub op that is enabled per room, until stage 3.
- **Acceptance:** "move the directors to m1mini". Either all five move, answer, and one lands a doc commit through
  the route, or one check is made to fail and nothing moves.

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
