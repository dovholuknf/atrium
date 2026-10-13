# Close is a workflow (rnd-005)

Status: design by r-005-close-workflow, 2026-10-13. Nothing here is built. r-004 (learnings on close) is held and
folds in here as one fixed step. "Open choices" at the end lists what only clint can settle, ranked, each written so
an interviewer can ask it alone.

clint, 2026-10-13: "on closing a card I want a little workflow ... the human wants it closed but atrium's API tells
the agent 'do some stuff' on close. That stuff is predefined, you can't change it, atrium just does it for you. You
are closing down. If there is anything in flight stop it immediately. Terminate any processes you have started. Clean
up any resources. Scan for files we didn't commit. Scan for files we'd lose. ... If you cannot do this reply back with
atrium hold to stop the card closing. At which time a user growler is popped, the card is resurfaced and the user has
to decide if they care or not. THEN I want the user to be able to customize additional messages to happen on close in
the atrium settings and possibly on each room."

Read for this design: `internal/api/close.go` and `internal/api/closeprogress.go` (close today),
`internal/daemon/finish.go` (the verb a hold copies), `internal/daemon/actions.go` and `internal/store/actions.go` (a
stored prompt said to a card, the shape custom steps copy), `internal/daemon/messages.go` (delivery and turn end),
`internal/link/growl.go` and `internal/hubstore/growl.go` (growlers), `internal/link/interview.go` (rules built into
the binary), the backlog items rnd-005, r-004 and u-005, branch `claude/u-005-link-opens-card` (in flight), the brief of
branch `claude/hub-branch-inventory` (in flight, no commits yet), `docs/rnd/gwt-parity.md` and gwt itself
(`git-worktree.ps1`: `Remove-Worktree` 828, `close` 1928, `prune -Recapped` 5440, `stamp-recap.ps1`).

## 0. The design in one paragraph

Pressing close still opens the one confirmation u-005 asks for, listing what goes and asking about work that is
nowhere else. Confirming no longer frees anything at once. It **arms** a close: a durable row on the room that says the
card is wrapping up, with the answers the human gave. Atrium then says a **fixed wrap-up prompt** to the card's session,
built into the binary like `interviewRules`, followed by any **custom close steps** the board and the room add. The
session works through them and either ends its turn (the close goes on) or runs **`atrium hold <reason>`** (the close
stops, the card goes to needs-input, a growler pops, and the human picks close anyway, keep it open, or tell the agent
something). A wrap-up that never answers, a card with no live session and a session mid-turn each have a rule. When the
wrap-up is over the room reads the inventory again, asks again only if the wrap-up made things worse, and runs the
close it runs today. The card shows each phase on the board and the phone.

## 1. What is settled before this design starts

These are decided elsewhere and not reopened.

### 1.1 Close today (internal/api/close.go)

- Two calls: `GET /v1/tasks/{id}/close` previews, `POST` with `confirm` and `answers` closes
  (internal/api/close.go:30). A close **never refuses and never decides alone**: a worktree with uncommitted files or
  commits on no remote is an ask, answered keep, stash or delete per row seq (:36-41, :204-220).
- The order is fixed: the runner, its processes, ports, overlays, the review, worktrees, branches, refs, scratch
  folders, the card (:46-49, :397-595). The session is killed as step 1 when `cardRunning` (:225, :398).
- The preview measures and asks from disk at the time it is read (:235-316). `worktreeWork` counts dirty files and
  commits on no remote (:354-372).
- A second press while a close runs answers 409 `closing` (:189-193), held in memory in `closing` (:144).
- The card ends in done with `atrium:closed` and a `notified` event with `by: card-closed` (:612-631).
- Each step is broadcast as a `close-progress` event when the board sent a progress id (:110,
  internal/api/closeprogress.go:10-35).
- `reclaimInventory` is a close without questions for a reclaimed worker: every ask is answered keep (:641-665).

### 1.2 u-005 (branch claude/u-005-link-opens-card, decision 4 in its brief)

- **Every card cleans up when closed**: its worktree, the clone it made, and its `claude/*` branches here and on the
  hub. The "throw it away" and "clean up when done" options are gone.
- **Close confirms first**: "Closing deletes X, Y, Z" plus a warning for unpushed work. Built on that branch in
  `closeCardNow` (internal/api/web/js/sharing.js:809-835 there) with `CLOSE_KIND_WORDS`, which already names a
  `hubbranch` kind so the hub branch row lists without a board change.
