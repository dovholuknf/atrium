# Keep-alive and restart policy: only what a human is using (backlog-2 items 38 and 39)

Status: DESIGN ONLY, nothing here is built. Written 2026-09-29 by sa39 for @runtime.

Mercurius session: `s_qO84hqX9gsqk`. Verdict: NONE YET. Round 1 was started on this draft and had not finished when
the launcher asked for a wrap-up, so nothing in it has been reviewed. Treat the design as an unreviewed first draft.

## Status block

- **Rounds run:** round 1, started 2026-09-29 12:13Z, still running at commit time. No findings collected, no round
  notes recorded, no second round started. To continue: collect round 1 on the session above, triage, fix, repeat.
- **The brief's six questions:** all six have an answer in this draft. 1 (what counts as human use, where the signal
  comes from, stored) is section 1. 2 (the whole rule, what replaces dollars) is section 2. 3 (existing cards) is
  section 3. 4 (parking) is section 4. 5 (the say to a parked card) is section 5. 6 (tests) is section 6. None has
  been through review.
- **Facts the draft rests on that were read and not run:** that a card is filed `dead` during wind-down so its
  status at boot is useless for the resume rule (section 4), and that `Launch` stamps tags before
  `keepaliveAtLaunch` runs (section 2). Both are marked for a rig check in the build.
- **Open questions for clint so far:** the five at the end of this file.

This replaces `docs/restart-idle-spec.md` (item 38) and `docs/keepalive-marked-spec.md` (item 39). Those two specs
measured what a resume and a refresh cost, and their numbers still stand. What changed is the question. They asked
whether the saving was worth building. clint's answer on 2026-09-29 is that the point is tokens, and that tokens are
wasted whenever a cache is kept warm for nobody. So the policy below is built on one fact, when a human last used the
card, and everything else follows from it.

## Decisions taken, which this design follows

- Keep-alive is opt-in and off for a new card. A card is warmed only while a human has used it in the last 3 hours.
- `atrium:subagent` cards are never warmed, whatever their switch says.
- There is no age cap beyond that window and no dollar figure anywhere in the rule.
- A restart resumes a card only if it was `running` or `needs-permission`, or has a queued message or a pending
  restart wake. Everything else is parked.

## 1. What counts as the human using a card

### The signal, and why it has to be new

Three things exist and none of them is this.

| Existing | What it knows | Why it is not enough |
| --- | --- | --- |
| `runner.lastTyped` (`supervisor.go`) | the last operator keystroke on this runner | in memory, gone at a restart, and only for a supervised card |
| `turn_seen.seen_at` and `seen_via` (`store/seen.go`) | the operator saw the latest turn | moves only while a turn is unseen, so a card typed into ten times keeps its first stamp. It also counts `viewed`, a window merely in front |
| `usage.promptCause` (`usage.go`) | whether a prompt was a peer, a wake or the operator | a per-turn attribution for the usage record, not a per-card fact |

The new fact is one timestamp per card: `human_at`, the last moment a person did something to it, and `human_via`,
what they did. It is stamped by one function, `d.humanTouch(taskID, via)`, called from the places below and nowhere
else. Every one of them already has to tell a person from a machine for another reason, so each rule below reuses
that decision instead of inventing a second one.

### What counts (via)

| via | Where it comes from | How a machine is kept out |
| --- | --- | --- |
| `typed` | a keystroke on the card's own attach socket (`attach.go`, the `in` frame, not `?kind=shell`) | only keys count. `noteOperatorTyped` already discards focus, blur, click and query replies (`typedline.go`), so it must return whether the frame was a key. Today it returns nothing and `seenTyped` runs on every frame |
| `prompt` | the `prompt` activity event (`activity.go:766`) when `promptCause` says operator | `promptWasPeer` already holds a 30 second window after atrium types a peer's say or a restart wake, and a wake or exit banner is labelled. This is the path for a window-mode card whose person types in their own terminal, where no attach exists |
| `permission` | `POST /v1/permissions/{id}/decide` and `RoomDecide`, when the stored `decided_by` is `you` (`store.DecidedBySelf`) | a rule, auto mode, and a message block each write their own `decided_by` (`DecidedByMessage`, the rule and auto names), so they never match |
| `message` | `POST /v1/tasks/{id}/message` with no `from` (`handleMessage`) | a session's say always carries `from`, so a peer's say never matches. A raw script posting with no `from` does match, which is the known false positive below |
| `action` | running a stored action on a card (`actions.go`) | a click on the board. The action's text is typed as a prompt too, and the `prompt` rule ignores it, so it counts once |
| `enable` | the card's keep-alive switch turned on by hand (`keepaliveSet`) | see "Enabling counts" |
| `resume` | a human waking a parked card (section 4) | the same click or key that resumes it |

### What does not count

- **A peer's `atrium_say` or `atrium tell`.** Carries `from`.
- **A restart wake, the exit banner, a queued message an agent left.** Labelled, and inside the peer window.
- **An agent's tool calls, the card's own turns and its subagents.** The card working is the opposite of a person
  using it.
- **A keep-alive fork.** It is a separate process and writes no prompt event on the card.
- **A `viewed` mark** (`POST /v1/tasks/{id}/seen`, a board window showing the terminal). See question 1: a window
  left open on a second monitor for a day would keep the card warm for a day, which is the waste this exists to end.
- **`PATCH /v1/tasks/{id}`.** Titles, tags, ordering and status moves are what `atrium_task` and `atrium_alias` do
  for an orchestrator through the same route, and the route cannot tell a script from a person. A person who is
  really using the card types into it, and that counts.
