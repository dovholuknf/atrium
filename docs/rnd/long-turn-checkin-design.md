# The wtf-o-meter: atrium asks a long turn what it is doing, and its director decides

Status: design by @rnd, 2026-10-02. Nothing built. Asked by clint through the orchestrator. In short: a turn of two
hours is too long, and after about 30 minutes atrium itself should ask the card what it is doing, so that whoever
launched it can stop it if it has gone off the rails. Builds on `docs/rnd/held-message-escalation-design.md` (stage
R3, built) and the queued-message step of the permission chain (built).

## 0. The answer

- **What happened.** f-c-preflight, a worker of @fabric's, stayed in one turn for 2h08m. It produced 106k output
  tokens, made no commit, and left 1181 uncommitted lines against a two-item ask. Nobody saw it until clint read the
  turn time on the board. One three-line status ask exposed it, and @fabric cut it back to scope.
- **What atrium has today.** Stage R3 flags a turn over 45 minutes and tells the launcher once, as "a report, not an
  action" (`internal/daemon/a2a.go:762-797`). It never writes to the worker. That is a stated rule
  (`a2a.go:30-36`): atrium never forces a turn, and automatic notices only travel upward. A report to a busy director
  is easy to miss, and nobody asks the worker anything.
- **What changes.**
  1. A **meter** per running card climbs from the turn's length. The commit gap and the uncommitted diff can bring
     it forward (section 2).
  2. At the first level, atrium **queues a check-in for the worker itself**. It is delivered at the worker's next
     tool call, through the permission chain's queued-message step, so no turn is forced. The worker sends its
     launcher three lines: done, left, stuck (section 3).
  3. The launcher **answers continue, cut scope, or stop**, with one tool call. An answer resets the meter. No
     answer lets it climb, and it goes up the chain, never to clint's phone (section 4).
  4. The board shows the meter on the row, with playful copy (section 5).
  5. A director can **sanction a long job in advance**: a build, a mutation run, a long piece of research. A
     sanctioned card shows its allowance and is not asked until the allowance runs out (section 6).
- **The one rule this reverses.** Since R3, atrium "never writes to a worker". The check-in writes to the worker
  once per level. This is clint's ask, so the reversal is decided. The token argument behind the old rule still
  holds, and is met: the check-in rides a tool call the worker was making anyway, and fires at most once per 30
  minutes per card.
- **The old rule's other reason, no loop of automatic messages, still holds.**
  - Only the meter's timer starts a check-in. No message, reply or notice ever starts one, so nothing atrium sends
    can cause another check-in.
  - The bound per card, between two answers from the launcher: at most 4 check-ins (levels 1 to 4), 4 facts
    notices, and 4 "no answer" notices one step up the chain. An answer resets the level, and it takes a person or a
    model to give one.
  - A card cannot send as `atrium`. The sender of a queued message is stamped by the daemon from the calling card
    (`atrium_say` has no sender field). The check-in mark is a column on the message, not its text. So a card that
    writes "atrium check-in:" in a message is shown under its own name. The worker's model reads the name in the
    banner, not the column, so `atrium` is also reserved: no card may take it as a handle or an alias.
  - W2 rewrites the comment at `a2a.go:30-36` to say this: atrium writes to a worker only for the timed check-in,
    within this bound.

## 1. First, find out why R3 did not catch it

R3 is on `claude/main` (0b16fe78, 2026-09-30). On m1mini, the `a2a_notice` table has never held a `long-turn` row,
only `ended` and `silent-stop`. Before building, @runtime checks three things on the room f-c-preflight ran on:
- Was that room's build older than R3? The pause has held back room deploys.
- If R3 was there, did @fabric get the 45-minute notice? Look for "has been in one turn" in the room log and a
  `long-turn` row in `a2a_notice`.
- If the notice was sent, where did it land? It may have been typed into @fabric's own busy turn, or held on a
  hold-notices card.

Whichever it was, the design below still stands. A report the launcher must notice and act on is the part that
failed. Its answer goes in section 9 of this doc, because it sets how much of stage W1 is new.

## 2. What counts