- Hub branch rule, from the launcher's brief to this item: a hub branch that is an ancestor of `claude/main` is
  deleted. An unmerged one is an ask in the close preview, **keep or delete** (no stash, since it is already on the
  hub).

### 1.3 Hub branch inventory (branch claude/hub-branch-inventory, in flight)

A `claude/*` branch a card pushes to the hub becomes an inventory row on that card. The hub deletes it only when its
push log shows that card pushed it, through its own API, never through `git push --delete` (receive.denyDeletes stays
on). A failed hub delete is reported and does not block the rest of the close.

### 1.4 gwt

- gwt's `prune -Recapped` (git-worktree.ps1:5440) is the same idea in a cruder form: a session writes its recap
  (`/recap`, `stamp-recap.ps1`), the ledger marks it `+r`, and only then is its folder eligible to go. Capture first,
  free second. This design keeps that order.
- `Remove-Worktree` refuses a folder with a live gwt session (guard #1, :849-856) and asks atrium to release its own
  card first with `POST /v1/cards/archive {cwd, wait}`, waiting at most 30 seconds and deleting anyway
  (claude-shell.ps1:505-510).
- `gwt close` (:1928) only ends the tab and marks the ledger. It frees nothing.

### 1.5 r-004 (held)

A wrap-up turn on close asking for learnings that help the next agent on the repo, not a recap. "Nothing worth
keeping" is a valid answer. Bounded at 5 minutes. A card with no session skips it. Where the learnings go is reopened
in Open choice 1, because r-004's answer collides with the hub branch rule in 1.2.

## 2. The sequence

```
 human presses close
   │  GET /close            preview, as today, plus the wrap-up line (section 7)
   ▼
 confirmation dialog        answers per ask (keep, stash, delete, and keep or delete for an unmerged hub branch)
   │  POST /close {confirm, answers, progress}
   ▼
 ARMED  ── card_close row written, answers and the counts each was given against
   │
   ├─ no live session, shelved, or reclaim ─────────────────────────────┐
   ▼                                                                    │
 WRAPPING UP  fixed prompt + custom steps said to the session           │
   │  turn ends, no hold            atrium hold <reason>                │
   │  deadline passes               (or a question, or atrium_blocked)  │
   ▼                                         ▼                          │
 RECHECK  preview read again          HELD  needs-input, growler,       │
   │  an answered ask grew?                 close anyway / keep / tell  │
   │  yes: HELD with "the wrap-up                                       │
   │       left work behind"                                            │
   ▼                                                                    │
 FREEING  closeCard as today (internal/api/close.go:392) ◄──────────────┘
   ▼
 done, atrium:closed, card_close row ended
```

The wrap-up goes **after** the confirmation and **before** step 1 of `closeCard`. Before the confirmation would
make the human wait minutes for a dialog they asked for, and break u-005's "close confirms first". After step 1 there
is no session left to talk to.

### 2.1 Why the preview is read again

The wrap-up changes the disk: the agent may commit, push to the hub (a new hub branch row), or write a file. The
answers the human gave were against the counts they saw. So after the wrap-up the room reads the preview again
(`closePreview`, :235) and compares each ask with the counts stored when the close was armed:

- An ask that is gone (the agent pushed) is freed with no question, as any clean worktree is today (:269).
- An ask whose dirty or unpushed count did not grow keeps its answer.
- An ask whose count grew, or a new ask (a worktree that was clean and is not now, an unmerged hub branch that did not
  exist), is **unanswered**. The close goes to HELD with the reason "the wrap-up left work nowhere else has" and the
  dialog asks again for those rows only. This is close.go's own rule: it never decides alone (:36). A "delete" given
  against 2 files is not a "delete" for 40.

### 2.2 Durable, not in memory

`closing` (:144) is a `sync.Map`, which is right for a request that lasts seconds. A wrap-up lasts minutes and must
survive a room restart, so the armed close is a row in the room store (AGENTS.md, "State lives on disk"). One new
migration, added at the end of the slice and written to tolerate being there:

```sql
CREATE TABLE IF NOT EXISTS card_close (
  card        TEXT PRIMARY KEY,
  phase       TEXT NOT NULL CHECK (phase IN ('wrapping','held','freeing','ended')),
  answers     TEXT NOT NULL,      -- {seq: answer}, as closeRequest.Answers
  seen        TEXT NOT NULL,      -- {seq: {dirty, unpushed}} the answers were given against
  steps       TEXT NOT NULL,      -- the custom steps said, by id and label, so the board can list them
  progress    TEXT NOT NULL DEFAULT '',
  armed_at    TEXT NOT NULL,
  said_at     TEXT,               -- when the prompt was typed or delivered, NULL while queued
  deadline    TEXT,
  hold_reason TEXT NOT NULL DEFAULT '',
  held_by     TEXT NOT NULL DEFAULT '',   -- agent, deadline, recheck, question
  ended_at    TEXT
);
```

On start the room reads every row not ended. A `wrapping` row whose session is gone takes the no-session rule (5.2).
A `freeing` row runs `closeCard` again, which is safe: it frees only live rows (:421-428) and a freed row says so.
`closing` stays as the guard against two presses in the same second.

### 2.3 What the HTTP calls answer

`POST /close {confirm: true}` answers at once with `{phase: "wrapping", deadline}` (202) when a wrap-up starts, and
with today's `closeResult` (200) when none does. The work goes on in the room. Three callers wait today:

- the board keeps the dialog up and follows `close-progress`, which it already does (sharing.js `onCloseProgress`).
- `atrium close --yes` (internal/cli/close.go:156) prints each phase and waits until the row ends or holds, bounded by
  the deadline plus `closeGitWait` steps. `--no-wait` returns at the 202.
- `atrium_close` (internal/link/control_mcp.go:390) returns the 202 as it is. Its 5 minute `closeTimeout` (:2031) is
  for the freeing, and an MCP call must not sit for a whole wrap-up.

`GET /close` on a card with a row answers the preview plus `close: {phase, deadline, hold_reason, steps}`, so a board
opened in the middle shows where it is.

## 3. The fixed wrap-up prompt

Built into the binary as a constant in the room (`internal/daemon/closewrap.go`, `wrapUpRules`), for the reason
`interviewRules` gives (internal/link/interview.go:15-17): the rules are not in anyone's memory and are not editable.
No setting reaches it. Custom steps (section 6) are appended after it, never spliced in.

The text, as it would ship. Step 5 follows the recommendation of Open choice 1 and step 4 that of Open choice 3. Both
change if clint answers otherwise.

```
# atrium is closing this card

The human closed this card. You are closing down. These steps come from atrium and are not up for editing.

1. STOP WHAT IS IN FLIGHT. Stop whatever you are doing now. Start no new work, and do not finish the task you were on.
2. END WHAT YOU STARTED. Terminate every process you started in this session: servers, watchers, tunnels, builds and
   background jobs. Say which ones you ended.
3. FREE WHAT YOU MADE. Remove what you created outside this card's folder for this work: containers, temporary
   folders, test databases, ports you reserved by hand. Leave the worktree, its branches and anything atrium made:
   atrium removes those.
4. LIST WHAT WOULD BE LOST. Run git status and list every file not committed. List every commit that is on no remote.
   Do not commit, stash or push them. The human already chose what happens to each in atrium's close.
5. WRITE WHAT YOU LEARNED. If you learned something that would help the NEXT agent on this repository (a build or
   test command that worked, a trap, where something lives, a convention reviewers enforce), write it to
   .atrium/LEARNED.md in this folder, at most 30 lines, wrapped at 120. Nothing about this card's own task. If there is
   nothing worth keeping, write nothing. Atrium collects the file before the folder goes.
6. ANSWER. When every step is done, end your turn with one line saying so. If you cannot finish a step, or finishing
   it would lose something the human has not seen, run `atrium hold <reason>` and end your turn. A hold stops the
   close and shows your reason to the human. Never hold only to keep working on the task.
```

It is said as **one prompt, one turn**: the fixed steps, then a heading `## Steps this board adds` and the custom steps
numbered on from 7. One turn is one answer to wait for and one deadline. Per-step progress comes from what the board
already sees during a turn (the activity badge and tool names), not from the agent reporting each step.

## 4. How the agent answers

### 4.1 Done is the turn ending without a hold

clint's words define only the hold: "if you cannot do this reply back with atrium hold". Silence about holding means
go on. So the done signal is the turn ending (`turnEndedBecause`, internal/daemon/messages.go:431) while the card has a
`wrapping` row and no hold was recorded. No new verb is needed for the common case, and a runner without a tool
surface still says it.

A turn that ends on a **question** (the Notification hook with `asked`, internal/daemon/activity.go:893-906, or
`atrium finish --status question`) is a hold with the question as its reason: the agent cannot go on without the
human. A worker's `atrium_blocked` during a wrap-up is a hold too. A worker's `atrium_done` is a done.

A turn that ends with the session's subagents still out (`turnPaused`, messages.go:469) is not an end. The wrap-up
waits for the Stop after the last one, as the card's column already does.

### 4.2 `atrium hold <reason>`

A command, for the reason `atrium finish` is one (internal/daemon/finish.go:29-35): every runner can run a command.
`atrium hold "<reason>"` posts to `/hold` on the agent listener, beside `/finish` (internal/daemon/daemon.go:1161), with
the same identity rules (`taskFor`, finish.go:568). A control MCP tool `atrium_hold` lands in the same function, as
`atrium_report` lands in `finish`.

It follows the hook posture (AGENTS.md resilience 7). An agent atrium never heard of gets `ok` and nothing recorded. A
hold on a card with no wrapping close answers `ok`, `recorded: false`, and the note "nothing is closing this card", so
an agent that holds by mistake does not fail. A reason is required (the same "says what to add" refusal finish uses,
finish.go:152-155) and bounded like a recap.

### 4.3 What a hold does

1. The `card_close` row goes to `held` with the reason and `held_by`.
2. The card moves to `needs-input` with waiting reason `close-held`, and a `notified` event with `by: close-held` and
   the reason. It does **not** go to done: `growlEnded` drops every growler of a done card (internal/link/growl.go:267).
3. The hub raises a growler with the new reason `close-hold`, titled "<name> held its close" with the reason as the
   body. This needs the hub's growl table's CHECK to take `close-hold` (internal/hubstore/schema.go:242), which is a
   table rebuild like `0010` and `0014`, and a branch in `derive` (growl.go:316) that does **not** skip agent-launched
   cards: the close was the human's, so the hold is the human's whatever launched the card. It joins the ladder default
   with permission, halt and deploy-hold (growl.go:82).
4. The session is left running. Nothing is killed, nothing is freed.
5. The card shows three buttons in place of its usual strip:
   - **close anyway**: the close goes on to RECHECK and FREEING without asking the agent again.
   - **keep it open**: the row ends with `ended_at` and no free. The card stays on the board where it is.
   - **tell the agent**: a message box. What happens to the close after it is Open choice 6.

Answering the hold any of these ways ends the growler, since its reason ends (hubstore/growl.go:16-19).

## 5. Timeouts

| Case | Rule | Why |
| --- | --- | --- |
| The agent never answers | A deadline of 5 minutes from `said_at`, a room setting `close_wrapup_minutes` (0 means no wrap-up at all). What happens when it passes is Open choice 2. | r-004's bound. Counting from delivery, not from the press, so a prompt still queued does not eat its own time. |
| The prompt cannot be delivered | Typed into a terminal atrium owns, or queued. A queued prompt with no delivery in 60 seconds, or a session that answers `undeliverable`, is treated as no live session. | `atrium_say`'s own words: queued reaches a session at its next tool call, which an idle external session never makes. |
| No live session | Skip the wrap-up, straight to FREEING, with "no session to wrap up" in the progress list and the event. A **parked** card (transcript kept, no process) is Open choice 4. | r-004: a card with no session skips it. There is nobody to ask, and the inventory already stops processes and frees what atrium recorded. |
| Mid-turn | The prompt is typed as soon as the input line is empty, mid-turn, as `atrium_say` does by default, never `when: done`. | clint: "if there is anything in flight stop it immediately". Waiting for the turn to end would let it finish the thing it was told to stop. |
| A dialog is open, or a permission is pending | The prompt is queued, never typed, since an Enter answers a dialog (actions.go:72-76). A pending permission of atrium's own is answered **deny** with the wrap-up text as its reason, which delivers it in the same breath. | The permission chain already carries a queued message in a deny (AGENTS.md, chain step 2, `onPermRequest` internal/daemon/daemon.go:877). |
| A permission asked during the wrap-up | Asked as any other, through the whole chain. The deadline keeps running. | Auto mode and rules still answer it first. A wrap-up is not a reason to approve anything. |
| Shelved card | No wrap-up. | Chain step 3 refuses every tool call a shelved card makes, so the agent could do nothing. |
| `reclaimInventory` (a cull of a merged worker) | No wrap-up. | The worker already ended with `atrium_done` and a sha, and the launcher culls after the merge. Its session is parked or gone. |
| `POST /v1/cards/archive {cwd, wait}` (gwt, g-close-by-cwd) | No wrap-up. The session is stopped and the close runs. | gwt waits 30 seconds and deletes the folder anyway (claude-shell.ps1:507-510). A wrap-up cannot fit and would be cut off by the delete. |
| The room restarts during a wrap-up | The row is read on start. A live session (pty host on) keeps its deadline. A session gone takes the no-session rule. | 2.2. |

## 6. Custom close steps

### 6.1 Shape

A close step is a card action without a button: a label and a prompt, scoped, ordered and switched on or off. It
copies `CardAction` (internal/store/actions.go:37-58) and its `Offers` rule (:63-85): every condition must pass and an
empty one passes everything.

| Field | Meaning |
| --- | --- |
| `label` | What the board shows in the progress list ("run the leak check"). Required. |
| `prompt` | What is said, at most `MaxActionPrompt` (4000, actions.go:35). |
| `tag` | Only cards carrying this tag. |
| `runner` | Only cards on this harness. |
| `repo` | Only cards whose repository matches, as `host/owner/repo` or a glob (`github.com/openziti/*`). New, since a close step is most often about a repository. |
| `sort`, `enabled` | Order inside its level, and an off switch that keeps the text. |

### 6.2 Two levels

- **Board-wide**, on the hub, a hub setting like the deploy owner (internal/link/deploy_mcp.go:31). The room asks the
  hub for the steps when it arms a close. A hub that does not answer in 5 seconds means the board-wide steps are
  skipped, with "the hub's close steps could not be read" in the progress and the event. A close never waits on the
  hub, as no source is ever durable state (AGENTS.md resilience 6).
- **Per room**, in the room store, one new table at the end of the migrations, shaped like `card_action`.

### 6.3 Order

Fixed first, then board-wide by sort, then the room's by sort. Fixed first because "stop what is in flight" is useless
after a custom step has started new work. Board before room because the room is the narrower scope and reads as the
last word, the same way a more specific rule wins in the permission chain.

### 6.4 Scoping is decided at arm time

The steps that match are written into `card_close.steps` when the close is armed, so a step edited during a wrap-up
does not change what the board says was asked.

### 6.5 Can a custom step hold

Open choice 5. The fixed step 6 says "if you cannot finish a step", which includes the custom ones unless the custom
heading says otherwise.

## 7. On the board and the phone

### 7.1 The confirmation (board, sharing.js closeCardNow)

u-005's dialog gains one line above "Closing deletes": "First the agent wraps up: stops what it is doing, ends what
it started, lists what would be lost, writes what it learned (5 minutes at most)", followed by the custom step labels
that match. On a card with no live session the line says "No session to wrap up". The button reads "close" as today.

### 7.2 While it wraps up

The dialog stays up, as it does for a close today, and the card carries a strip under its title:

```
closing · wrapping up · 3:41 left            [close anyway] [keep it open]
  ✓ prompt typed
  … stop, end, free, list, learn, run the leak check
```

The strip is driven by `close-progress` events with new steps `wrapup` (said, typed or queued, answered, held,
deadline) and `recheck` before today's steps. The activity badge stays what it is (activity is a badge, never stored,
AGENTS.md). The phase and deadline come from the stored row, so a board opened later or reloaded shows the same strip.
The column stays `running` during the wrap-up, since the agent is working.