- **A guest's keystrokes are counted** (a lent session is somebody at the terminal), and a shell attach is not, since
  a shell is not the card.

### The known false positive

A script that posts to `/v1/tasks/{id}/message` with no `from` is indistinguishable from the board's composer.
`link/control_mcp.go` sets `X-Atrium-Agent`, so an agent using the MCP is already a session with a `from`. What is
left is a hand-written script. The damage is bounded twice: only an opted-in card is warmed at all, and each false
touch buys at most 3 hours. A stricter test (a board header the daemon could mint) would need an auth idea atrium
deliberately does not have. Not worth it.

### Stored, not in memory

Stored, as a column, and this is the one place the design goes against the grain of `activity.go`.

Activity is in memory because what a runner is doing is false the moment the daemon restarts. This is the other
kind of fact, the kind `turn_seen` is stored for: it was true when written and stays true. Held in memory it would be
empty after every restart, and then a card the person used twenty minutes before a deploy would look untouched, be
skipped by keep-alive, and go cold. Restarts are frequent (two in 8.6 hours in the item 37 window), so the rule would
misfire exactly when it matters.

Writes are cheap by construction. `humanTouch` keeps `lastPersist[taskID]` in memory and returns after one map lookup
if the last write was under a minute ago. Otherwise it writes off the calling path, as `seenTyped` does. One write per
card per minute of use is nothing against a 3 hour window. Failures are logged and swallowed, the hook posture: a
keystroke must never fail because a timestamp could not be saved. It does NOT publish the card. The board does not
draw `human_at` on the face (it is in the details), so a keystroke must not cost an SSE frame.

Shape: migration at the END of `schema.go`'s slice (next free number at build time, `0070_human_at` today), two
tolerant statements, the way `0068_owed_at` did it:

```sql
ALTER TABLE task ADD COLUMN human_at  TEXT NOT NULL DEFAULT ''
ALTER TABLE task ADD COLUMN human_via TEXT NOT NULL DEFAULT ''
```

Empty means never, and there is no backfill: a card never touched since this ships is exactly a card that has not
been shown to be in use. `Task` gains `HumanAt *time.Time` on the pattern of `PromptedAt`, and `taskColumns` and its
scan gain the two columns. The same migration adds `parked_at` (section 4), so a build pays for one migration.

### Enabling counts

Turning the switch on stamps `human_at` with `via=enable`. Otherwise a person who ticks the box on a card they have
not touched since yesterday would see it refuse with "you have not used this card", which reads as broken. They just
looked at the card and made a choice about it, and that is use.

### Other consumers, named and not built

clint suspects this helps subagents. It does, and these are the places, all of them a one-line read of `human_at`:

1. **`Cull` and `worktreegone.go`.** A worker a human has typed into since it went idle has been adopted. Culling it
   deletes a worktree somebody is now working in. Both should refuse, with the touch time in the answer.
2. **`atrium_peers`.** An orchestrator choosing whether to say something to a worker could see "clint typed in it 4
   minutes ago", a durable and coarser cousin of the live typing gate.
3. **Parking (section 4).** The restart rule could prefer a recent human touch when the column alone is ambiguous.
   It does not need to today.
4. **The sweep.** `sweep.go` archives on age. Age since a human touched is a better clock than age since the last
   event for a card an agent keeps poking.

None of them is part of this change. They are listed so the column is named for what it is, a fact about the card,
not a keep-alive setting.

## 2. The keep-alive rule, as a whole

`decide` in `keepalive.go` keeps its shape and its early-out order. Two new rules go in, one old rule comes out.

```
1. no switch, or state != on            skip ""
2. NEW  tagged atrium:subagent          skip "atrium:subagent worker"
3. not a Claude card                    skip
4. no session id or directory           skip
5. NEW  human_at empty                  skip "you have not used this card"
   NEW  now - human_at >= 3h            skip "idle 3h since you last used it"
6. status != needs-input                skip
7. a permission dialog is open          skip
   ... transcript, model, fast, 1h cache, context, local hooks, cold, not due: unchanged ...
8. refresh
   REMOVED  the dollar budget and the "break-even" verdict
```

**Rule 2 goes before the harness check and before anything that reads a file,** and is checked in three places, since
one place is a rule someone can walk around: `decide` (belt), `keepaliveAtLaunch` (a subagent card never gets an
`on` row, even when the gear default is on) and `keepaliveSet` (turning it on answers an error naming the tag). The
tag is `SubagentTag` in `cull.go`. Check at build time that `Launch` stamps the tags before `keepaliveAtLaunch` runs.

**Rule 5 sits ahead of the transcript read on purpose.** `decide` runs for every switched card every minute, and
reading the last 2 MB of a transcript is its dominant cost. With most cards cold, this cuts the work for the rest.

**The window is a constant, `keepaliveHumanWindow = 3 * time.Hour`, with no setting.** A setting is one more thing to
explain, clint asked for a number, and a constant is one edit if it is wrong.

### Why 3 hours

The 1 hour cache is refreshed five minutes before it expires, so a refresh lands about every 55 minutes. What the
window changes is how many refreshes one touch can buy:

| Window | Refreshes after the last touch | Cache goes cold at |
| --- | --- | --- |
| 2h | 2 (about 0:55, 1:50) | 2:45 |
| 3h | 3 (0:55, 1:50, 2:45) | 3:45 |
| 4h | 4 (0:55, 1:50, 2:45, 3:40) | 4:35 |

