# Who owns the context limit: atrium's new context or the runner's compaction

Status: study by @rnd, 2026-10-02 late, asked by the orchestrator. Design only, nothing built. For @runtime.

## The ask

On 10-02 every room had `auto_new_context` off, at the default limit of 300k, so Claude Code compacted on its own. The
orchestrator compacted at about 190k, and workers at 236k to 300k. The orchestrator then set mode `agents` and
`auto_new_context_k` = 200 on sg4-control, claude-sg4, m1mini and sg3. Three questions:

1. Which gives better work after the event: a clear plus a handoff, or a compaction? Compare quality, tokens, lost
   state and re-ramp time, measured on real cards.
2. Should atrium own the limit and the cycle for every runner (claude, opencode, others), instead of relying on each
   runner's compaction? What does each runner expose that makes this possible?
3. Should the runner's autocompact then sit above atrium's limit, or be off?

It also asks to fold in `docs/backlog/runtime/r-new-context-clear-vs-restart.md` and `r-new-context-limit-layer.md`.
They were untracked on sg4, and the orchestrator sent copies. Section 7 folds them in. One is a restart that lands
during a clear, which is already built. The other is a stored per-runner limit, which is held, and which this design
gives a place in the one function.

## The answer

1. **On the measured numbers they tie. On what atrium needs, the clear is better for long-lived cards, and compaction
   is better for short workers.**
   - Tokens over the next 10 calls are the same.
   - Compaction gets back to work faster, and it ran mid-turn without breaking anything that was measured.
   - The clear leaves a file that a person can read, that survives a restart, and that can move to another runner. A
     compaction summary is none of those.
   - Repeated compactions do not shrink, and they leave the handoff file stale.
2. **Yes: atrium owns the limit and the timing for every runner. The runner's mechanism differs per runner, and it
   runs through a per-runner adapter.**
   - There are three actions:
     - `clear`, which is the new-context cycle;
     - `restart`, which exits and relaunches with the wake prompt (the one action every runner can do);
     - `compact`, which is the runner's own compaction, at a size atrium sets.
   - Policy:
     - directors and the orchestrator get `clear` at the limit;
     - workers get `compact` at the limit;
     - runners that cannot clear get `restart`.
3. **Above atrium's limit, never off. The shipped backstop math is wrong, though, and that is the first thing to
   fix.**
   - Claude compacts about 33k below its `--autocompact` value.
   - The shipped flag is the limit plus 10%. That fires before atrium's limit at every limit under about 330k, which
     includes tonight's 200k.
   - So the new settings do not do what they were set to do. Claude cards launched since the change compact at about
     187k, before atrium's 200k cycle can start (section 3).

## 1. What was measured

The sources are m1mini only, from 09-28 to 10-03 01:30Z:

- the transcripts under `~/.claude/projects`;
- `atrium.db`, opened read-only;
- a script that pairs each event with the 10 calls that follow it.

The orchestrator runs on sg4-control, so it is not in the sample. The raw numbers and the measuring
scripts are off-repo on m1mini, because they read transcripts. They are in the blog sources (`rndctx/`: `main.py` and
`report.py`, with the report `rnd-ctx-measure.md`). L5 reruns them.

| | Compaction | Clear + handoff |
|---|---|---|
| Events | 26, all automatic (13 director, 8 worker, 1 other, 4 forks of review) | 5 (one ceiling card, three directors, one worker) |
| Context before (median / p90) | 298k / 360k | 204k / 437k |
| Context on the first call after | about 36k | about 36k |
| Context by the 10th call | about 53k | about 51k |
| Tokens over the next 10 calls (input + cache) | 0.43M | 0.44M |
| Reads before the first edit, say or commit (median / p90) | 2 / 4 | 5 / 10, the handoff plus 2 to 4 orientation calls |
| Wall time to that first act (median / p90) | 0.2 / 2.0 min | 0.6 / 5.9 min |
| Cost of the event itself | blocks the card 56 / 74 s, plus one summarising call over about 300k that is recorded nowhere | 18 to 47 s, plus a capture turn of 0.3M to 1.4M tokens |
| Carried state | a summary of about 13.5k chars (median), inside the conversation | a handoff of 4k to 12k chars at the wake. Director files on disk are now 21k to 28k |
| Lost state found | none, by machine check plus 6 read by hand | none |

