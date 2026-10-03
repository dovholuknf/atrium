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

## Re-read: e373918c

Range `1c9e1727..e373918c`. One file changed: the design doc.

Closed:
- **N1.** The W1 row now reads the diff from 5 minutes into a turn, which matches section 2.
- **N2.** The bound is stated per card, between two answers.
- **N3.** `atrium` is reserved as a handle and an alias, and the reason is given.

One note, not a hold: the reservation is code, so give it a stage. It belongs in W2, beside the daemon-stamped
sender. Aliases may also be set on the hub, so check there too.

Verdict: doc-ok (re-read, 74d44a19..e373918c)
Quality: every note is closed, in the section where it belongs.

Atrium-Verdict: doc-ok 74d44a19..e373918c

## Re-read: a2ea5e98

Range `e373918c..a2ea5e98`, one file, `docs/rnd/long-turn-checkin-design.md`. It adds W6 and section 11, "owed
answers that survive", after the 10-02 evening scm stall.

Pointers, checked at a2ea5e98:
- `silentStop` is `a2a.go:423`, `launcherOf` `:243`, `notifyLauncher` `:277` (once per worker, source and key) and
  `holdNotice` `:333`.
- The ended notice is the `queueNotice(NoticeEnded)` call at `ledger.go:786`.
- SessionStart is in `internal/cli/session.go`, and step 2 is still the queued-message block at
  `daemon.go:873-884`.
- One is off by a few lines: `a2a.go:466` is the parked guard inside `stoppedSilently`, which starts at `:460`.
  Point at `:460`.
- `deliverPeer` is `peers.go:523`. Name the file.

The diagnosis reads right, case by case:
- A worker that asked a question has reported, so it is not silent.
- `notifyLauncher` types its notice once, and a context clear erases it.
- Nothing goes above the launcher.

Keeping the item in the store, not in the conversation, is the right fix. W6 not depending on W1 to W5 is right
too.

On the Verify in item 5: Claude Code's SessionStart fires with source `compact` after a compaction, as well as for
`startup`, `resume` and `clear`. So SessionStart covers all three, and no compact hook is needed. Say so, and check
codex's SessionStart the same way.

Verdict: **HOLD on M1 to M3.** Each is a few lines of the doc. W6 goes to @runtime as soon as this lands, so they
need to be settled first.

### M1: "the orchestrator" has no lookup, and on m1mini it is on another room

Item 4 sends a held notice to the orchestrator, and item 2 keeps an orphan's item on "the orchestrator's card".
Nothing in the daemon finds the orchestrator today:
- `OrchestratorTag` is read only as a property of a card that is already in hand (`a2a.go:321`, `idlepark.go`).
- On m1mini, where the 10-02 stall happened, the orchestrator is a card on sg4-control. A local lookup finds
  nothing, so the 10-minute push goes nowhere on exactly the room it was written for.

Say how it resolves, in order:
1. A local card tagged `atrium:orchestrator`.
2. Otherwise, the hub's orchestrator through the cross-room say. That one is held by the receiver, as the hub
   already does for `name@room`.
3. Otherwise, the board chip alone.

Say also where an orphan's item lives when the orchestrator is remote. It should stay on the worker's room and be
visible in that room's board, with the notice sent across.

### M2: a final report opens an item on every finished worker

"Owes" (b) is "the worker's last message to its launcher came after the launcher's last message to it". A worker
that reports done and ends meets that, because launchers rarely answer a done report. Every finished worker would
then hold an item, and 10 minutes later the orchestrator gets a notice for it. That is the noise this design exists
to avoid.

Fix it in item 1:
- A `done` report closes its own debt. It opens an item only when it asks something, using the same
  question-or-not test the board's STUCK mark uses.
- Or let reading a done report close it, and keep "reading does not close" for questions and needs-input.

### M3: an item can outlive what it was about

An item closes only when the launcher acts. Two common cases leave it open after the reason is gone:
- The operator approves the worker's permission on the board.
- The worker is unblocked by someone else, or files a later report.

The orchestrator then gets a 10-minute notice about a worker that is running again.

Add a mechanical close, checked when the 10-minute push fires: the item closes when the worker's owing condition no
longer holds. That means the permission was decided, the worker's status left needs-input or needs-permission, or a
later report answered it. "The launcher acts" stays, as the other way to close it.

### Lows

- **L1: the bound contradicts itself.** "At most three times" lists "once more after **each** of the launcher's
  context clears", which is 2 + N pushes for N clears. Each push is one line listing every item, so say that: the
  launcher's line once, the orchestrator once, and one listing line per context clear. None of these pushes can
  start another. Also say that the orphan case and the case where the launcher is the orchestrator never send the
  orchestrator a notice about itself. Item 4 says this only for the second case.
- **L2: the `atrium` reservation has to cover names derived from a folder.**
  - Today a card started in a folder named `atrium` (this repository) registers the handle `atrium`
    (`resumeclaim_test.go:72`, `NameFromDir`).
  - An alias can also equal it when that card owns it (`alias_test.go:173-181`).
  - The ended notice already uses `atrium` as its sender (`ledger.go:858`).
  - W6's check must refuse a derived name as well as a typed handle or alias, and give the folder-named card a
    suffix. Otherwise the most likely holder of `atrium` is the atrium repo itself.
- **L3: "W0 also checks" adds to W0 without touching W0's row.** Add it to the table, or move it to W6.
- **L4: the director case in "Not covered".** A director's silent stop reaches the orchestrator only if the
  orchestrator is its launcher (`reportsToLauncher`: agent-launched, or `report_to` set). A resident director
  started by the operator has neither. Say so, or name that as the case operator focus covers.

Atrium-Verdict: hold e373918c..a2ea5e98
Quality: a sharp diagnosis from a real stall, each case traced to the line that lets it through. The store-kept
item is the right shape. M1 to M3 make sure it fires on the room that needs it, and only when something is owed.
