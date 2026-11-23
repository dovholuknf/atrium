# Room move spike: everything a real move needs, and the first stage built

Status: spike. Design note plus a first stage, `atrium move <card> --to <room>`, built on the room routes of M1
(`internal/daemon/moveroutes.go`). Branch `claude/room-move-spike`. The reviewed design is
`docs/rnd/room-handoff-design.md`, and this note does not repeat it. It covers what that design leaves out or
defers, says what the spike picks, and records what the test on sgg showed.

## 0. Where the code stood

The brief said none of the move was in code. That was stale. M1 is built: `moved_to` and `moved_from`, the freeze
with its queue and lease, the one lookup that follows `moved_to`, and fourteen idempotent room routes under
`/v1/move/` (freeze, renew, check, capture, bundle, park, moved, queue, ack, release, undo on A, and receive,
deliver, adopt on B). Launch takes `moved_from`, `pinned` and `pin_order`. What was missing is the driver (M2) and
the two halves M1 skipped: carrying the branch and carrying the work tree to a room with a different card root.

## 1. The verb

```
atrium move <card> --to <room> [--from <room>] [--wip] [--fresh] [--no-capture] [--check] [--url <hub>]
atrium move <card> --from-url <roomA> --to-url <roomB>      # no hub: rooms addressed directly
atrium move --undo <move id>                                 # roll back from the local record
atrium move --resume <move id>                               # finish a move past the launch, from the local record
```

The driver runs in the CLI, not on the hub. The hub only relays `/v1/*` to a room by `X-Atrium-Room`, which it
already does for every route. That is the smallest thing that works, and it means a move needs no hub change. The
cost is that the move record lives in a file beside the CLI (`~/.atrium/moves/<move id>.json`), not in the hub's
store. M2 moves the same steps into a hub op, and the record with them. The steps and their order do not change.

## 2. Tool parity on the target

A card does not need "the same tools as A". It needs what its project needs, and that is already written down:
`atrium.requirements.yaml` and `POST /v1/preflight` (built). The rule:

| Tool | Where it is asked for | Missing on B |
| --- | --- | --- |
| git | always: the branch carry needs it on both rooms | refuse. Git for Windows only on Windows (a Cygwin or MSYS git writes `/cygdrive` gitfiles) |
| the harness (claude, codex) | the card's `runner` row on B, and `runner_auth` in the preflight | refuse: `<B> has no runner <r>`, or `not logged in` |
| go, node, python | the repo's `atrium.requirements.yaml` `toolchain:` | refuse with the check's name and its fix line. `--force-tools` moves anyway and writes the gap into the successor's first prompt |
| gh | `toolchain:` when the repo's work uses it (a forge card) | as above |
| playwright and its browsers | `toolchain:` with a `check:` command (`npx playwright --version`), since it lives in `node_modules` and a browser cache, not on PATH | as above. Never installed by the move |

A move never installs anything. Installing is `room-toolchain.ps1` and the requirements `fix` lines, run by a person
or by the room's own provisioning, because a move that installs is a move that can half-fail on a package mirror.
A repo with no requirements file gets the always rows only (git and the harness), and the move says so.

The spike checks two rows before it freezes anything: B is attached, and B has the card's runner row. git is not
probed. A room without it fails at `check`, `gitpack` or `worktree`, and the move undoes. The git probe and the
requirements preflight are the next rows to wire.

## 3. ssh, and the card's state between rooms