A refresh reads the whole context, and a full rewrite costs about 40 times a read on Opus 5.5 (`keepalivePrices`:
1h write 8 against read 0.20 per million). So one more warm hour is worth buying if the chance the person comes back
in that hour is better than about 1 in 40. Item 37's data has cold returns after gaps from 1h17m to 4h19m, so people
do come back across the whole range, and the hours near the front carry the most of that. 3 hours is the middle of
clint's range, covers a meeting and a long lunch, and caps the cost of a touch at three reads. It is short enough
that a card left overnight costs nothing after a person's last evening use.

### What replaces the dollar budget

The time window, alone. It is a hard ceiling on cost per touch (at most 3 refreshes) with no price in it, so it
does not move when `keepalivePrices` does, and it does not need sa37b's hidden figures. A token count is NOT needed
and is not added: 3 refreshes of one context is bounded by the context, and the ledger already records each
refresh's tokens if a cap is ever wanted.

What stays, because none of it is priced in dollars:

- The receipt classification (`acted`, `refused`, `failed`, `warmed`, `miss`) and its stops. A `miss` reads under
  90 percent of the context from cache, a fact about tokens. `stopped:miss`, `stopped:failing` and
  `stopped:acted` stay, and so does the room suspension after two misses on two cards.
- `keepaliveMinContext` (50k) and the model and 1h checks.

What goes: `budgetFor`, `keepaliveBudgetFraction`, `spent+next > budget`, the `break-even` verdict, and
`store.KeepaliveBreakEven` as a state a card can enter (the constant stays so old rows still parse, see section 3).
`Cost`, `SpentBefore` and `Budget` on `keepalive_refresh` keep being written as history and `decide` never reads
them. `keepaliveCardView.Spent` and `Budget` are dropped from what the board draws, which is sa37b's part too, and
the tooltip says how long is left: "warming until 14:20, 1h50 after you last used it".

The window is a SKIP, never a stored state. A card outside the window stays `on`, shows why, and starts warming
again when the person touches it. Storing a `stopped:` state would make the person re-enable a card each time they
came back, and the whole point is that they do not have to.

### A consequence to know about: the 1h pin is set at launch