**The clock is the turn.** `activityTracker.turnAt` (`internal/daemon/activity.go:200`) holds when the current turn
began, from hooks, for claude, codex and opencode. Gemini and ollama have no hooks, so they get no meter, which is
said on their row. Two corrections:
- **Time waiting on clint does not count.** A turn blocked on a permission dialog or an operator question pauses the
  clock. The pause is from the permission request until its answer. (Verify: whether `waiting` currently ends the
  turn in `set`, activity.go:461.)
- **The turn start survives a restart.** It is persisted with the card's activity, together with the session it
  belongs to. Today it lives in memory, and the comment at activity.go:200 says "a restart is a new turn anyway".
  That holds for the session, not the daemon. A deploy restarts the daemon while the card's session keeps running
  mid-turn. The next hook then opens the clock from zero, so a runaway would get a fresh 30 minutes after every
  deploy. On reload, the persisted start is kept only while the same session is still running. A resumed session
  starts a new turn, as it should.

**Two facts bring the meter forward.** Both are read through the same code as `GET /v1/tasks/{id}/changes`
(`internal/daemon/changes.go`). Reads start once a card is 5 minutes into a turn, every 2 minutes until half the
threshold, and every minute after that. Each read costs one `git status` and one `git diff --numstat`, and only for
cards in a turn that has a worktree.
- **Uncommitted lines.** Added plus removed lines, not yet committed in the card's worktree. Over 400 lines, the
  meter starts at level 1 at once, whatever the clock says.
- **Time since the last commit.** If the worktree has commits this turn, the clock counts from the newest one, not
  from the turn's start. A worker that commits every 20 minutes is never asked. The rule is "this long with nothing
  committed", which is what clint meant by off the rails.

**Signals left out, and why.**
- **Output tokens** are known only at Stop (`internal/daemon/usage.go`), so a reading mid-turn means parsing the
  transcript every tick. Time and diff already catch the same case. Tokens go in the check-in's fact line where
  cheap, but they do not move the meter.
- **The same files churned again** needs a count of edits per path from PostToolUse. That is cheap, but it is a
  second-order signal. It is left for after the meter has run for a week (W5).
- **Diff size against the brief's scope.** No brief declares a size today. Section 6 lets the launcher give an
  allowance, and that does this job without parsing the brief.

**Thresholds.** These are the defaults. Each is a room setting with an environment override, beside
`escalate.turn_after`.

| who | level 1 (check-in) | then every | why |
| --- | --- | --- | --- |
| a worker (`atrium:subagent`, or any card with a launcher) | 30 minutes with nothing committed | 30 minutes | clint's number |
| a director (`atrium:director`) | 60 minutes | 30 minutes | a director's turn is mostly waiting on workers, and its workers carry their own meters |
| the orchestrator, or a card clint launched | no check-in. Meter only, from 60 minutes | | nobody above it but clint, and this never pages clint |

Per card, the launcher's allowance (section 6) replaces the clock for that card.

**Levels.** Level 0 is under the threshold. Level 1 is at the threshold, and the check-in is queued. After that, one
level per further 30 minutes without an answered check-in, up to level 4. An answer resets the card to level 0 with
a fresh allowance.

## 3. What fires

**Atrium queues the check-in, as a message on the card.** It goes in the same queue `atrium_say` uses
(`internal/daemon/messages.go`), from the sender `atrium`, marked as a check-in. It is delivered by step 2 of the
permission chain (`onPermRequest`, `internal/daemon/daemon.go:873-884`). The worker's next tool call is blocked once,
with the check-in as the reason. The model reads it, writes its lines, and carries on.

The text:

> atrium check-in: you have been in this turn for 34 minutes with nothing committed, and 612 lines are uncommitted.
> Tell rnd-director, with atrium_say, in three lines:
> done: what is finished.
> left: what is left of what you were asked.
> stuck: what is in the way, or "nothing".
> Then carry on unless they say otherwise.

- **To whom.** The worker's launcher, from `launcherOf` (`a2a.go:243`). If the card has no launcher, the
  orchestrator gets it. The check-in names the handle, so the worker does not have to look it up.