"close anyway" and "keep it open" are there during the wrap-up too, so the human never has to wait out 5 minutes.

### 7.3 Held

The card moves to needs-input, the strip turns amber with the reason, and the three buttons of 4.3 show. The growler
says the same on every board and the phone. A recheck hold (2.1) shows the dialog's ask rows again for the rows that
grew, instead of "tell the agent".

### 7.4 The phone

`internal/api/web/m/js/resume.js` gets the same strip in the card row and the same three buttons in its sheet. Today
its `cleanUp` (:121-131) answers every ask `delete` without showing anything when "don't ask again" was ticked
(:128, `CLEANUP_KEY`, :31). That breaks u-005's "close confirms first" and is a bug apart from this design. The close
workflow build fixes it by always showing the asks.

## 8. Changes to the code, by file

| File | Change |
| --- | --- |
| `internal/store/schema.go` | `card_close` and `close_step` at the end of the slice. |
| `internal/store/closes.go` (new) | the row's reads and writes, the steps table, `Offers` with `repo`. |
| `internal/daemon/closewrap.go` (new) | `wrapUpRules`, saying it, the deadline timer, `/hold`, turn end and question as answers, restart recovery. |
| `internal/daemon/messages.go` | `turnEndedBecause` tells the wrap-up its turn ended. |
| `internal/api/close.go` | `postClose` arms instead of freeing when a wrap-up applies. A recheck function comparing asks with `seen`. `closeCard` unchanged except the `closing` guard. |
| `internal/api/closeprogress.go` | the `wrapup` and `recheck` step keys. |
| `internal/cli/hold.go` (new), `internal/cli/close.go` | `atrium hold`, and `atrium close` waiting through the phases. |
| `internal/link/control_mcp.go` | `atrium_hold`. `atrium_close` documents the 202. |
| `internal/link/growl.go`, `internal/hubstore/schema.go`, `internal/hubstore/growl.go` | the `close-hold` reason and its table rebuild. |
| board: `sharing.js`, card strip, settings | the line in the dialog, the strip, the held buttons, two settings sections for steps. |
| phone: `m/js/resume.js` | the strip, the buttons, the confirmation always shown. |