`keepaliveAtLaunch` pins `CLAUDE_CODE_PROMPT_CACHE_TTL=1h` only when the card's switch is not off. With the default
now off, a card launched off runs on whatever TTL Claude Code picks, and `decide` skips it as "not on the 1h cache"
after it is switched on. It will warm after its next restart or resume. The details should say so ("this session
started on the 5 minute cache, so it can be warmed after its next restart") rather than the bare skip.

The alternative, pinning 1h on every card, makes every write of every card cost more, to serve a switch most cards
never have on. See question 3.

## 3. Cards whose switch is on today

**Recommendation: they become off, once, in the migration.** Item 23 turned the switch on for every Claude card by
default. Those `on` rows are the default speaking, not a choice, and opt-in means the person picks the cards. There
is no way to tell a deliberate `on` from a defaulted one, since `state_at` moves for both.

```sql
UPDATE keepalive_card SET state = 'off', state_at = <now>
  WHERE state IN ('on', 'stopped:break-even')
```

in the same migration that adds the columns, so it runs exactly once (a recorded migration never reruns). It leaves
`stopped:miss`, `stopped:failing` and `stopped:acted` alone, since those say the mechanism looked wrong for that card.
The board says so once on first load: "Keep-alive is now opt-in. It was on for N cards and is off. Turn it on for the
cards you want warm." The count comes from the migration, held for the first `/v1/health`, or simply a toast text the
board builds when it sees `cache_keepalive_default` change. Nothing is deleted, and the ledger of past refreshes
stays.

The default flips too. `KeepaliveDefaultOn` returns true for unset and for any read failure today, "the default clint
asked for". It becomes: only an explicit `on` is on, and a read failure answers off. The gear keeps the choice, now
off unless set. A person who wants item 23's behaviour back turns it on there, and the rule still bounds it by their
own use, so it costs nothing extra for cards they never touch.

**The alternative, keep them on and let the rule bind them,** costs no more tokens than opt-in does, because a card
with no human touch is skipped either way. It is rejected only because it is not what was decided: 21 boxes ticked
that nobody ticked. If clint would rather not re-tick the ones he uses, this is the fallback, and it is a one-line
change to the migration (drop the `UPDATE`). Question 2.

## 4. Parking at a restart (item 38)

### The one hazard, found by reading the shutdown path

**By the time the next daemon starts, no card's status says what it was doing.** `stopSupervised` winds each runner
down, and each runner's `awaitExit` then files its card as `dead` (`supervisor.go:2131`, unless shelved or done).
`saveReopen` runs after that, and stores card ids only. `reopenWanted` filters on shelved and throwaway and nothing
else, so it never needed the status. A rule of "resume if `running` or `needs-permission`" evaluated at boot would
read `dead` for everything. **The decision must be made before the wind-down and carried across the restart.** Verify
this on a rig first in the build, it is the design's one factual dependency on code not re-run here.

### Where

1. **Snapshot, in `stopSupervised`, before `windDown`.** For each live runner read its card and decide:

   ```
   resume  if status is running or needs-permission
           or the card has a queued message (store.PendingMessages > 0)
           or the card has a pending restart wake (d.wakes.get(id) != nil)
   park    otherwise
   ```

   `done` with a live runner (the item 83 worker awaiting review) is parked like any idle card.
2. **Record it.** `reopen.json` keeps `cards` exactly as now, and gains `parked: [{id, status}]`, the cards that were
   open and are not to be resumed, with the status they had. A file with no `parked` key (written by an older daemon)
   means resume everything, so a mixed-version restart behaves as it does today.
3. **At boot, `reopenWanted` splits its list.** Resume ones go on as now: launched in order, `settle.expect(ids)`
   naming only them, so the settle window is shorter by every parked card. Parked ones are not launched, are not in
   `settle`, and get `parkCard(t, status)`: put the recorded status back if the card is `dead` now (the wind-down
   filed it), stamp `parked_at`, publish once. Fixtures are unchanged and still start first: a fixture is a standing
   decision that a terminal exists.
4. **Nothing changes for a crash.** With no snapshot written there is no `parked` list and nothing reopens, exactly
   as today.

### What "parked" is on the card

A flag, not a status: `parked_at TEXT NOT NULL DEFAULT ''` on `task`, added by the same migration as `human_at`.
Set at boot by `parkCard`, cleared by `unpark`. It survives a second restart on its own: a parked card had no runner at
the second shutdown, so it is not in `cards` and is left exactly as it is.

**History:** one existing-kind event with a payload, `status-changed` with `{"parked": true, "was": <status>}`, and
`{"parked": false, "by": <via>}` when it wakes. NOT a new event kind: `store/CLAUDE.md` warns that widening the
event `CHECK` is a rebuild of the largest table.

The card stays in its column, and `sessionGone` (item 83) must not read it as gone. Its test is "done or dead with
nothing live". A parked `needs-input` card fails the first half and is not gone. A parked `done` card would pass it,
so `sessionGone` returns false for a card with `parked_at` set. This is the fix that makes parked answer differently
from undeliverable, and it must go with the flag.

### How the board shows it

The column does not change. The card wears a small "parked" mark, styled with the idle one, with a tooltip:
"Parked at the restart: no process is running, and it resumes when you press a key in it or click Resume." A Resume
entry in the card menu. The details show `parked_at` and `human_at`. No toast, no arrival alert, and no chime.

### What resumes it

One function, `d.unpark(taskID, via)`, holding the card's launch lock (`launching.lock`, as `RestartRunner` does). It
builds the launch request from the card exactly as `reopenSaved` does, which becomes a shared `reopenRequest(t)`,
launches, clears `parked_at`, appends the event and stamps `humanTouch` if the cause was a human's. Everything that
resumes goes through it.

| Trigger | Resumes? | Notes |
| --- | --- | --- |
| The first keystroke in the card's attach | yes, `via=resume` | the terminal shows the saved scrollback and a line "parked: press any key to resume", and the frame that resumes is held until the runner is up |
| Resume in the card menu | yes | a click |
| A board message with no `from` | yes, then delivered | the person is at the keyboard, so this is the same as a key |
| Running an action | yes, then the text | as above |
| A restart wake | never needed | a card with a pending wake is not parked |
| A peer's say | only on confirmation | section 5 |
| Merely opening the card's terminal | **no** | see below |
| A keep-alive refresh | no | it forks from the transcript and never needs a process |

**Attaching does not resume.** The board attaches when a terminal is opened, and a browser tab restoring 20 open
terminals would resume all 20, undoing the park in one page load. Resuming on the first KEY is what "a person is
using it" means, and the key is also the human touch. The cost of the resume itself is no tokens (item 38 spec: a
resumed process spends nothing until its next turn), so the only price of a late resume is a few seconds of
process start, which the held frame and the line above cover.

### What parking saves, honestly

Not tokens. The item 38 spec measured that: an idle resumed card that is never typed into spends nothing. It saves a
process and its memory per idle card (about 21 of them), the length of the settle window, and the chance of a resume
failing and filing a card as `dead` that nobody was using. clint's answer to the spec's question 1 was to build it
anyway, and those are the reasons that hold.

## 5. A say to a parked card

**Recommendation: answer `parked` first and resume only when the sender confirms, for a peer. Resume straight away,
with no confirmation, for the operator.**

### Why a peer's say does not resume on its own

Resuming costs nothing. What the say does next is not free: the resumed session reads the message and runs a turn,
and a parked card has by definition had no human touch and no keep-alive, so its cache is very probably cold. That
turn writes the whole context again. Item 38's table has first turns after a cold gap writing 150k to 270k tokens,
on a card nobody looked at. The sender is a model that does not see that cost and will not weigh it.

Resuming straight away is the simpler design and it has two failure modes that confirmation does not:

- **A loop or a sweep.** An orchestrator saying "status?" to every worker after a restart wakes every parked worker
  and pays every cold turn, to hear that most had nothing to say. `peerLimit` caps the rate and not the breadth.
- **A silent cost.** The sender's `delivered` answer would read like any other, and the cost would surface days later
  in item 37's by-cause totals as a `say` line.

Confirmation puts the cost in the sender's context at the moment of the choice, and the second say is one more call.

### The flow

```
say to X            -> delivered: "parked", reachable: "parked", nothing queued, nothing resumed
                       warning: "X is parked (idle since 09:12, no process). Its cache is probably cold, so waking it
                       runs a turn on a full context. Say it again with wake=true to resume it and deliver this."
say to X, wake=true -> the card resumes (unpark, via=say), then the text is queued for it exactly as for a card
                       that is starting, and delivered by the ordinary path
```