**No ssh, by default and in the spike.** Rooms dial the hub, never the reverse, and the hub holds no keys to the
rooms. Every byte of a move goes room to hub to room over the link that is already there: the move bundle (the
transcript, the memory, the card's files) and the git bundle. Each is capped at 64 MiB (`maxMoveBundle`), refused
at A with a 413. JSON carries the bytes as base64, so a route reads up to 4/3 of that (`maxMoveBody`), and a body
over it is a 413 at B. Every transcript seen so far fits.

ssh is the fallback for one case: **the hub is down**. Then the operator carries a pack by hand:

```
atrium move export <card> --out card.move     # on A: freeze, check, capture, bundle, park, and write one file
scp card.move b:                               # any transport the operator has
atrium move import card.move                   # on B: receive, land the branch, launch with moved_from
atrium move finish <move id>                   # on A, once B answered: moved_to, release
```

The pack is the same JSON the routes pass (the move bundle plus the git bundle), so it is one format, not two. Not
built in the spike. It is listed as stage S3 below.

## 4. Uncommitted work and the card's files: git for the tree, a copy for the rest

Picked: **git carries everything git can see, and the move bundle carries the few files git must not see.**

- **Tracked changes and untracked files that are not ignored** are committed on the card's branch after the freeze,
  as `WIP: move to <room>` (`--wip`, the `check` route). The branch then travels as a git bundle (section 5). The
  successor's first prompt says the commit is there, so it can `git reset --soft HEAD~1` to have its tree as it
  was. One transport, and it is the one that already handles binary files, renames, modes and line endings.
- **Atrium's own files in the cwd** go in the move bundle, through B's `safepath` and a name allowlist: `BRIEF.md`,
  `HANDOFF.*.md`, `REPORT.md`, and the files directly under `.atrium/incoming/` (dropped files, not subfolders).
  They are kept out of the wip commit (`moveGitArgs`), so they never land on the branch.
- **Ignored files are not carried.** `node_modules`, build output, `.venv`, a browser cache: they are rebuilt on B
  from the lockfiles the branch carries. A card that needs an ignored file it made by hand puts it in
  `.atrium/incoming/` or commits it.

Rejected: a tar of the whole cwd (carries ignored trees of hundreds of MB, and paths and line endings from the wrong
OS), and a diff (`git diff` loses binaries and untracked files unless both are special-cased, which a commit is not).

## 5. The branch

A's `gitpack` route answers a git bundle of the card's branch, from the merge base with `origin`'s default branch
when there is one, so it carries only the card's own commits. With no origin, or when B lacks the base, it is the
whole branch (`full`). B's `worktree` route finds or clones the repo the same way `POST /v1/new` does
(`repoCheckout`), fetches `origin`, fetches the bundle into `refs/heads/<branch>` (fast-forward only, so a stale
branch of the same name on B is never overwritten), and adds the worktree at B's own layout
(`<worktree root>/<repo>/<branch flattened>`). The answer is the path, and it becomes the successor's cwd.

When the hub has git sync (stage 1, built) the branch could ride that instead. The bundle is used because it works
for a repo the hub does not mirror and needs no hub at all, which a move with direct urls has to do.

## 6. OS differences

| Difference | What the move does |
| --- | --- |
| Paths (`G:\x` against `/home/x`) | Nothing in a move is a path from A used on B. B makes its own worktree path, and the successor is told the old path and the new one, so it reads its history right |
| The card root per room | B's provider root and worktree root, from B's own settings. Never A's |
| Transcript folder | Claude Code keys transcripts by the encoded cwd. B writes the jsonl under the encoding of B's new cwd (`api.ProjectDirFor`), which is what `--resume` there looks under |
| Line endings | git carries the tree, so `core.autocrlf` on B applies as on any checkout. The bundled atrium files are copied byte for byte. They are markdown and either ending reads |
| Case sensitivity | B's clone and worktree are made by git. Two files that differ only in case on Linux refuse on Windows and macOS at checkout, which surfaces as a `worktree` failure and an undo. Not worked around |
| Shells | Nothing in the move runs a shell. git is run directly. The successor's harness row on B picks its own shell |
| Executable bits | carried by git |
| A card not in a git worktree | refused before anything moves: B makes its own worktree from the branch, so a card with no branch has nothing for B to stand in |

## 7. The conversation and its resume

The bundle carries the jsonl of the card's last SessionStart (`ConversationOf`, not the stored `resume_id` that lags
after a `/clear`), and B writes it under its new cwd's encoded folder. The successor launches with `resume = <that
id>`. Claude Code reads the transcript by id from the folder of the cwd it is started in, so the folder is all it
needs. Paths inside the history still name A's tree. The first prompt maps them: the old path is now the new one.

`--fresh` carries no transcript and starts the successor from `HANDOFF.<alias>.md`. It is also the fallback when
the transcript is over the cap or B's harness is not the one that wrote it (a codex card moving to a room with no
codex, should that ever be allowed).

## 8. MCP config, hooks and permissions on the target

- **Hooks are B's, not A's.** The successor is a card on B, launched by B, so it gets B's hook wiring and B's
  `ATRIUM_*` env, as any launch there does. Nothing of A's hook config travels, and nothing should.
- **MCP servers.** atrium-control is B's own. Other servers named on the card's runner row come from B's row. A
  server A had that B lacks is a tool parity row (section 2), not something the move copies, because MCP config can
  hold secrets and machine paths.
- **Project settings in the tree** (`.claude/settings.json`, `.mcp.json`) travel with the branch, since they are
  committed. `settings.local.json` is ignored and does not.
- **Permissions.** The successor launches gated with nothing auto-approved, whatever A had. The `adopt` route sets
  the carried `gated` and `auto_approve` only at the cut-over, so a successor that misreads its history cannot run
  unattended before the move is done.
- **Env.** Names only, from B's room env. A per-card env value never leaves A (design row 7).

## 9. A hub that is down, or itself moving

- **The hub is down before the move:** the move refuses at the first request. With `--from-url` and `--to-url` the
  driver talks to both rooms directly and needs no hub, which is the same-machine case and any case where the
  operator can reach both rooms' boards. The pack of section 3 is the case where it cannot.
- **The hub goes down mid-prepare:** the driver stops at the failing request and undoes what it can reach. Whatever
  it cannot reach, A undoes on its own at the lease (`sweepFreezes`): unpark, unfreeze, replay the queue. A
  successor already launched on B is left running with `moved_from` set and no alias. `atrium move --undo <id>` ends
  it once the hub is back, or at once with `--from-url` and `--to-url`. The lease is `--lease` seconds (30 minutes by
  default) and is not renewed, so a move that takes longer must renew it (the `renew` route is there, the driver does
  not call it yet).
- **The hub goes down after the launch:** any step that gets no answer at all (no connection, or a 502, 503 or 504
  from the hub), whether waiting for the pid, sending the note or setting `moved_to`, stops and keeps the record. It
  does not undo, since it cannot see the rooms it would undo on. `--resume` with `--from-url` and `--to-url`
  finishes the move without the hub.
- **The hub goes down after `moved_to`:** the move only goes forward. The record says which steps are left, and
  `atrium move --resume <id>` finishes them (adopt, then queue, deliver and ack, then release). A say to the old
  card meanwhile is held by A's relay and forwarded when the hub is back.
- **The hub is itself moving** (sg4 hibernating and coming back as another machine): no card moves while the hub
  moves. The hub move is a separate thing (its store, its git mirror, its certs). Rooms reattach to the hub's new
  address and keep their cards. Cards move after, if at all.

## 10. Failure and rollback, step by step

| # | Step | Room | On failure | Rollback |
| --- | --- | --- | --- | --- |
| 0 | read the card, check B | A, B | refuse | nothing changed |
| 1 | freeze, lease 30 min | A | refuse | nothing changed |
| 2 | check: idle, clean (or `--wip`) | A | stop | undo on A (unfreeze, replay the queue). The undo, and the lease sweep, put back as staged changes the WIP commit this check made, by its sha, and only while it is still HEAD (`unwip`). The sha is kept in memory, so after a room restart the commit stays |
| 3 | capture the handoff | A | stop | undo on A. A half-written handoff file is left. It is only a file |
| 4 | gitpack | A | stop | undo on A |
| 5 | worktree on B | B | stop | undo on A. A worktree made on B is removed only if the route made it (it answers `created`), and the branch too if the route made it (`branch_created`), so a retry is not refused for a stale tip |
| 6 | bundle and receive | A, B | stop | as 5. Files written into B's new worktree go with it |
| 7 | park | A | stop | undo on A (unpark, unfreeze, replay) |
| 8 | launch on B with `resume` and `moved_from` | B | stop | exit the successor on B if it was made, then as 7 |
| 9 | `moved_to` on A | A | no answer: stop and keep the record, `--resume` later. After the lease it is refused | if refused, A undid itself: exit the successor, done |
| 10 | adopt: alias, pin, gate | B | retry | none |
| 11 | forward the queue, ack each | A, B | retry | none. Forward only. B dedups by message id |
| 12 | release: alias, done | A | retry | none |

Every step before 9 is undone newest first. From 9 on the move only goes forward and every step is idempotent, so a
retry is always safe. The local record holds the move id, the old and new ids, the last step that answered and the
B worktree, which is what an undo or a resume needs.

## 11. Stages

- **S1, built here.** `atrium move <card> --to <room>`, one card, through the hub or directly, with the branch, the
  wip commit, the atrium files, the transcript and the resume, the queue forwarded, and an undo before the cut-over.
  The new routes `gitpack` (A) and `worktree` (B).
- **S2.** The move record and the driver in the hub (M2): resumed or undone at hub start, the lease renewed,
  `resolvePeer` preferring the `moved_to` successor, the requirements preflight as a check row, and the successor's
  answer awaited before the cut-over.
- **S3.** `move export` and `move import`, the pack for a hub that is down.
- **S4.** Lists and the landing route (M3).

## 12. What the spike cut

- The driver is in the CLI, so the record is a local file (`~/.atrium/moves/<move id>.json` of whoever ran it) and
  not resumed at hub start. A move started from one machine is resumed or undone only from that machine.
- The lease is not renewed. A move longer than the lease self-undoes on A.
- The successor's answer is not awaited. The driver waits for B's card to have a pid, then cuts over.
- Checks are rows 3 (B attached) and 4 (B has the runner row) only. No toolchain, env, lean-kit, cap, landing or
  who-asks check.
- Children are not re-pointed with `SetLauncher`. The lookup that follows `moved_to` covers a child's report.
- No `resolvePeer` change on the hub, so a say by alias across rooms can still hit two matches until the old card
  releases its alias, which it does at the end of the move.
- A resumed conversation takes no launch prompt (`/v1/launch` refuses one), so the move note ("you were moved from A
  to B, your worktree is now X") is the successor's first delivery, through the `deliver` route with the message id
  `<move id>-note`, sent once the successor has a pid.
- Capture needs a live claude that answers the idle handoff. The rig's fake runner does not, so the rig runs with
  `--no-capture`. Capture is untested here.
- `--repo host/org/repo` is needed when A's origin is a local path, since `repoFromRemote` reads only a forge URL.
- `--stop-after <step>` (hidden) stops a move after a step, for tests of `--resume` and `--undo`. The successor's
  id is printed before a stop after `launched`.
- No credentials and no live processes (a dev server the card started) are carried. Out of scope for this spike.

## 13. The test on sgg: the rig

`scripts/move-rig.ps1` stands up a throwaway hub and three rooms on one machine, all from `build.claude/atrium.exe`
of this branch, none touching a live hub or room.

| Part | Where |
| --- | --- |
| hub board, link | `127.0.0.1:7960`, `127.0.0.1:7961` (`-Port` moves all of them) |
| room `ra`, `rb`, `rc` boards | `7972`, `7982`, `7992`. Agent ports one higher |
| data | `-Data`, by default `move-rig` beside the checkout. One folder per room: `state`, `home`, `src`, `wt` |
| origin | a local bare repo `origin/widgets.git`. Each room has its own clone under its own `src` |
| runner | `fake`: `fake-claude.ps1`. Writes a transcript under its HOME's `.claude/projects/<encoded cwd>`, posts SessionStart to its room, refuses a `-Resume` id it cannot find, as claude does |

Each room is `--isolated` with its own `ATRIUM_LOCATION` and its own HOME, so its own `~/.claude` and git identity.

```
go build -o build.claude/atrium.exe ./cmd/atrium
pwsh -File scripts/move-rig.ps1 -Start                 # hub and rooms ra, rb, rc
pwsh -File scripts/move-rig.ps1 -Seed ra -Card w1      # branch claude/w1, a commit, dirty.txt, BRIEF.md, alias w1
pwsh -File scripts/move-rig.ps1 -Status                # the hub's rooms, and every room's cards
pwsh -File scripts/move-rig.ps1 -KillHub               # the hub process only
pwsh -File scripts/move-rig.ps1 -StartHub [-Readopt]   # the hub again on its state. -Readopt: section 15
pwsh -File scripts/move-rig.ps1 -WipeHub               # the hub killed and hub.db deleted. CA and certs stay
pwsh -File scripts/move-rig.ps1 -Stop                  # everything, and the data folder deleted
```

Pass the same `-Port` (and `-Data`, if set) on every call, not only `-Start`. The default is 7960.

The driver runs through `-Cli`, its arguments in one string. The rig runs `<data>/bin/atrium-cli.exe`, a copy
`-Start` makes, with every `ATRIUM_*` variable cleared and HOME set to `<data>/cli-home`. So the move records
(`<data>/cli-home/.atrium/moves/<move id>.json`) stay in the rig, and the CLI never talks to the room it was started
in:

```
pwsh -File scripts/move-rig.ps1 -Cli 'move w1 --from ra --to rb --url http://127.0.0.1:7960 --repo github.com/acme/widgets --no-capture --wip'
pwsh -File scripts/move-rig.ps1 -Cli 'move --undo <move id> --url http://127.0.0.1:7960'
pwsh -File scripts/move-rig.ps1 -Cli 'move --resume <move id> --from-url http://127.0.0.1:7972 --to-url http://127.0.0.1:7982'
```

`--repo` is needed because the rig's origin is a local path (section 12), and `--no-capture` because the fake runner
cannot write a handoff. Direct, without the hub: `--from-url` and `--to-url` in place of `--url`.

The rest of the tests go to the rooms' and the hub's HTTP APIs on loopback:

| What | How |
| --- | --- |
| a say | `POST http://127.0.0.1:<sending room>/v1/say` with `{"from": "w2", "to": "w1", "text": "..."}`. A moved card is reached on its old room by its old id or its alias, and the say is forwarded |
| what a room said and got | `GET /v1/says` on that room. A held say shows its `note` on the sending room only |
| a room's cards | `GET /v1/tasks` on the room, or on the hub board for every room |
| a backlog item, hub only | `POST http://127.0.0.1:7960/_hub/backlog` with `{"dept": "rnd", "title": "..."}`, read back with `GET /_hub/backlog` |
| the lease | the sweep runs on the room's reaper tick (`ReapEvery`, 20 s), so a 20 s lease undoes 20 to 40 s after the freeze |

