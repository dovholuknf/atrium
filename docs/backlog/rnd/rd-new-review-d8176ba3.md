# Review: long-turn check-in design d8176ba3

`docs/rnd/long-turn-checkin-design.md` is the only file in its commit, on `74d44a19`. It lands by cherry-pick.

Verdict: **doc-ok.** The mechanism is the right one: a check-in that rides a tool call the worker was already making,
with atrium's measured facts next to the worker's own account. Fold three Mediums in before W1 is built. They are
design points, not reasons to hold the doc.

## The three questions

1. **The reversal is half argued.** The rule at `a2a.go:30-36` gives two reasons.
   - **Tokens and forced turns.** The doc meets this one well. Step 2 costs one blocked call and no extra turn. The
     Stop block is rightly ruled out, and the rate is capped at one check-in per level.
   - **No loop of automatic messages.** This reason is the line "nothing here ever writes to a worker, so no loop of
     automatic messages can exist". The doc does not answer it. That is M1.
2. **The pointers hold** at d8176ba3, with one exception, M2:
   - `a2a.go:30-36` (the rule), `:762-797` (`longTurn`), `:243` (`launcherOf`), `:320` (`holdsNotices`), `:469` (a
     director with outstanding workers) and `:500` (`runnerDelivers`, at 502).
   - `activity.go:200` (`turnAt`), `:461` (the mid-turn edge in `set`) and `:630` (`subagentStarted`).
   - `daemon.go:873-884` (the queued-message step).
   - `changes.go`, which already runs git with `core.fsmonitor=false`, so reusing it keeps that guard.
   - `opencode/atrium.js:317`, the deny throw.
   - `ATRIUM_A2A_LONG_TOOL` is at `a2a.go:68`.
   - The exception is `settings-spine.js:1583`. It points at the right code, but it does not do what the doc says
     (M2).
3. **Nothing quotes clint verbatim**, and nothing is attributed as clint's words.
   - "Off the rails" and "wtf-o-meter" are the name and wording of the ask, used as short phrases. That is fine.
   - Section 9 paraphrases.
   - Nothing pages clint by design. The board code still needs the change in M2 to make that true.

## Mediums

### M1: say why the check-in cannot loop

After this change, an automatic message does reach a worker. So the doc should state the bound that replaces "no
loop can exist":
- Only the meter writes to a worker, and only once per level per card, up to four levels per turn.
- The worker's reply goes up to its launcher and never back down.
- A launcher's `continue`, `cut` or `stop` is the launcher's own act, not an automatic one.
- A check-in to a director (60 minutes) is about the director's own turn. A worker's lines arriving on the director
  do not move the director's meter or start a check-in of their own.

Then update the comment at `a2a.go:30-36` in W2 to say this, so the stated rule and the code agree.

Also say that a card cannot send as `atrium`. The check-in's authority comes from the sender, so a card whose handle
or alias is `atrium` must be refused, or the check-in must be marked by a field no card can set.

### M2: the board's alert rings for this today

Section 4 says the escalation alert at `settings-spine.js:1583` "ignores this source". It does not. `isStuck`
(`stack.js:571`) is true for any `escalation` with `count > 0`, whatever its source. R3's `long-turn` escalation
already rings the bell when the stuck setting is at "alert".

If the meter reuses `Escalation` with a climbing count, it will ring the bell at every level. So:
- W4 gets an item: the stuck alert skips the meter's source, while the mark on the row stays.
- Section 5's acceptance test already says "nothing calls the notification API". Make it cover the stuck setting at
  "alert", which is the case that rings today.

### M3: the 400-line trigger and the half-threshold gate disagree

Section 2 reads the diff "only for cards already past half the threshold", which is 15 minutes for a worker. It also
says over 400 lines starts level 1 "at once, whatever the clock says". The acceptance list then has "500 uncommitted
lines gets one at 5 minutes".

These can't all be true. Either read the diff for every running card on a slower tick, or change the acceptance to
"at 15 minutes".

## Lows

- **L1: `activity.go:200` says "a restart is a new turn anyway".** Section 2 says a restart wrongly resets the clock.
  Say why the comment is wrong now (a card's process outlives a room restart), so W1 does not read the comment and
  keep the turn clock in memory only.
- **L2: the level-4 tooltip names clint.** "Clint sees it here, and nowhere else" is board copy in a public repo.
  Write "the operator" or "nobody is paged".
- **L3: the facts line attaches to "the worker's reply".** Say how atrium picks that message: the first `atrium_say`
  from the worker to its launcher after delivery. A worker that answers someone else instead then counts as no
  answer.

Atrium-Verdict: doc-ok 74d44a19..d8176ba3
Quality: a well-aimed design. It starts from the real case, keeps clint out of the loop, and its false-positive table
covers most of what will go wrong. M1 to M3 make the reversal and the "never page" promise hold in the code.

## Re-read: 1c9e1727

Range `d8176ba3..1c9e1727`. One file changed: the design doc.

Closed:
- **M1.** The no-loop bound is stated: only the meter's timer starts a check-in, and every count is capped. The
  `atrium` sender is stamped by the daemon. That checks out: `atrium_say` has no sender field. The mark is a column on
  the message, not its text. The rewrite of the a2a.go rule comment is in W2.
- **M2.** The meter is its own field, not an `Escalation`, so `isStuck` never sees it. R3 is not raised for a card
  with a meter. W4 tests that a card at level 4 never rings the bell with the stuck setting at "alert".
- **M3.** Diff reads start 5 minutes into a turn, and the acceptance test matches.
- **L1.** The doc now says a daemon restart is not a new turn, and the saved start is tied to the session.
- **L2.** The level-4 tooltip now says "the operator".
- **L3.** The facts line is a separate notice, and the worker's message is not edited.

Open. None of these holds the doc:
- **N1: the W1 row still says "uncommitted lines for cards past half the threshold".** Section 2 now says the reads
  start at 5 minutes, so fix the W1 row to match.
- **N2: "at most 4 per card per turn" counts between answers.** An answer resets the level, so a turn with answers can
  get more check-ins than 4. The bound is still right, because each reset needs the launcher to act. Write it as "at
  most 4 between two answers".
- **N3: the model sees a name, not the column.** The worker reads the banner's text, and the check-in mark is not in
  it. Reserve `atrium` as a handle and an alias, so no card's message is shown to a model as "atrium".

Verdict: doc-ok (re-read, 74d44a19..1c9e1727)
Quality: a clean fold-in. Each fix lands in the right section, and the W4 test pins the never-page promise.

Atrium-Verdict: doc-ok 74d44a19..1c9e1727