`wake=true` is a new optional field on the message body and on `/tell`, and on the `atrium_say` tool and `atrium
tell --wake`. It is meaningless for a card that is not parked and is ignored there.

**Nothing is queued on the first answer.** This copies item 83's rule for `undeliverable`: not queued as well, or a
resumed session would get it twice. The sender says it again, and the second say is the only one that lands.

**The delivery after a wake follows the peer bus rule and adds nothing to it.** `peers.go` says a peer's message is
queued and delivered by a hook, and never typed into a terminal. The wake only creates a runner. The text is
enqueued through the same message path a card mid-start uses, so a peer's words are still delivered by the existing
gate and hooks and never by a new typing path. Do NOT reuse `unpark` as a place to type the text, which is the
shortcut `peers.go` warns about.

### Where it fits with item 83

`undeliverable` means "resume it first, then say it again", and it is for a card with no session that only the sender's
own hands can bring back. `parked` means the same words with a different last step: the room can bring it back, at a
cost, if asked. They are two answers, not one. The check for `parked` goes BEFORE `sessionGone` in both places that
call it (`handleMessage` at `messages.go:452` and the target check at `peers.go:291`), through one shared `d.sayGate(t)`
so they cannot drift. In the say log (`saylog.go`) the refusal is recorded as `SayRefused` with the note "parked", so
the sender's earlier say is visible next to the later one. The `atrium_peers` listing marks a parked card so a
sender sees it before trying.

### The operator's say

A person typing in the board's composer (a message with no `from`) resumes at once. There is a human at the keyboard,
they have just chosen this card, and asking them to confirm is a second click for no information. It stamps
`human_at`, which puts the card back in the keep-alive window.

## 6. Testing

### Unit tests

In `internal/daemon` unless a store test is named. Each is small and uses the fixtures the neighbouring test files
already have (`keepalive_test.go`'s fake fork and clock, `reopen_test.go`'s `saveReopen`).

**Signal**

- `TestHumanTouchKeyCounts`: an `in` frame with a key stamps `human_at` and `human_via=typed`.
- `TestHumanTouchReportsDoNotCount`: a focus (`ESC [ I`), a click report and a device reply stamp nothing.
- `TestHumanTouchShellAttachIgnored`: `?kind=shell` stamps nothing.
- `TestHumanTouchPeerPromptIgnored`: a `prompt` event inside `peerPromptWindow` of a peer's typed say stamps nothing.
  Twin: the same event with no peer stamp counts, and one after the window counts.
- `TestHumanTouchWakeIgnored`: a restart wake's prompt stamps nothing.
- `TestHumanTouchPermissionByHuman`: a decide with `by=you` stamps, and decides by a rule, auto mode and the message
  block do not.
- `TestHumanTouchMessageFrom`: a message with `from` stamps nothing, and one without does.
- `TestHumanTouchThrottled`: 100 keys in a second cause one store write, and one after a minute causes a second.
- `TestHumanTouchSurvivesRestart` (store): stamp, reopen the database, and the value is there.
- `TestHumanTouchStoreFailureSwallowed`: a failing store logs and the key is still delivered.
- `TestHumanTouchDoesNotPublish`: no SSE frame per touch.
- `TestEnableStampsHumanTouch`: switching keep-alive on stamps `human_at` with `via=enable`.

**The rule** (`keepalive_test.go`, reusing its rig)

- `TestDecideNeverUsedIsSkipped`: switch on, `human_at` empty, skip "you have not used this card".
- `TestDecideInsideWindowRefreshes`: `human_at` 2h59m ago, due, refreshes.
- `TestDecideAtWindowEdgeSkips`: `human_at` exactly 3h ago skips, one second inside refreshes.
- `TestDecideSubagentNeverRefreshes`: a subagent-tagged card with switch on and a fresh touch is skipped.
- `TestSubagentNeverGetsSwitchAtLaunch`: with the gear default on, `keepaliveAtLaunch` gives it `off`.
- `TestSubagentSwitchRefused`: `keepaliveSet(on)` answers an error naming the tag.
- `TestNoDollarInDecide`: a card with `SpentBefore` far over any old budget still refreshes inside the window
  (it fails today, which is the regression the design fixes).
- `TestWindowSkipDoesNotStore`: a skip for the window writes no state, and a touch later refreshes with no re-enable.
- `TestWindowEarlierThanTranscriptRead`: the window skip happens with no transcript on disk.
- `TestThreeRefreshesPerTouch`: run the clock across 5 hours from one touch, exactly 3 forks.
- `TestMissStillStops` and `TestFailingStillStops`: the receipt stops are unchanged.
- `TestDefaultOffFlipped` (store): unset is off, a read failure is off, an explicit `on` is on.
- `TestNewCardIsOffByDefault`: a launched Claude card has an `off` row.

**Migration** (`store`)

- `TestMigrationTurnsDefaultedOnOff`: rows `on` and `stopped:break-even` become `off`, `stopped:miss` and the others
  do not, and a second open changes nothing.
- `TestMigrationTolerant`: run twice on a database that already has the columns.
- Verified by hand against a COPY of the live database, counting the cards changed, then deleting the copy.

**Parking**

- `TestSnapshotBeforeWindDown`: `stopSupervised` records status from before the runners exit, and the file still
  says `running` for a card `awaitExit` then filed dead. This one proves the hazard in section 4.
- `TestParkRule`: table over status (`running`, `needs-permission`, `needs-input`, `done`), queued message, pending
  wake. Resume for the first, second and any with a message or wake, park for the rest.