## 9. Tests that prove it

Go integration tests use the room's fake runner and a real git repository in a temp folder, like
`internal/api/close*_integration_test.go`.

1. **Order.** A close with a live session: the wrap-up prompt is typed before any `close-progress` step of
   `closeCard`, and the session is killed only after the turn ends. Asserted from the event order.
2. **The text is fixed.** The typed prompt starts with `wrapUpRules` byte for byte, and no setting changes it. A test
   writes every setting the room has and checks the text again.
3. **Custom steps.** Board-wide and room steps, scoped by tag, runner and repo: only the matching ones are appended,
   in the order fixed, board, room, numbered from 7. A hub that does not answer skips the board steps with the
   warning and the close goes on.
4. **Done.** A turn end with no hold frees the inventory. The card ends in done with `atrium:closed`.
5. **Hold.** `atrium hold "x"` during the wrap-up: nothing is freed, the card is needs-input with `close-held`, the
   session is still alive, the event carries the reason. A hold with no close pending answers ok, not recorded.
6. **Question and blocked.** A turn ending on a question, and an `atrium_blocked`, both hold.
7. **Deadline.** A fake clock past the deadline takes the rule clint picks in Open choice 2.
8. **No session, shelved, reclaim, gwt.** Each skips the wrap-up and frees, and the progress says why.
9. **Mid-turn and dialog.** Mid-turn the prompt is typed at once, with a dialog open it is queued, and a pending
   permission is answered deny with the prompt.