- **Why not the Stop hook.** Blocking a Stop forces a whole extra turn, which is the cost the old rule guarded
  against. Also, a card that is mid-turn is by definition not stopping. The permission step costs one blocked tool
  call.
- **One long tool call** (a build, a test run, a `sleep`) makes no tool call, so the check-in waits. The existing
  long-tool notice to the launcher (`ATRIUM_A2A_LONG_TOOL`, 20 minutes) covers that case. The meter shows "in Bash
  25m" instead of climbing, until that call ends (section 6).
- **Codex and opencode.** Codex delivers through the same path (`runnerDelivers`, a2a.go:500). Opencode shows a deny
  as a thrown error (`scripts/opencode/atrium.js:317`), which still puts the text in front of the model. (Verify:
  that the model reads it as an instruction and not as a failed call to retry.)

**Atrium adds the facts it knows.** After a check-in is delivered, the first message from the worker to its launcher
counts as its answer. Right after it, atrium sends the launcher a separate notice with one line of facts. It does not
edit the worker's message:

> facts: turn 34m, last commit 1h02m ago (a1b2c3d), 612 lines uncommitted in 9 files, 41 tool calls in the last 10m.

The launcher judges the worker's account against what atrium measured, not against the worker's word alone.

**A worker that never answers.** If no message reaches the launcher within 10 minutes of delivery, atrium sends the
launcher the facts line by itself, with "no answer from the card". The meter goes up one level.

## 4. What happens next

The launcher answers with a new tool, `atrium_checkin`:

| answer | what atrium does | the meter |
| --- | --- | --- |
| `continue` (with optional `minutes`, default 30) | sends the worker "carry on" as a normal message | level 0. The next check-in is after `minutes` |
| `cut`, with `text` | sends the worker the text, prefixed "cut scope:", by the same queue | level 0. The next check-in is in 15 minutes, so a cut gets checked |
| `stop`, with optional `text` | sends the worker "stop: commit what is useful on a branch, report, end your turn". It is delivered at the next tool call | the meter shows `stopping`. If the turn has not ended within 10 minutes, the launcher is told, and can use `atrium_exit` |

- **Why a tool, not a reply in words.** atrium has to know the answer to reset the meter. A free `atrium_say` back
  to the worker still works, but it leaves the meter climbing. The check-in text tells the launcher which tool to
  use.
- **No answer from the launcher** within 15 minutes:
  - The meter climbs.
  - The facts and the worker's lines go one step up the chain: the launcher's own launcher, else the orchestrator.
  - The orchestrator holds notices (`holdsNotices`, `a2a.go:320`), so it reads them when it asks.
- **Never clint.** Nothing here rings the board's bell, sends a push, or types into the operator's terminal. The
  meter is its own field on the card, not an `Escalation`. So `isStuck` (`stack.js:571`) and the bell in
  `settings-spine.js:1583` never see it. For a card with a meter, R3's `long-turn` escalation is not raised. Today
  that escalation does ring the bell when the stuck setting is "alert", and a climbing meter would ring at every
  level. W4 tests this. clint sees the meter on a look at the board, and that is the point.

## 5. The board side

The meter replaces the `turn 1h32m` chip planned in held-message U1, on the board card and on the strip row. It is
always on the face, never hover-only (u-032).

| level | chip | colour | tooltip |
| --- | --- | --- | --- |
| 0, past half the threshold | `⏱ 18m` | none | in this turn for 18 minutes, last commit 12 minutes ago |
| 1, check-in sent | `🤔 34m · checking in` | amber | atrium asked what it is doing. Waiting on its three lines |
| 1, answered "continue" | `👍 41m · rnd-director says go` | none | allowed until 1h11m |
| 2 | `😬 1h04m · champ?` | amber | no answer for 30 minutes. Told the next one up |
| 3 | `🚨 1h34m · off the rails?` | red | two check-ins unanswered |
| 4 | `🚂💨 2h04m · gone` | red, and a slow pulse | three unanswered. The operator sees it here, and nowhere else |
| sanctioned | `🏗 1h10m of 2h · mutation run` | blue | allowed by fabric-director for 2h: "mutation run" |
| stopping | `🛑 stopping` | grey | told to stop 4 minutes ago |