- `TestReopenNoParkedKeyResumesAll`: an old file resumes everything.
- `TestReopenSkipsParked`: a parked card is not launched, is not in `settle.expect`, has its status restored and
  `parked_at` set, and an event is written.
- `TestSettleShorterByParked`: the settle window closes when the resumed set is back.
- `TestParkedSurvivesSecondRestart`: park, stop, start, still parked, still no launch.
- `TestFixtureStillStartsWhenIdle`.
- `TestSessionGoneFalseWhenParked`: a parked `done` card is not gone.
- `TestUnparkOnKey`: the first key resumes and is not lost, attach alone does not.
- `TestUnparkIsIdempotent`: two triggers at once launch one runner (the launch lock).
- `TestUnparkClearsFlagAndStamps`.

**Say**

- `TestSayToParkedAnswersParked`: `delivered: "parked"`, nothing queued, `SayRefused` in the log.
- `TestSayToParkedWithWakeResumesThenQueues`: unparks and enqueues, once.
- `TestSayToParkedNotTypedByPeer`: the wake path never calls the typing function.
- `TestOperatorSayResumesImmediately`: no `from`, resumes, delivers, stamps.
- `TestTellToParked` and `TestPeersMarksParked`.
- `TestWakeIgnoredWhenNotParked`.
- `TestParkedPrecedesGone`: order of the two checks, both callers.

### Test plan scenarios, for the builder to add under the next free letter

1. Switch keep-alive on for a card you used a minute ago. It warms at the next due time, and the tooltip names the
   time the window ends.
2. Leave it. After 3 hours from your last key it stops being refreshed and says "idle 3h since you last used it". Type
   one key, and it warms again with no re-enable.
3. A card you have never touched, switch on. It shows "you have not used this card" and no refresh appears.
4. A card tagged `atrium:subagent`. Its switch is greyed or refused, and it is never refreshed.
5. Have a peer say something to a card. Its window does not move (read `human_at` in the details before and after).
6. Type a line, approve a permission, run an action. Each moves it.
7. Upgrade with three cards on. All three are off, the board says so once, and the ledger of past refreshes is intact.
8. Restart the room with one running card, one idle card, one idle card with a queued message and one with a wake.
   The first, third and fourth resume, and the idle one comes up parked with the mark and no process.
9. Open the parked card's terminal. It shows the scrollback and the hint and stays parked. Press a key and it resumes.
10. `atrium_say` to the parked card. It answers `parked` and nothing else happens. Say it again with `wake=true` and it
    resumes and receives it.
11. Restart the room again with the card still parked. It stays parked and is not launched.
12. Kill the daemon uncleanly. It comes back as it does today, with nothing parked.

## 7. Parking a card that has gone idle (r-007)

Added 2026-09-29 by @runtime. clint: "do we need to keep directors online all the time? they should shut down after a
couple hours if they are not working." Unreviewed, like the rest of this draft.

Section 4 parks an idle card only at a restart. This parks it on a clock, with no restart, by the same mechanism, so
a card parked either way is one kind of thing: `parked_at` set, status kept, resume id kept, no process, and woken by
the triggers in section 4's table and the say rules in section 5.

### When a card is parked

The reaper tick (`reaper.go`, the same one that runs `reapGoneWorktrees`) asks, for every card in the supervisor's
`runners` map:

```
park  if  idle_park_after is on
      and the card is subject (see "Who is subject")
      and its status is needs-input or done         (not running, not needs-permission)
      and it has no pending permission request
      and it has no open question from a report (status question or blocked, not yet answered)
      and it has no queued message and no pending restart wake
      and none of its own workers has a live runner (below)
      and it has been idle for idle_park_after
```

**Idle since** is the latest of: the card's last turn end, its last prompt, and `human_at` (section 1). A turn atrium
started itself for the handoff (below) does not move it, or the handoff would reset the clock it exists for.

**Its own workers** are the cards whose `work_item.launcher_id` is this card (`store/ledger.go`). Any one of them with a
live runner, in any status, keeps the launcher up. That includes a worker that reported `done` and sits at its prompt
awaiting review (item 83). Such a worker is itself subject and parks on the same clock, and then its launcher is free
to park. So a director and its idle workers wind down together, workers first, and none is parked while a worker it is
waiting on is still working.

**The setting.** `idle_park_after`, in seconds, daemon-wide, in the gear beside keep-alive. Default 7200 (2 hours).
`off` disables the whole section. A floor of 30 minutes, so a mistyped value cannot park a card between two of its
own turns.

### Who is subject

- **Agent cards are subject.** `origin:agent` is on every card an agent launched, directors and subagents alike.
- **clint's own cards are exempt unless he opts in.** A card with no `origin:agent` (the board's launch dialog, a
  joined session, a fixture) is never parked on the clock. It opts in with the tag `atrium:park-idle`, set from the
  card menu. The restart rule in section 4 still applies to it, as today.
- **The orchestrator card is one of clint's.** `atrium-87300` has no `origin:agent` (its tags are `orchestrators`),
  so it is exempt by this rule until clint tags it `atrium:park-idle`. That is the opt-in, and question 7 asks him.
- **Pinned is not exempt by itself.** A pin is about where a card sits on the board, not about keeping a process up.
- **Never:** a fixture, a shell, a card with a lent session in use, a throwaway card.

### The handoff, first, for a director

A card tagged `atrium:director`, or opted in with `atrium:park-idle`, writes its handoff before it is parked. A
subagent does not: its brief and its branch are its state, and it has reported.