10. **Recheck.** The fake agent adds 10 dirty files to a worktree answered delete: the close holds with the recheck
    reason and asks only that row. The fake agent pushes a worktree answered keep: that row is freed with no ask.
11. **Restart.** The room restarts mid wrap-up with the session gone: the close finishes by the no-session rule. With
    the session alive (pty host): the deadline holds.
12. **Growler.** Hub side: a card with `close-held` raises a `close-hold` growler, also for an agent-launched card,
    and close anyway ends it. The table rebuild keeps existing rows (migration test).
13. **Board.** Headless units: the dialog's wrap-up line, the strip from a stored row after a reload, the three held
    buttons, settings for steps. Phone: the confirmation shows even with `CLEANUP_KEY` set.
14. **Learnings.** Per the answer to Open choice 1: a LEARNED.md in the worktree is collected before the worktree goes,
    and an empty one or a missing one files nothing.

## 10. Plan

| Item | What | Depends on |
| --- | --- | --- |
| r-005a | `card_close`, arming, the fixed prompt, done by turn end, deadline, no-session rules, recheck. Room only. | u-005 and hub-branch-inventory landed, so close.go is settled |
| r-005b | `atrium hold`, `atrium_hold`, the held state, the `close-hold` growler. | r-005a |
| r-005c | Board and phone: dialog line, strip, held buttons, phone confirmation fix. | r-005a, r-005b |
| r-005d | Custom close steps: tables, hub setting, settings UI. | r-005a |
| r-004 | Learnings, as Open choice 1 settles it. | r-005a |