For scale, a fresh worker starting from BRIEF.md makes 14 / 30 reads before its first act.

What the table cannot say:

- Five clears give no meaningful p90.
- All 26 compactions fall in about 7 hours of one evening, after the `--autocompact` deploy at about 18:43.
- Lost state is hard to see. A decision that was quietly dropped from a summary does not show up as "where was I".

The data shows no quality gap either way. The decision therefore rests on the differences the table does not price.

**For the clear:**

- **The handoff is an artifact.** It is a file in the worktree that a person, the launcher or a later card can read.
  It survives a room restart and a move to another runner (runner-switch design). It is also the input of the lean
  cycle's rewrite. A compaction summary lives inside one runner's conversation and dies with it.
- **Compaction chains.** Every card that compacted twice did so with no clear in between: review 5 times, ui 4,
  runtime and rnd 2 each. The summary did not shrink along the chain, so each step carries the previous summary
  forward. The vendors say themselves that compaction loses information, and that early instructions can get lost.
  The public issue trackers of all four runners carry reports of lost instructions and of compaction loops.
- **The handoff goes stale.** ui ran three compactions while noting that HANDOFF.ui.md was out of date. Its next clear
  would have woken from an old file. That makes compaction worse than it looks: the risk is deferred to the next
  clear.

**For compaction:**

- **No capture turn.** A clear's capture turn costs 0.3M to 1.4M tokens of input and cache. Compaction's one
  summarising call is over a similar context, so the event costs about the same. The clear, though, also pays the
  orientation reads after the wake.
- **It works mid-turn.** 16 of the 26 compactions happened inside a turn, and each card carried on. atrium's cycle
  types only between turns (r-new-context-mid-turn), so a long turn that crosses the limit can only be compacted.
- **Workers ramp in about 2 reads.** A worker has no handoff discipline. Its brief is BRIEF.md, and its launcher holds
  the record. A clear plus a handoff would add a capture turn to a card that lives an hour.

## 2. Policy: one limit, three actions

atrium owns:

- the number;
- the reading of the context size;
- the decision when to act.

The runner owns only the mechanism.

| Card | Action at the limit | Why |
|---|---|---|
| Orchestrator, director, ceiling card | `clear`: capture, `/clear`, wake | Resident for days. The handoff is the record, and it is what survives a room restart or a runner switch |
| Worker (`atrium:subagent`) | `compact`, at a size atrium sets | Short-lived and cheap to re-ramp. Its record is BRIEF.md plus its launcher |
| Any card on a runner with no usable clear | `restart`: exit, then launch fresh with the wake prompt | The one action every runner supports |
| A card that crosses the limit mid-turn | the runner's `compact`, as the backstop | atrium cannot type the capture mid-turn. The long-turn check-in (W1-W2) covers the turn itself |

`restart` exists because `clear` is a typed `/clear` plus a wait for a new SessionStart. That is a Claude mechanism,
and Gemini has an equivalent, but Codex and OpenCode do it differently, and an unknown runner may have none. Exiting
and relaunching with the wake prompt as the launch prompt is what atrium already does for a resume, so it costs no
per-runner code. It costs a process start and an MCP reconnect, a few seconds. The transcript is dropped either way.

After a compaction, atrium does two things it does not do today:

- **It re-anchors the card.** It types one line with who the card is, its launcher and the handoff file name. These
  are the lines lean-cycle part B puts in the wake, and they are what a summary drops first. The signals are
  PostCompact, SessionStart with source `compact`, or a `compact_boundary` line in the transcript.
- **It marks the handoff stale.** The next capture prompt says "your handoff predates N compactions, rewrite it", so
  the next clear does not wake from an old file.

## 3. The backstop, and the bug in it

Since r-autocompact (16488230, c30db92c), every claude card starts with `--autocompact` set to `cardLimit` plus 10%,
clamped to the model's window. The intent was that atrium cycles at the limit and Claude compacts 10% later.

Measured on Claude Code 2.1.288, **compaction fires about 33k below the flag value**:

- a flag of 330k compacted at about 297k;
- a flag of 165k compacted at about 133k.