- The meter's name on the board is the **wtf-o-meter**, shown in the tooltip's title.
- A row's level is also a filter on the board, "show off-the-rails cards", in the same menu as the other row filters.
- *Acceptance test.* Headless Playwright on a mocked card list: each chip at its level, with the right text and
  colour, on both views. A sanctioned card shows the allowance. Nothing calls the notification API.

## 6. False positives: work that is long on purpose

| case | what happens |
| --- | --- |
| **One long build or test call** | inside one tool call, the meter holds at its level and shows the tool and its age. The launcher's long-tool notice at 20 minutes is unchanged. When the call ends, the meter carries on from the turn's clock |
| **A mutation run, or other background work** | the Stop payload's `background_tasks` is known only at Stop. So a card that will wait on long background work should be sanctioned (below). Without that, it gets the normal check-in, which costs one blocked tool call and three lines |
| **Sanctioned long research or jobs** | the launcher gives an allowance, at launch or at any time: `atrium_launch` gains `allow_minutes` and `allow_why`, and `atrium_checkin continue` takes `minutes`. Until the allowance runs out, the card shows the blue chip, and no check-in fires. At the end of the allowance, the normal check-in fires. An allowance is capped at 4 hours, so nothing is sanctioned forever |
| **Live subagents** | a worker running its own `Task` subagents (`subagentStarted`, activity.go:630) is still asked. The question is the same, and its own answer can say "waiting on 3 subagents" |
| **A card blocked on clint** | the clock pauses (section 2), so a card waiting two hours on a permission dialog never climbs |
| **A director with workers out** | the director's threshold is 60 minutes. Its workers carry their own meters, and a2a.go:469 already treats a director with outstanding workers differently for silent stops |
| **Compaction** | counts as working, like today. A turn that compacts twice is a turn worth asking about |

## 7. Settings

| setting | default | env |
| --- | --- | --- |
| `checkin.worker_after` | 30 minutes | `ATRIUM_CHECKIN_WORKER` |
| `checkin.director_after` | 60 minutes | `ATRIUM_CHECKIN_DIRECTOR` |
| `checkin.every` | 30 minutes | `ATRIUM_CHECKIN_EVERY` |
| `checkin.dirty_lines` | 400 | `ATRIUM_CHECKIN_DIRTY` |
| `checkin.answer_within` | 10 minutes for the worker, 15 for the launcher | none |
| `checkin.allow_max` | 4 hours | none |
| `checkin.off` | false. When true, back to R3's report only | `ATRIUM_CHECKIN_OFF` |

R3's `escalate.turn_after` (45 minutes) stays for cards without a meter: gemini, ollama, and any card with
`checkin.off`.

## 8. Stages

| stage | what | owner | size | deploy |
| --- | --- | --- | --- | --- |
| W0 | section 1's three checks on the room f-c-preflight ran on | @runtime | XS | none |
| W1 | the meter in the daemon: the turn clock with waiting paused and persisted, the commit gap, uncommitted lines read from 5 minutes into a turn (section 2), the levels, and a `meter` field on the card | @runtime | M | room |
| W2 | the check-in: queued from `atrium` (a sender only the daemon can stamp), delivered at step 2, a facts notice after the worker's reply, a "no answer" notice, and the rewritten rule comment at a2a.go:30-36 | @runtime | S | room |
| W3 | `atrium_checkin` and `allow_minutes` on `atrium_launch`, with the chain-up on no answer | @runtime | S | room and hub (MCP tool list) |
| W4 | the board chip, tooltip and filter, which replaces U1's `turn` chip. Test: with the stuck setting at "alert", a card climbing to level 4 never rings the bell | @ui | S | hub |
| W5 | after a week: the churn count, and tuning the defaults from the check-ins actually sent and answered | @rnd | S | none |
| W6 | owed answers that survive (section 11): an open item on the launcher's card for every worker that stops owing an answer, a push to the orchestrator at 10 minutes, the list re-shown after the launcher's context clears, the W2 reservation of the `atrium` handle and alias checked on the room and on the hub | @runtime | S | room |