## Open choices

Ranked by how much of the build each one changes. Each stands alone.

### 1. Where do learnings go, given that a pushed commit makes the hub branch "unmerged"?

r-004 (held, decided by the orchestrator, not by clint) says: in atrium's own repo, edit AGENTS.md on the card's
branch, commit and push to the hub. Every other repo: `dotagents/<host>/<org>/<repo>/AGENTS.md`, uncommitted. u-005
says a hub branch that is not an ancestor of `claude/main` is an ask in the close preview. A learnings commit made
during the wrap-up is, by definition, not in `claude/main`, so every closed atrium card would ask keep or delete about
its hub branch, every time.

- **A. Collect a file (recommended).** The agent writes `.atrium/LEARNED.md` in the worktree. The close reads it
  before freeing the worktree and files it on the hub, tagged with the repo, for a later fold into AGENTS.md (by the
  minion for atrium, by clint for dotagents). Cost: a collect step in close and a place on the hub to keep them, plus
  a fold job. Works on every room, including ones without a dotagents folder. No branch is touched.
- **B. r-004 as written.** Commit on the card's branch. Cost: an ask on every atrium close, and learnings only land
  when that branch merges, which a closed card's branch usually does not.
- **C. dotagents for every repo, atrium included.** Cost: only works on rooms whose machine has clint's dotagents
  checkout, so m1mini and sgg cards learn nothing, and clint commits all of it by hand.