Claude reserves that margin for the summary itself. So the real backstop is `limit × 1.1 − 33k`, which is below the
limit whenever `0.1 × limit < 33k`, that is, at any limit under 330k.

| Card | atrium's threshold | Flag | Real compaction | Who acts first |
|---|---|---|---|---|
| 1M-window card, k = 200 (tonight's setting) | 200k | 220k | about 187k | compaction |
| 200k-window card, k = 200 | 140k (70% of the window, when the statusline is fresh) | 200k (clamped) | about 167k | atrium, by 27k |
| 200k-window ceiling card, ceiling 150k (ui, runtime) | 140k | 165k | about 133k | compaction |
| 1M-window card, mode off, k = 300 (10-02) | none | 330k | about 297k | compaction, as intended then |

This is why m1mini shows 26 compactions against 5 clears. At 18:44, review's deferred capture was overtaken by a
compaction. The card then wrote its handoff from the summary, and no clear ever followed.

The fix is one function. Both numbers come from it, at launch and in the watcher:

```
buffer   = the runner row's compaction margin (claude: 33k, a new field, measured, not guessed)
headroom = 20k  (a capture turn's growth)
window   = the model's window (modelWindowK), fixed at launch
flag      = min(min(cardLimit, window − buffer − headroom) + buffer + headroom, window)   (fixed at launch)
threshold = min(cardLimit, window′ − buffer − headroom, flag − buffer − headroom)
            where window′ is the statusline's window when it is fresh, else window
```

The flag is passed at launch and cannot move until the next launch or resume, while the threshold is re-read on every
tick. The threshold is therefore also bounded by the flag the card actually carries. A statusline window that differs
from `modelWindowK`'s guess (an unnamed model, say) can only lower the threshold. It can never put the threshold past
the flag's compaction point.

For a worker, which is never cycled (L2), the flag is `cardLimit + buffer`, clamped the same way. Its compaction
comes at its limit, not at the limit plus the headroom.

- 1M window, k = 200: the threshold is 200k and the flag is 253k, so compaction comes at about 220k, after atrium.
- 200k window: the threshold is 147k and the flag is 200k, so compaction comes at about 167k.

This keeps one rule: atrium always acts first by `headroom`. It replaces both the 70% factor and the 10% factor.

**What the headroom does not cover.** The 20k covers a capture turn started between turns. It does not cover a long
turn that crosses the threshold mid-turn. atrium's cycle waits for that turn to end, and if the turn grows more than
20k first, the runner compacts it. **That is by design:** the backstop exists for exactly this case, and 16 of the
26 measured compactions were mid-turn with no harm found. A larger headroom would only move the line, because a turn
has no bound on its growth, and it would waste window on every card. L5 measures how much a turn grows from the
crossing to its end. If most cycles are being lost to mid-turn compactions, the headroom is raised then, from that
figure.

**The margin is measured, not known.** The 33k comes from two points (330k fired at about 297k, 165k at about 133k)
on one Claude Code version, 2.1.288. A fixed margin fits both, and a ratio would not, since 10% would put the second
point at about 148k. It is therefore a runner-row field, not a constant, and it is re-checked in two ways:

- by L1 from every automatic compaction, whose `compact_boundary` carries `preTokens`. A compaction more than 5k away
  from `flag − margin` logs the observed margin and shows it in the runner editor;
- by hand at each Claude Code upgrade, with the real-card check in L0's done-when.

**The interim, until it is built:** no setting fixes it, because both numbers derive from the same k. Raising k to 330
or more puts compaction at k or later, but with no headroom, and it gives up the 200k the orchestrator wanted. The
honest interim is to accept that cards compact at about 187k tonight, which is the old behaviour at a lower number.
Nothing is lost, as section 1 shows.

**Off, or above?** Above, never off:

- **Claude with autocompact off** stops at the window with "Context limit reached · /compact or /clear to continue".
  A card mid-turn would sit dead until someone acted.
- **OpenCode** errors the same way.
- **Gemini** refuses to send the turn.
- **Codex** cannot turn it off at all.
- **The PreCompact veto** (Claude only) was considered and rejected. A veto skips that compaction, but the next
  request at the hard limit fails. It turns a backstop into a stall.

## 4. What each runner gives atrium

Checked on 2026-10-02 against Claude Code 2.1.288 and codex-cli 0.159.0, both installed on m1mini. OpenCode v1.18 and
v2, and Gemini CLI 0.62, were checked from their docs and source only. The full notes are off-repo
(`rnd-ctx-runners.md`, beside the scripts in the blog sources).

| | Claude Code | Codex CLI | OpenCode | Gemini CLI |
|---|---|---|---|---|
| Set the compaction size | `--autocompact <100k-1M>`, shipped in atrium | `-c model_auto_compact_token_limit=`, which can only lower its 90%-of-window cap | config v1 `compaction.reserved`, v2 `buffer` | `model.compressionThreshold` (a fraction) |
| Turn it off | yes, but not wanted (section 3) | no | yes | no switch |
| Read the context size | the transcript usage atrium reads today, plus the statusline window | `token_count` events in the session file | the session API (v2 `GET /api/session/{id}/context`) | token counts in the session files |
| Learn that it compacted | PostCompact, SessionStart `compact`, `compact_boundary` | PostCompact, SessionStart `compact` | event `session.compacted` (v1) or `session.compaction.*` (v2) | none |
| Clear and inject | `/clear`, then the wake (shipped) | `/new`, then the wake | HTTP: execute-command, then append and submit the prompt | `/clear`, then the wake |
| Compact on demand | `/compact` typed | app-server `thread/compact/start` | `POST /session/:id/summarize` (v1) | none |
| Action atrium uses | `clear` or `compact` | `restart` first, `clear` once tested | `clear` over HTTP | `restart` |

So the adapter is three fields on the runner row, next to `autocompact_args`:

- the compaction margin;
- how to read the size;
- which of `clear` and `restart` the runner supports.

The watcher (`watchContext`) runs on Claude cards only today (`isClaude`). It widens one runner at a time, as each
runner's reader lands.

Notes for the build:

- The PreCompact payload in `docs/rnd/hook-coverage-spike.md` names `triggered_by`. The current docs say `trigger` and
  `custom_instructions`. Check it against a captured payload before anything reads it.
- Forks of review inherit 250k to 272k and compact within 1 to 3 minutes. atrium credits those compactions to the
  review card. The `compacted` events should name the session, not just the card.

## 5. Stages

| Stage | What | Size | Done when |
|---|---|---|---|
| L0 | Backstop math (section 3): one function for the threshold and the flag, the margin as a runner row field, `threshold_k` in the card details, and the 70% and 10% factors gone | S, room deploy | a card at k = 200 on a 1M model starts with `--autocompact 253k` and is cleared, not compacted, in a test daemon with a fake transcript; and on a real card, launched at k = 200 on a 1M model, the transcript's first automatic `compact_boundary` shows `preTokens` of about 220k (±5k) on a turn long enough to pass atrium's cycle |
| LL | The limit layer (r-new-context-limit-layer, held): a stored per-runner limit as layer 2 of the function, in the API and the three runner editors, and the row's bar reading `threshold_k` | S + the board half | a codex row limit of 150 moves only codex cards' thresholds |
| L1 | After a compaction: re-anchor line, handoff marked stale, `compacted` events by session | S | a fake PostCompact gives one line and a stale note in the next capture prompt |
| L2 | Workers get `compact` at the limit: the flag is `cardLimit + buffer` (section 3), never a cycle, so the worker follows its runner row's limit (LL) and k like any card. The mode `agents` stays as it is | XS | a worker's flag follows k and its runner row's limit |
| L3 | `restart` action: exit, then launch with the wake prompt, journalled like a cycle, with a step for an exited runner that a room restart relaunches (section 7) | M | a non-Claude test runner is cycled by restart |
| L4 | Per-runner readers and margins: OpenCode, then Codex | M each | each runner's size shows on the card, and its cycle fires |
| L5 | Re-measure one week after L0, with the same script, on all rooms | research | the table in section 1 with more than 30 clears |

L0 stands alone, and it is the one that makes tonight's settings mean what they say. L5 decides whether directors keep
`clear` or go to `compact` too: if the re-measure still shows no quality gap and the handoffs are not being read,
`compact` is cheaper.

## 6. Questions for clint (held)

1. **Workers compact, directors clear.**
   - Scenario: an hour into f-hub-receive's brief, it reaches 200k. atrium sets its compaction at the limit, it
     compacts, and it carries on with a summary. It does not stop to write a handoff.
   - Meanwhile @fabric at 200k writes HANDOFF.fabric.md, clears and wakes.
   - Suggested: yes. A worker re-ramps in about 2 reads, and its record is its brief and its launcher.
2. **Restart where clear is not there.**
   - Scenario: a Codex card reaches its limit. atrium exits it and starts a new Codex on the same worktree, with "read
     HANDOFF.x.md" as the prompt.
   - It takes a few seconds longer than a clear, and you see the card restart.
   - Suggested: yes, for every runner except Claude, until each one's clear is tested.
3. **The runner's compaction is never off.**
   - Scenario: a director is 40 minutes into one turn when it crosses the limit. atrium cannot type the capture
     mid-turn, so the runner compacts it and it carries on.
   - With compaction off, it would stop at the window and wait for someone.
   - Suggested: never off. It sits just above atrium's limit as the backstop.

## 7. Folding in the two items

### r-new-context-clear-vs-restart: a restart that lands during a clear

The item is not about clearing against restarting. It is about two cases on 10-01:

- a hub deploy that ran during a clear on the orchestrator, after which the clear sat at step 1 of 3;
- an idle card whose capture waited for a turn end that never came.

It asked for four things:

- an idle card typed at once;
- the 409 naming the step;
- input refused during a clear;
- deploys waiting for a clear, and a restart ending a cut-off clear with its reason.

All four shipped as r-clear-vs-restart (7323b17f, re-read c685f9b6). Its open question, whether a clear is stored,
is answered by the `new_context_journal` setting.

Two things carry into this design:

- **The `restart` action (L3) is a run in the same journal.** A room restart during a restart cycle must end it the
  same way, with "the room restarted during step N" on the chip, and the deploy wait covers it unchanged. A restart
  cycle has a step a clear does not: the runner is exited and not yet relaunched. The journal must name that step,
  so a room that comes back relaunches the card with the wake prompt and does not leave it exited.
- **A compaction during a clear.** On 10-02 at 18:44, review's deferred capture was overtaken by a compaction. The
  card wrote its handoff from the summary, and no clear followed. With L0, atrium acts first by the headroom, so this
  can only happen on a capture that waits mid-turn. In that case the cycle carries on, because the capture token
  still proves the handoff. The chip says "compacted during capture", so the person reading it knows the handoff was
  written from a summary.

### r-new-context-limit-layer: a stored limit per runner

The item is held for the pause, and it belongs to `u-new-context-bar-on-rows`. It wants:

- a limit per runner, stored, readable through the API, and editable in the board's three runner editors;
- a default per runner kind;
- the row's context bar reading the limit from the API.

It becomes one layer of the one function. How the layers combine:

1. **The runner's limit:** the runner row's stored limit, from this item, when it is set. It REPLACES the global
   setting `auto_new_context_k` for that runner's cards. Otherwise the global setting applies, as the default for
   every runner kind.
2. **The ceiling:** a ceiling card (`context_ceiling_k`) takes the LOWER of the ceiling and the limit from step 1
   when the mode also reaches it, and the ceiling alone when it does not. That is what `cardLimit` does today with
   the global k.
3. **The bound:** the result is bounded by `window − margin − headroom`, from the model's window and the runner row's
   margin (L0), taking the lower.

So only the runner row overrides. The ceiling and the bound only take the lower of two.

That function gives four numbers:

- atrium's threshold;
- the runner's flag;
- the card details;
- the denominator of the row's bar.

The bar must read the **threshold**, not the setting. A 200k-window card at k = 200 acts at 147k, so a bar measured
against 200k would show 73% at the moment of the clear. The card details already carry `autocompact.limit_k` and
`window_k`. L0 adds `threshold_k` beside them, and the bar reads it.

The margin and the limit are both runner-row fields, but only the limit is edited by a person. The margin is measured
per runner version, so the editors show it read-only.

Today those numbers come from three places (`cardLimit`, `autoThreshold`'s 70%, `autocompactK`'s 10%), and they
disagree (section 3). A per-role limit is not needed: the action differs by role, and the number does not.