**Acceptance for W1-W3**, on a test room with a fake clock:
- A worker 30 minutes in with no commit gets one check-in at its next tool call, and the launcher gets its three
  lines with the facts line.
- A worker that committed 10 minutes ago gets no check-in.
- A worker with 500 uncommitted lines gets one at its first diff read, 5 minutes into the turn.
- Continue for 60 resets the meter, and the next check-in comes at 60.
- Cut is checked at 15 minutes. Stop delivers the stop text.
- No launcher answer in 15 minutes sends a held notice to the orchestrator. Nothing calls a notification or push
  path.
- A sanctioned card gets no check-in until the allowance ends.
- A card waiting 40 minutes on a permission dialog is at level 0.
- A restart in the middle of a turn keeps the clock.

**Acceptance for W6** is in section 11.

W6 does not depend on W1 to W5 and can be built first. The build waits for the pause to end, like everything else.

## 9. Questions for clint, held until asked

Order: the core picture first, then the mechanism, then the edge cases (`docs/rnd/interviewer-brief.md`).

1. **The core picture.** f-c-preflight is 30 minutes into its turn, with no commit and 600 lines uncommitted. atrium
   blocks its next tool call once and asks it to send @fabric three lines: done, left, stuck. @fabric reads them,
   with atrium's own facts underneath, and answers continue, cut or stop. Is that the flow? Or should atrium send the
   facts straight to @fabric without asking the worker anything?
   *Default: ask the worker.* The worker's account plus atrium's facts is what exposed the real case.
2. **Who decides when nobody answers.** It is 1h04m. f-c-preflight answered, and @fabric has not answered for 15
   minutes. Should atrium pass it up to the orchestrator, or tell f-c-preflight to stop by itself?
   *Default: pass it up, and never stop a card on its own.* Stopping a card is a director's decision.
3. **A sanctioned long run.** @fabric launches a mutation run that will take 3 hours. Should @fabric say so at launch
   ("allow 3h, mutation run") and the card go unasked until then? Or should it be asked every 30 minutes anyway,
   with @fabric answering continue each time?
   *Default: an allowance at launch, capped at 4 hours.* Asking every 30 minutes on a known job is the noise this is
   meant to avoid.
4. **Your own cards.** You start a card yourself and it runs for 90 minutes. Nobody launched it, so nobody can
   answer a check-in. Should it still get the meter on the board, with no check-in? Or should it ask you, with no
   push and only on the board?
   *Default: the meter only.* You said not to page you, and the board is where you will see it.
5. **The name and the copy.** On the board the meter is called the wtf-o-meter, and the chips go 🤔 checking in,
   😬 champ?, 🚨 off the rails?, 🚂💨 gone. Keep these, or would you rather name it yourself?
   *Default: keep them.* They keep the tone of the ask, made milder for a repo that is public.

## 10. Interview plan

When clint is ready, the orchestrator's interviewer card can run section 9 with `docs/rnd/interviewer-brief.md`:
- Question 1 is the whole picture. If clint's picture differs, everything after it is reworked from that picture.
- Question 5 last, and it may be answered just by looking at a mock of the chips.

The answers come back to @rnd as a file outside the repo, and the doc is revised from them.

## 11. Owed answers: a worker stops, and nobody hears it

Added 2026-10-02 evening, from the orchestrator. A separate failure from the long turn, folded in here because the
fix is the same kind of thing: atrium notices, and pushes it up the chain.

**What happened.** On the evening of 10-02, clint said go on the scm work, and it stalled for an hour without anyone
noticing. The details are in the evening entry of the factory log on sg4.
- @fabric launched nothing.
- r-hub-remote sat at needs-input for 67 minutes.
- r-scm-clone finished and reported to nobody.
- u-usage-chart's question to @ui was lost when @ui's context was cleared.