It reuses item 66's capture step (`newcontext.go`): the capture prompt, `handoffWritten`, and its timeout. Parking
waits for the capture to finish. A capture that times out parks anyway and records that on the card, because a card
that cannot write its handoff is still using a process nobody is using.

**When to take it: before the cache goes cold.** A capture turn run at the 2 hour mark reads a context whose 1 hour
cache has expired, so it writes the whole context again, 150k to 270k tokens by item 38's table, for every director,
every time. The same turn run at 50 minutes of idle reads the cache warm, for a few thousand tokens. So the capture
runs at `idle_park_after` minus 70 minutes, or at 50 minutes idle, whichever is later, and the park follows at
`idle_park_after` with no second turn. If the card does anything in between, its idle clock restarts and so does
this. This is question 6.

**It depends on item 91.** The orchestrator and @merge share one directory, and today's fixed name `HANDOFF.md` means
one card's capture overwrites the other's, and `handoffWritten` can pass on the other card's write. A clock that takes
handoffs unattended makes that likely rather than rare. Item 91's option 1 (a per-card file name) has to land first.

### Parking it

The same three steps as section 4, run on one card instead of at a shutdown:

1. Snapshot the status, before the wind-down (section 4's hazard: `awaitExit` files the card `dead` otherwise).
2. Wind the runner down with its exit keys (`windDown`, as the worktree-gone reaper does). Claude records
   `prompt_input_exit`.
3. `parkCard`: restore the snapshot status, set `parked_at`, and write one `status-changed` event with
   `{"parked": true, "was": <status>, "by": "idle", "idle_for": <seconds>}`. No toast and no chime, the parked mark
   on the card, as in section 4.

### Waking it

Section 5's rules, unchanged, with one addition for reports:

- **A say from clint** (a board message with no `from`, a key in its terminal, Resume, an action) resumes it at once.
- **A peer's say** gets section 5's `parked` answer, nothing queued, and needs `wake=true`.
- **A report owed to it** (`atrium_report` from one of its own workers) resumes it at once, as if `wake=true` were
  set. A report is an answer the launcher asked for. Refusing it would strand the worker, which cannot report again,
  and it is the case this design most often meets, since a director is parked only after its workers are. This is
  question 8.

**A director comes back with its HANDOFF.md read.** `unpark` resumes the conversation by its resume id, the way
section 4 does, so the session has its own history. Before the waking message is delivered, `unpark` queues item 66's
wake prompt for any card that took a handoff: "You were parked after N hours idle. Read HANDOFF.md, then act on what
follows." The waking message follows it through the ordinary queued path, never typed (the peer bus rule, section
5). If the resume id no longer resumes, the fresh-start fallback `spawnPTYResume` already has starts a new
conversation, and the wake prompt is what makes that survivable. The orchestrator card gets the same treatment once
it is opted in.

### The silent-stop notice for a resident director

Raised by the orchestrator 2026-09-29: "ended its turn without reporting" reaches it several times an hour for
directors that are idle by design, waiting on their workers or on clint.

**Why it rings.** A director is agent-launched (the orchestrator launched it), so `silentStop` and the board's STUCK
mark both apply (`a2a.go`, `stoppedSilently`). Item 41 made a resident owe its launcher a report for every prompt,
from anybody. So when a worker's report wakes the director and the director merges, relaunches or simply waits and
ends its turn, the turn "ran since the last prompt and said nothing to the launcher", and the orchestrator is told.
That is right for a worker and wrong for a director whose next report is due when its batch is done, not after every
worker message.

**Two options:**

1. Skip the notice, and the STUCK mark, for every `atrium:director` card. Simplest. The cost: a director that really
   stalls on something the orchestrator asked for is never flagged, and it is the orchestrator's only signal for that.
2. **Skip it while the director has outstanding workers.** A worker is outstanding while it has a live runner or is
   parked (`parked_at` set), whatever its status. That includes a `done` worker at its prompt awaiting review or
   merge. A culled or dead worker is not outstanding. Once every worker has ended, a director that still has not
   reported gets the one notice, which is exactly the case worth hearing about: its batch is done and it said nothing.

