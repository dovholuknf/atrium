# Two rooms on one machine, and moving a card to another machine (f-004)

Status: design, ACCEPTED by @rnd on 2026-09-30 with one required change (folded in). Written by @fabric. Not built.
Stage 1 waits for f-019. The item is `docs/backlog/fabric/f-004.md`.

## 0. The answer, in one paragraph

f-004 asked for two things under one name. The first, "bring a new room up beside the old one and migrate, so a
deploy stops nobody", is ANSWERED by f-011 design B (`docs/rnd/rolling-restart-design.md`), decided through
@orchestrator on 2026-09-30: the pty host keeps every runner alive while the daemon restarts alone, and no second room
ever exists. f-004 therefore drops its same-machine migration. What B does not cover is the second question, "what if
I wanted to move to sg3": carrying one card, with its worktree and its conversation, from one machine's room to
another's. That is what this design is about. The branch travels through the hub by f-019's git sync, with one
narrow new op. The transcript is a file copy over ssh in stage 1, the one ssh step left. The runner is an exit on A
and a resume on B.

Accepted by @rnd on 2026-09-30, with the one required change folded in: the branch does NOT travel by `room-git.ps1`
from this side. That is the agent-run git transport decision 19 retires. It goes through f-019 (section 3, step 2).

## 1. What f-004's four questions become

1. **How a card moves between rooms on one machine.** It does not. f-011 B makes it unnecessary, and f-011 section 8
   records why two rooms on one database, or rows copied between two, is where a card gets lost or duplicated.
2. **How hooks find the right room while two are up.** Not a case under B. A throwaway second room for testing is
   still started `--isolated` (`internal/cli/roomrun.go` `roomLocationEnv`), which already keeps the hook pointer.
3. **Whether it replaces the room restart for deploys.** B does, at f-011 stage 2. The board change is f-011's.
4. **Moving to another machine.** Sections 2 to 5 here.

## 2. What travels, and what cannot

| Part | Where it lives | How it moves |
| --- | --- | --- |
| The work (commits) | the card's worktree and branch on machine A | f-019: `atrium_git_collect` from A into the hub, then a new hub op carries that one branch to B, where a worktree is made on it |
| Uncommitted changes | A's worktree | not carried. The move requires a clean tree or a WIP commit first, the same rule as a cull |
| The conversation | `~/.claude/projects/<cwd slug>/<session id>.jsonl` on A | copied to B under B's slug for the new cwd, then `claude --resume <session id>` there. Over ssh in stage 1, through the hub later |
| The card (title, alias, tags, launcher, brief) | A's store | a NEW card on B, launched with the old card's fields, and the old card marked moved |
| The pty and the process | A | cannot move. The runner exits on A and a new one resumes on B, so the move waits for idle |
| Queued messages, held reports | A's store | A refuses new ones from the moment of the move, and delivers what is queued before the exit (section 4) |

The transcript slug is the cwd with separators replaced, so a path that differs between machines (`D:/worktrees/...`
against `~/git/...`) gets a different slug. The copy writes under B's slug, and the session id inside stays the same.
Paths INSIDE the transcript still name A's directory. Claude reads them as history, not as a working directory, so
that is left alone and the resume prompt says where the card now is.

## 3. The move, step by step, all driven from this side

A script, `scripts/move-card.ps1 <card> <to-room>`, run on this side by an operator or a director. It drives the hub
and the two rooms and runs no git itself. The steps:

1. **Check.** The card is idle by r-007's rule (`idleParkEligible`), its tree is clean, and B is a room f-019 syncs.
   Otherwise it stops and says which one failed. `-Wait` polls until the card goes idle.