**What atrium does today, and where each case slips through:**
- **A silent stop** (`silentStop`, `internal/daemon/a2a.go:423`). It tells the launcher once, and only when a
  worker ends a turn at needs-input without saying anything. A worker that asked a question and is waiting for the
  answer has "reported", so it is not silent. That is r-hub-remote.
- **The ended notice** (`internal/store/ledger.go:786`). It is queued when a worker's session ends without a final
  report. If the stored launcher no longer resolves, there is nobody to tell (`launcherOf`, a2a.go:243). That may
  be r-scm-clone (verify, from the log).
- **Delivery.** A launcher without `atrium:hold-notices` has every notice, report and question **typed into its
  conversation** (`deliverPeer`). A context clear erases it, and nothing ever says it again, because
  `notifyLauncher` sends once per event (a2a.go:277). That is u-usage-chart.
- **Nobody above the launcher.** Nothing goes to the orchestrator when a launcher does not act. That is the hour.

**The change: an open item, kept by the board on the launcher's card.**
1. **It opens** when a worker that has a launcher reaches done, ended, needs-input or needs-permission, and owes
   an answer. "Owes" means one of these:
   - the worker ended with no report;
   - the worker's last message to its launcher came after the launcher's last message to it, i.e. a question or a
     report nobody answered;
   - the worker has been at needs-permission for 5 minutes.
2. **It is stored** with the held notices on the launcher's card (`holdNotice`, a2a.go:333), **whether or not the
   launcher holds notices.** A launcher that has its notices typed still gets the typed line, as a nudge. The
   record lives in the store, not in the conversation.
   A worker whose launcher no longer resolves keeps its item on the orchestrator's card, so it is never kept by
   nobody.
3. **It closes** when the launcher acts on that worker: it messages the worker, exits it, relaunches it, or
   dismisses the item with `atrium_task` (a new `dismiss` field). Only reading the notices does not close it. Reading
   is what @ui did before its context was cleared.
4. **At 10 minutes open,** atrium sends the orchestrator one held notice. For example: "fabric-director has not
   answered r-hub-remote for 10 minutes. r-hub-remote is waiting at needs-input. Its last words: <first 200
   characters>." The orchestrator holds notices, so this pages nobody, clint included. If the launcher is the
   orchestrator itself, the board's row chip is all there is.
5. **After the launcher's context clears**, by a `/clear`, an automatic new context, or a compaction: at its first
   tool call, step 2 of the permission chain delivers one line, "3 open items from your workers: r-hub-remote
   (needs-input 67m), r-scm-clone (done, no report), u-usage-chart (asked you a question 40m ago). atrium_task
   notices to read them." A new conversation is seen through SessionStart (`internal/cli/session.go`). (Verify:
   that a compaction gives a SessionStart, or else key this on the compact hook.)
6. **On the board,** the launcher's row shows `📬 3 owed`, and the chip turns amber at 10 minutes. It reuses the
   `held_notices` count already on the row, counting only open items. It never rings the bell.

**The bound.** Each open item is pushed at most three times: once to the launcher, once to the orchestrator, and
once more after each of the launcher's context clears. Every push goes upward. Nothing is written to the worker.
None of these pushes can start another.

**Not covered: a director that launches nothing after a go.**
- If the go came as a message from the orchestrator, the director's silent stop already tells the orchestrator,
  held (`stoppedSilently`, a2a.go:466).
- If clint typed it into the director, the director owes no report today.
- W0 also checks which of these happened to @fabric. A director that sits idle after a prompt from the operator
  belongs to the operator-focus design, not here.

**Acceptance for W6,** on a test room:
- A worker that ends with no report opens an item on its launcher's card. The launcher's row shows `📬 1 owed`.
- With no action in 10 minutes, the orchestrator gets one held notice, and nothing calls a push or notification path.
- A worker that asks its launcher a question and waits opens an item. The launcher's reply closes it.
- With the launcher's notices typed (no hold tag), the item is still on its card. After a `/clear`, the launcher's
  first tool call carries the one-line list.
- Exiting or dismissing the worker closes the item. Reading the notices does not.
- A worker whose launcher no longer resolves opens its item on the orchestrator's card, which shows it.