**Recommendation: option 2**, in `stoppedSilently` itself, so the notice and the STUCK mark keep one definition of
owing (the reason that function's comment gives). Counting a parked worker as outstanding matters with idle parking:
otherwise every worker parking at the 2 hour mark would end the suppression and ring its director once, which is the
same noise on a slower clock.

Two rules go with it, whichever option is picked:

- **A parked card is never silently stopped.** It has no process and its status is kept, so a parked `needs-input`
  director would otherwise read STUCK on every watchdog tick. `stoppedSilently` returns false for `parked_at` set.
- **A report still clears the debt as today.** A director that reports "waiting on clint" owes nothing afterwards,
  so a wait on clint that was reported never rings. Only an unreported wait does, and under option 2 only once its
  workers have ended.

BUILT (r-007 stages 1 and 3): option 2 in `stoppedSilently`, counting live runners and parked workers
(`workerOutstanding` in `a2a.go`), and a parked card is never silently stopped.

BUILT (r-007 stage 3), sections 1, 4 and 5 minus the restart-time snapshot: the human touch stamping, `parkCard`,
`unpark`, the say gate and `wake`, in `internal/daemon/park.go` and `internal/store/park.go`. Tests in `park_test.go`.
BUILT (r-007 stage 4): `internal/daemon/idlepark.go` answers who is subject and holds the orchestrator's own rule, the
tag `atrium:orchestrator` and no other card with a live runner (parked, fixtures and shells do not count). The tick
that asks it is stage 5.

Not built: the keep-alive rule reading `human_at`, the restart snapshot, and the stamps for a plain operator message
(covered by the resume stamp), an action and enabling keep-alive.

Tests: `TestDirectorWithLiveWorkerNotSilent`, `TestDirectorWithParkedWorkerNotSilent`,
`TestDirectorAllWorkersEndedIsSilent` (one notice, on the usual backoff), `TestWorkerSilentStopUnchanged`,
`TestParkedCardNeverSilent`, and `TestStuckMarkMatchesNotice` for each of those.

### Tests, for the builder

- `TestIdleParkRule`: a table over status, pending permission, open question, queued message, pending wake and a live
  worker. Parks only on the all-clear.
- `TestIdleClockIgnoresHandoffTurn`: the capture turn does not move idle-since.
- `TestIdleParkWorkersFirst`: a director with a done worker at its prompt is not parked, the worker parks, then the
  director does.
- `TestIdleParkExemptsOperatorCards`: no `origin:agent` and no `atrium:park-idle`, never parked. With the tag, parked.
- `TestIdleParkSetting`: off parks nothing, under the floor is clamped, the default is 2 hours.
- `TestIdleHandoffBeforeCold`: the capture runs at 50 minutes idle, the park at 2 hours, one capture.
- `TestIdleHandoffTimeoutStillParks`: recorded on the card.
- `TestIdleParkKeepsStatus`: a `done` card is parked as `done`, not `dead`.
- `TestReportWakesParkedLauncher`: a worker's `atrium_report` resumes it and is delivered, with no `wake`.
- `TestUnparkQueuesHandoffWake`: the wake prompt is queued ahead of the message, and neither is typed.

Test plan: leave a director idle past the setting with no workers. Its handoff is written near 50 minutes, it is
parked at 2 hours with the mark, and `atrium_peers` shows it parked. Say to it from a peer: `parked`. Say from the
board: it resumes, reads HANDOFF.md, then answers.

## Order of building

The order is meant to keep every step shippable and to keep the keep-alive rule safe first.

1. The store: the migration (columns, the default flip, the `UPDATE`), `HumanAt`, `ParkedAt`, `TouchHuman`.
2. `humanTouch` and its callers, the `noteOperatorTyped` return value.
3. The rule in `decide`, the subagent guards, the removal of the budget, the view and tooltip.
4. Parking: snapshot, `reopen.json`, `parkCard`, `unpark`, `sessionGone`, the attach behaviour and the board mark.
5. The say: `sayGate`, `wake`, the tool and CLI flag, `atrium_peers`.

Steps 1 to 3 need no restart-path change, and step 4 is what a room restart exercises, so step 4 is the deploy that
needs care. The board half is HUB-SIDE and safe alone, the daemon half needs a room restart.

## Open questions for clint

1. **Should a `viewed` mark (a board window showing the terminal) count as using a card?** Recommend no. It cannot tell
   a person reading from a window left open on another monitor, and a day-long open tab would keep a card warm for a
   day. Typing, approving or acting counts. If reading should count, the cost of the mistake is one card's refreshes.
2. **Turn every card's switch off once when this ships, or leave the `on` ones on under the new rule?** Recommend off:
   those were defaulted, not chosen. The cost is re-ticking the cards you use. The other costs no more tokens.
3. **Pin the 1h cache on every launch, or only when the switch is on?** Recommend only when on, as today. Then a card
   switched on mid-session warms after its next restart, and the details say so. Pinning everything makes every card's
   writes dearer to serve a switch most never have on. This wants a look at the share of writes by TTL in item 37's
   rows before it is settled, which this design did not run.
4. **Is 3 hours right?** It is the middle of your range. 2 hours saves one refresh per touch, 4 hours buys one more.
5. **Should the operator's say to a parked card resume with no confirmation?** Recommend yes. A peer's needs one.

Section 7, idle parking (r-007). Decided by the orchestrator 2026-09-29, without clint: 6 yes, 8 yes, 9 yes. Asked of
clint: 7, and whether to build. 10 is open.

6. **Take a director's handoff at 50 minutes idle, while its cache is warm, and park it at 2 hours?** Recommend yes. A
   handoff taken at the 2 hour mark rewrites each director's whole context on a cold cache, 150k to 270k tokens every
   time. The cost of taking it early is a handoff up to 70 minutes older than the park, which a director that does
   nothing in that time does not notice.
7. **Opt the orchestrator card in?** It is one of your cards by the rule (no `origin:agent`), so it is exempt until you
   tag it `atrium:park-idle`. Recommend yes, since the orchestrator asked for it. Any other card of yours stays up
   unless you tag it.
8. **Should a worker's `atrium_report` wake a parked launcher with no `wake=true`?** Recommend yes. A report is an
   answer the launcher asked for, and a worker refused with `parked` has no second report to send.
9. **Item 91 first?** Parking takes handoffs unattended, and the orchestrator and @merge share one `HANDOFF.md`.
   Recommend building item 91's per-card file name before this section.
10. **The silent-stop notice for directors: skip it for every director, or only while its workers are outstanding?**
    Recommend only while outstanding (live or parked workers). Skipping it always removes the orchestrator's only
    signal that a director finished a batch and said nothing. This part does not need idle parking, and it can be
    built on its own first, since it is the noise reaching the orchestrator today.