## 14. The test on sgg: what worked and what broke

All runs on sgg, 2026-11-04, on the rig of section 13.

| # | Test | Result |
| --- | --- | --- |
| 1 | `w1` ra to rb through the hub, first try | Failed at the launch: 400 "a resumed conversation already has its instruction". The undo was clean: the successor ended, B's worktree removed, A resumed its own conversation with a new pid. Fixed by sending the move note as a delivery (section 12) |
| 2 | the same move again | Done. On rb: the transcript under rb's encoded cwd with A's history plus the resumed line, the branch with `WIP: move to rb` holding `dirty.txt`, `BRIEF.md` copied and untracked. ra's card `done` with `moved_to`, rb's card has the alias and the note |
| 3 | hub killed, cards running | Every room answers `/v1/tasks` and `/v1/say` itself. A say inside a room is delivered. A say to a card on another room is `held` by the sending room |
| 4 | `w2` ra to rc direct, the hub dead | Done, with no hub at all |
| 5 | a say to the moved `w1` while the hub was dead, then the hub back | Held on ra with `forwarded: moved to rb~...`, sent when the hub came back (ra's say log: `handed`, `sent after being held`), and delivered on rb |
| 6 | `--stop-after launched`, a say to the frozen card, then `--resume` twice | The say was queued, the resume forwarded it once, the second resume answered `done` |
| 7 | `--stop-after parked --lease 20` through the hub, then the hub killed | ra undid the move on its own at the lease (`the move's lease ran out, the card undid it`) and resumed its conversation. `--undo` with direct urls then removed rb's worktree |
| 8 | the same card moved again after 7 | Broke: rb kept `claude/w4` at the old WIP commit, the new bundle was not a fast-forward, and the move undid itself. ra also kept the `WIP: move to rb` commit after the undo. Fixed: an undo deletes a branch the move made on B, and puts the WIP commit back on A (also at the lease). Rerun on a fresh rig: clean on both sides |
| 9 | `--stop-after launched` through the hub, hub killed, `--resume` through the dead hub | Broke: it waited a minute for a successor it could not see, then tried to undo through the dead hub and failed. Fixed: no answer stops the resume at once and keeps the record. Rerun: stopped in 0.3 s, and `--resume` with direct urls finished it |
| 10 | hub store wiped (`hub.db` deleted, CA and certs kept), hub restarted | Every room refused: "this hub has no room called ra". Each room kept answering on its own and kept redialling |
| 11 | the same, hub restarted with `-Readopt` | Each room reattached within one redial (5 s). The board listed every card again, with `moved_to` on the old ones, from the rooms alone. A backlog item filed before the wipe was gone |
| 12 | after 11, `w3` rb to rc through the rebuilt hub, and a say to the old `w3` | Done. The say was forwarded to rc and delivered |

A reviewer card with no atrium background rebuilt the rig from this note on its own ports, reran tests 2 to 12 and
confirmed them, and found these. Each is fixed in the code or in this note, and the code fixes were rerun on the rig.

| # | Finding | Fix |
| --- | --- | --- |
| 13 | An undo reset any HEAD whose subject was `WIP: move to`, so it stripped the WIP commit of an earlier move | The check keeps the sha it made, and only that sha is reset. Rerun: a card on rb with an older `WIP: move to rb`, moved to rc and undone, keeps that commit |
| 14 | `REPORT.md` and `.atrium/incoming/` went into the WIP commit, against section 4 | Kept out of the commit, carried in the move bundle, and allowed by B's receive. Rerun: the WIP commit holds `dirty.txt` only, and both files land on rb untracked |
| 15 | The 64 MiB cap was checked on raw bytes, while B read at most 65 MiB of base64 JSON, so a bundle over about 48 MiB failed at B as a JSON parse error | B reads up to 4/3 of the cap and answers 413 above it (section 3) |
| 16 | Past the pid, a dead hub during the note or `moved_to` started an undo through the same dead hub | Every step maps no answer to "stop and keep the record" (section 9) |
| 17 | A card parked by a move had its `why` overwritten with "failed to start: ..." (the runner was young when the park ended it), and a successor copied it | The park notes a cause on the runner, and an exit atrium caused never writes "failed to start". Rerun: `why` kept |
| 18 | A card not in a git worktree would launch on B in A's path | Refused before anything moves (section 6) |
| 19 | Sections 2, 5, 9 and 10 did not match the code (the git probe, the rig's `git: true`, the cut-over order) | The note now says what the code does |
| 20 | `Readopt` let a forced-out room back on a hub that was not wiped | Refused while the audit names the room removed or forced out (section 15) |
| 21 | The rig instructions were not enough: no `atrium-cli.exe`, a POSIX command line, `-Port` and the say, backlog and lease details missing | `-Start` makes the copy, `-Cli` runs it, and section 13 has the rest |
| 22 | `--resume` ignored `--wait`. `-Status` showed blank room states | Both fixed |

Gaps seen and not fixed:

- A say to a move-frozen card answers "this card is starting a new context", the new-context hold's wording, with
  `reachable: no`. It should say the card is moving and the say is queued.
- The old card, `done`, keeps a stale pid on its row.
- A say sent during the freeze stays `queued` in A's say log after it was forwarded and delivered on B. The log is
  not told about the forward.
- The undo puts the WIP commit back staged (`git reset --soft`). A file that was untracked before comes back as an
  added file, not as untracked. Harmless, and not quite "as it was".
- The successor's `resume_id` on B can differ from A's once the runner writes its own SessionStart. The transcript
  is the same.
- The move record lives with the CLI, not on the hub or in the rooms (section 15 says where it should).

## 15. The hub as a pane of glass

The rule asked for: each room owns its data and writes an append-only ledger of its own changes, and the hub holds
nothing that is not rebuilt by replaying the rooms. Below is what today lives only on the hub (`internal/hubstore`
tables and the files beside `hub.db`), and where each should live.

| Hub state today | Where it is | Who should own it |
| --- | --- | --- |
| The rooms registry (`room`), join secrets (`room_secret`) | `hub.db` | The registry is rebuilt from rooms dialling in with a certificate the hub's CA signed (built here as `Readopt`, test 11). A join secret is used once and not needed after the join, so nothing is lost |
| The CA key, the hub's certificate | `ca.key`, `ca.crt`, `hub.key`, `hub.crt` | The hub's identity, and the one thing that cannot be rebuilt from rooms. Kept beside the store and backed up apart from it, or escrowed with the operator. With a new CA every room must rejoin |
| Card cache (`room_card`) | `hub.db` | Already a cache. Rebuilt from each room's announcement on attach (test 11) |
| Room audit (`room_audit`) | `hub.db` | Each room's own log of its attaches and refusals. What only the hub sees (a refused stranger) stays a hub log, not state |
| Backlog (`backlog_item`), its gates and renames (`item_gate`, `item_rename`) | `hub.db` | Lost on a wipe (test 11). Owner: the room of the card that filed or changed the item, as ledger entries. The hub is the merge of every room's entries, ordered by entry id. An item filed by the operator from the hub board is written to the room the operator names as the backlog's home |
| Items and work (`item`, `item_event`, `work`, `work_card`, `work_step`, `work_fact`, `work_task`, `work_event`, `work_outcome`, `shepherd`) | `hub.db` | `item_event` and `work_fact` are append-only already and are pushed by the room that runs the item. Each row's owner is that room. Rebuild is a replay of each room's facts, so a room keeps its own copy before pushing |
| Reports (`director_report`) | `hub.db` | The reporting card's room |
| Usage and prices (`price`, `work_usage`, `work_usage_cursor`) | `hub.db` | Usage: the room that ran the card. Prices: a setting, see hub settings below |
| Says and relays between rooms | the sending room holds a held say. The hub holds none | Already right (test 5) |
| Git meeting point (`<hub dir>/git/*.git`, `git_push`, `change_request`, `pr_claim`, `merge_request`, `merge_land`, `flow_*`) | the hub's bare repos and `hub.db` | The bare repos are a mirror: every branch on them was pushed from a room that still has it, so a wiped mirror is refilled by each room pushing its branches again. Claims and the merge queue are ledger entries of the room whose card asked. `flow_check` and `flow_deploy` belong to the room that ran the check or the deploy |
| Hub file drop (`hub_files`, `<hub dir>/files/<sha256>`) | the hub | A transit store. The sending room keeps the file until the receiving room acks it, and the hub copy is a cache |
| Docs (`doc`, `doc_version`) | `hub.db` | The writing card's room, as ledger entries |
| Hub settings (`hub_setting`), room specs (`room_spec`, `room_lock`), room ssh logins (`room_ssh`) | `hub.db` | Settings: a ledger kept by the operator's room, or replicated to every room so any of them can rebuild a hub. `room_lock` and `room_ssh` are published by each room and come back on attach. `room_spec` is desired state the operator wrote, so it belongs to the operator's room |
| Notifications and growls (`notify_sent`, `growl`), push subscriptions (`push_subscription`), events (`hub_event`, `event_sub`, `event_delivery`), CI runs (`ci_run`, `ci_delivery`, `ci-artifacts`) | `hub.db` and the hub folder | Derived or delivery bookkeeping, not truth. After a wipe a notice is resent at most once. Push subscriptions are the browser's: lost, a browser subscribes again |
| The move record | the CLI's HOME | Both rooms of the move, as ledger entries on each: A writes freeze, park, moved, release. B writes worktree, receive, launch, adopt. A move is resumed from the two rooms' ledgers, not from wherever the driver ran |

What the prototype shows (tests 10 to 12): with the CA kept, a hub with an empty store is rebuilt by the rooms for
everything a room already announces: the rooms, their cards, `moved_to` and `moved_from`, and the routing of says
and moves. Everything in the table that no room holds yet is lost, the backlog first.

What wrongly needs the hub today, from the runs: a move through `--url` (the direct urls are the workaround), a say
to a card on another room (held, not lost), and the backlog and reports, which no room holds. Nothing a room does
for its own cards needed the hub.

`Readopt` is behind `ATRIUM_HUB_READOPT=1` for the spike. It trusts a certificate the hub's own CA signed and makes
a row for its name, unless the hub's audit says that name was removed or forced out. The audit is in `hub.db`, so a
wipe loses it, and a room forced out before the wipe comes back. The real version needs the revoked names kept with
the CA, outside the store. A readopted row says `direct` whatever transport the room came in on, since the store
has no other record of it. A row from `atrium rooms add` would have named the transport.

`-WipeHub` also deletes `hub/backups`. A real store loss may leave a backup, and restoring it is the first thing to
try. The rebuild from rooms is for what a backup does not have: anything since it was taken, and a hub with none.

The ledger itself is not built. The smallest real step: each room appends every mutation its store makes to
`<state>/ledger.jsonl` (id, at, kind, subject, payload), and on attach the hub asks each room for the entries after
the last id it holds for that room. Rooms already write a projection, `work-ledger.md`, that nothing reads back. The
jsonl is the source it should be projected from.
