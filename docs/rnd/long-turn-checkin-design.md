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
- **The turn start survives a restart.** It is persisted with the card's activity. Today it lives in memory only, so
  a restart quietly resets a two-hour turn to zero.

**Two facts bring the meter forward.** Each is read only for cards already past half the threshold, through the same
code as `GET /v1/tasks/{id}/changes` (`internal/daemon/changes.go`). This costs one `git status` and one
`git diff --numstat` per card per minute, and only for those cards.
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

**Atrium adds the facts it knows.** When the worker's three lines reach the launcher, atrium appends one line of its
own:

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
  escalation alert in `settings-spine.js:1583` ignores this source. clint sees the meter on a look at the board,
  and that is the point.

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
| 4 | `🚂💨 2h04m · gone` | red, and a slow pulse | three unanswered. Clint sees it here, and nowhere else |
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
| W1 | the meter in the daemon: the turn clock with waiting paused and persisted, the commit gap, uncommitted lines for cards past half the threshold, the levels, and a `meter` field on the card | @runtime | M | room |
| W2 | the check-in: queued from `atrium`, delivered at step 2, a facts line on the worker's reply, a "no answer" notice | @runtime | S | room |
| W3 | `atrium_checkin` and `allow_minutes` on `atrium_launch`, with the chain-up on no answer | @runtime | S | room and hub (MCP tool list) |
| W4 | the board chip, tooltip and filter, which replaces U1's `turn` chip | @ui | S | hub |
| W5 | after a week: the churn count, and tuning the defaults from the check-ins actually sent and answered | @rnd | S | none |

**Acceptance for W1-W3**, on a test room with a fake clock:
- A worker 30 minutes in with no commit gets one check-in at its next tool call, and the launcher gets its three
  lines with the facts line.
- A worker that committed 10 minutes ago gets no check-in.
- A worker with 500 uncommitted lines gets one at 5 minutes.
- Continue for 60 resets the meter, and the next check-in comes at 60.
- Cut is checked at 15 minutes. Stop delivers the stop text.
- No launcher answer in 15 minutes sends a held notice to the orchestrator. Nothing calls a notification or push
  path.
- A sanctioned card gets no check-in until the allowance ends.
- A card waiting 40 minutes on a permission dialog is at level 0.
- A restart in the middle of a turn keeps the clock.

The build waits for the pause to end, like everything else.

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