### 2. The agent never answers within the deadline: close, or hold?

- **A. Close (recommended).** The human already confirmed, the answers stand, and the recheck still asks about any
  worktree that got worse. The progress and event say "the wrap-up did not answer in 5 minutes". Cost: whatever the
  agent was half way through freeing outside the folder (a container) may be left behind.
- **B. Hold.** The card resurfaces with a growler "did not answer". Cost: every stuck or slow agent turns a close into
  a second decision, and a close never finishes unattended.

### 3. May the wrap-up commit and push the card's own unsaved work?

Clint's text says "scan for files we didn't commit. scan for files we'd lose", which is a scan.

- **A. List only (recommended, and what step 4 says).** The human's keep, stash or delete answer covers it, and stash
  already pushes to the hub (close.go:506-521). Cost: none beyond today.
- **B. Commit and push to the hub.** Cost: commits nobody reviewed land on hub branches, and the hub branch becomes
  unmerged, which turns into an ask (the same collision as choice 1).

### 4. A parked card (transcript kept, no process): wake it to wrap up, or skip?

- **A. Skip (recommended).** r-004 already says a card with no session skips it. Cost: a parked card's learnings are
  lost, and anything it left running outside atrium's inventory stays.
- **B. Wake it.** Resume the session with the wrap-up as its first prompt. Cost: a cold start per close (time and
  tokens), and a close of an old card takes a minute before it can even start.

### 5. Can a custom close step hold?

- **A. Yes (recommended).** One verb, one rule: "if you cannot finish a step, hold". A step like "make sure the PR has
  a description" is only useful if it can stop the close. Cost: custom steps can make closes stop more often, which
  is the operator's own doing and visible in the reason.
- **B. No.** Custom steps are best effort and the heading says so. Only the fixed steps can hold. Cost: a step that
  finds a real problem can only mention it in its last line, which nobody reads after the card is gone.

### 6. After a hold, "tell the agent": does the close stay armed?

- **A. Armed (recommended).** The message is said, the close goes back to wrapping with a fresh deadline, and the
  next turn end without a hold completes it. The strip says "close resumes when the agent ends its turn". Cost: a
  human who meant "never mind, keep working" has to press keep it open instead.
- **B. Disarmed.** Telling ends the close. Closing later means pressing close again, which wraps up again. Cost: a
  second wrap-up turn and a second confirmation for the common case "go ahead, just commit it".

### 7. Can a close skip the wrap-up from the start?

Clint: "atrium just does it for you". The design offers close anyway only while a wrap-up runs or after a hold.

- **A. No skip in the confirmation (recommended).** Cost: closing a card known to be junk still costs one turn, or a
  press of close anyway on the strip.
- **B. A "skip the wrap-up" box in the confirmation.** Cost: the box becomes the habit, and the learnings and the
  in-flight stop are skipped exactly when nobody is watching.