2. **Carry the branch, through f-019.** `atrium_git_collect` on A brings the card's branch into the hub as
   `refs/rooms/A/claude/<branch>`. B then needs that ONE branch, and f-019 on purpose gives a room only claude/main.
   So this adds one narrow hub op: carry `claude/<branch>` from room A to room B for a move. B fetches it into
   `refs/heads/claude/<branch>` (the card's own branch, never `hub-main`) and makes a worktree on it. The op takes a
   named source room and branch, and is allowed only for a card being moved. It is a small f-019 extension, so stage 1
   waits for f-019 to land.
3. **Stop the card on A.** `atrium_exit` on A, which delivers what is queued first (section 4).
4. **Copy the transcript. The one ssh step left.** Read `<session id>.jsonl` off A over ssh and write it on B under
   B's slug. A later stage moves it through the hub too. The session id is the one from the card's LAST
   SessionStart, the conversation it is actually in, and not the stored `resume_id`, which lags after a `/clear`
   (item 62).
5. **Launch on B.** `/v1/launch` with `X-Atrium-Room: B`, the old card's title, alias, tags, theme, model, effort and
   launcher, cwd the new worktree, and `resume` set to the session id. The launch request already has a `resume` field
   (`internal/daemon/launch.go`). If the hub's launch path does not pass it through to the room, that is the one
   binary change, and it is small.
6. **Mark the old card.** A's card goes to done with a recap line `moved to B~<new id>`. It is not deleted, so its
   history and audit stay where they happened. The new card links to the old one and does not copy its history.
7. **Tell the launcher.** One `atrium_say` to the card's launcher: the new handle and room.

## 4. The hazards

1. **Messages sent in the gap.** A message to the old card after step 3 has nowhere to go. A answers
   `undeliverable: moved to B~<id>` for a card marked moved, so the sender learns the new address instead of the
   message dying quietly. That note is a small room change (it reads the recap line, or a new `moved_to` field if
   @runtime prefers a column, which would be its migration).
2. **Handles.** The alias moves with the card, because the old card is done and an alias is unique only among live
   cards. `name@A` and `A~id` do not follow. The hub would have to route per card for that, which is f-011 section 8.3's
   hub work, and this design declines it: a move is rare and deliberate, and the launcher is told.
3. **Two live copies.** Step 5 runs only after step 3 has confirmed the old runner is gone, so there is never a moment
   with the same conversation running on two machines.
4. **Credentials.** Nothing new travels. The transcript is a file the account on both machines already reads and
   writes, and B's claude signs in with its own login. The move never carries a token.
5. **Windows on either side.** The stage 1 transcript read and write go through the same `Invoke-Remote` shapes
   provision-room.ps1 uses (`sh -s` on Unix, `powershell -EncodedCommand` on Windows), so no pwsh is needed on B.
6. **The branch op is not a general room-to-room git channel.** It names one source room, one branch and one card
   being moved, and refuses otherwise. f-019's rule that a room gets only claude/main holds for everything else.

## 5. What is out of scope

- Moving a whole room. That is many single moves, and the script takes a list.
- Moving a busy card. It waits, or the operator moves it when it goes idle.
- Keeping `A~id` working. Declined above.

## 6. Staged plan

- **Stage 1 (@fabric, after f-019 lands).** The f-019 move op (section 3, step 2), `move-card.ps1`, and the ssh
  transcript copy, proven sg4 to m1mini with a throwaway worker that commits, moves, commits again, and reports from
  B. It also checks whether launch's `resume` field reaches the room through the hub.
- **Stage 2 (@runtime, only if wanted).** The `moved to` answer for a message sent to a moved card, and a `moved_to`
  column only if the recap line is not enough.
- **Stage 3 (@fabric).** The transcript moves through the hub, so the move needs no ssh.

## 7. Decided by @rnd, 2026-09-30

1. Dropping the same-machine migration for f-011 B is right. A room on a new state dir is a restore or a
   re-provision, not a live migration, and needs no second room.
2. The recap line for now. A `moved_to` column only if stage 2 needs it.
3. The new card links to the old one and does not copy its history.
4. REQUIRED, folded in above: the branch travels through f-019, never `room-git.ps1` from this side. The session id
   comes from the last SessionStart, not `resume_id`.
