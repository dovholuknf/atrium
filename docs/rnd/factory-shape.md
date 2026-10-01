# The factory: how it is shaped, what it costs, and whether to change it

Written by @rnd on 2026-09-30, at clint's request through the orchestrator. Design only. Nothing here is built.

## The answer

- **Keep directors, but make them cheaper.** Do not flatten. Flattening saves money only because less work gets
  done. The cost per unit of work was the same in both eras.
- **Keep @review on Opus.** It is the cheapest department that catches the worst defects.
- **Move the coding directors to Sonnet, and let atrium enforce their context ceiling.** Those two changes are worth
  about $285 over the two days measured, a quarter of all spend.
- **Make @rnd on demand.** Launch it for a design and exit it after. An idle resident pays a cold cache on every
  wake.
- **Hub orchestration: partial.** Build (b) and (c) now. Build (a) in two steps: first a "ready to deploy" signal
  with a one-click deploy, and the automatic deploy only after a week of clean clicks. See the last section.

## The factory today

**Who does what:**

- **clint** decides. He answers the questions that only he can answer, approves designs, and reads the morning
  report. He also runs his own cards, for example PR reviews.
- **The orchestrator** runs on sg4-control, in its own room, so that a claude-sg4 restart does not take it down. It
  takes clint's requests, sets priorities, briefs directors, accepts finished work and deploys. It hub-deploys when
  @review says HUB DEPLOY OK. It room-deploys claude-sg4 when the room is idle.
- **Five Opus directors** run on claude-sg4. Each one is a resident card that owns one area and one branch:
  - @ui owns the board and the phone pages.
  - @runtime owns the daemon, the store and every migration.
  - @fabric owns the hub, the rooms and provisioning.
  - @rnd owns designs and reviews of designs.
  - @review reads every commit before it ships and gives a verdict: HUB DEPLOY OK, ROOM DEPLOY OK, or HOLD.
- **Sonnet workers** do one item each, on sg4, sg3 or m1mini. A director briefs its workers, reviews what they
  write, and merges their branch into its own. Then the director fast-forwards claude/main.
- **The record** is one shared file, `notes/director-reports.md`. Every director appends a line to it, and also
  sends the same news to the orchestrator as a message.

**The flow for one item:** clint or the orchestrator files an item. A director picks it up, and a worker builds it.
The director reviews it and merges it to claude/main. @review gives its verdict, and the orchestrator deploys.
Each arrow in that chain is a message, and each message wakes a model.

## What it costs

These figures are from `notes/USAGE-2026-09-29-30.md`, which uses the board's usage rows, the transcripts, and list
prices. They cover 2026-09-29 00:00 to 2026-09-30 17:15.

- **Total:** $1,202.50.
- **Directors:** $677.00, 58%.
- **Workers:** $212.45, 18%.
- **Orchestrator:** $158.96, 14%.
- **clint's own cards:** $113.93, 10%.
- **Coordination,** which means directors and the orchestrator together: $835.97, 72%.
- **One landed worker:** $2.22.
- **One non-merge commit,** across everything atrium ran: $1.55.

**Where the director money goes:**

- **Relays.** 684 relay turns into directors cost $455.84. Each relay re-reads the director's whole context.
- **Context size.** @runtime paid $1.19 per relay at an average context of 416k. The terminal director paid $0.35 at
  101k, because it cycled 12 times. The difference is context, not the shape of the org.
- **Cold caches.** 18 director relays and 9 captures each rewrote the cache after it expired. Those cost $59.

**Orchestrator turns:**

- **Relays:** 436 turns for $54.44, $0.12 each. That is about half of the orchestrator's turns.
- **Notices:** 135 turns for $23.30. These are "ended without reporting", "is at 150k", and scheduled wakes.
- **Acknowledgement relays:** across all receivers, 452 of these cost $31.95, and the orchestrators received 235 of
  them. An acknowledgement relay is a turn that read a message and said "ok".

## Three eras, compared

I took the dates from git and the spend from the transcripts. The spend counts atrium's projects on sg4 only.

**Pre-factory, 09-17 to 09-25.** clint drove Opus sessions directly, and there were few workers.

- Spend: $2,206.
- Commits that touch code: 287, which is $7.69 each.
- Lines added: 113.6k, which is $19.40 per thousand.

**Flat, 09-27 18:00 to 09-28 22:00.** The orchestrator dispatched saNN Sonnet workers, and one merger (saorch, then
@merge) owned claude/main.

- Spend: $305 over 28 hours, which is $10.90 an hour.
- Commits that touch code: 90, which is $3.39 each, or 3.2 an hour.
- Lines added: 24.8k, which is $12.30 per thousand.
- Item ids named in commit subjects: 31, which is about $9.80 each.

**Directors, 09-28 22:00 to 09-30 24:00.** The directors started at 22:00 on 09-28. Their first branch merges are at
23:20.

- Spend: $1,253 over 50 hours, which is $25 an hour. sg3 and m1mini add about $40.
- Commits that touch code: 377, which is $3.32 each, or 7.5 an hour.
- Lines added: 86.5k, which is $14.50 per thousand.
- Item ids named in commit subjects: 209, which is about $6.00 each.

**What the comparison shows:**

- **The cost per unit is the same in both factory eras.** A code commit costs $3.39 flat and $3.32 with directors.
  The directors bought about 2.3 times the throughput, at the same price per unit.
- **Both factory eras cost less than half per commit than clint driving Opus by hand.**
- **The comparison has limits.** The flat era was short, ran in the daytime, and had clint present. The director
  era includes a six-hour Windows Update reboot and the full relaunch on 09-30. Item ids are not comparable across
  eras, because the naming changed. Lines added count tests and generated files too.

## Defects caught, by era

**Pre-factory and flat.** Designs went through Mercurius and codex rounds. Code review was the merger's check and the
orchestrator's read. `FACTORY-EVAL-2026-09-29.md` records 0 bad merges, 0 reverts and 0 red mains. Nobody kept a list
of the defects that review caught, so I cannot count them.

**Directors reviewing their workers,** from the night of 09-28 to 09-29:

- sa79 missed a notify-off path.
- sa11 had a read-then-update race.
- sa89 would have wound down any worker in a repo subdirectory.
- sa74 and sa53 claimed `-race` runs that are not possible on this machine.
- A toast-timing theory was wrong.

All of these were caught before main.

**@review, since about 11:00 on 09-30:**

- The security audit found 2 critical issues (CSRF to launch and shell, and a cross-origin terminal attach), plus 3
  high, 4 medium and 4 low.
- In code reviews: 1 HIGH, an event on an empty card that halted the room store (c184ae8c, caught before the room
  deploy). 1 HIGH in `room-defender.ps1`, an agent-writable file run as admin (53ccc3b4). That commit landed before
  review, so the gate was bypassed, not missed.
- About 19 mediums, and the rest low.
- 54 review lines in total, 11 of them HOLD.

In the morning report, the night's review caught r-040, a store halt, plus four more. Those four are r-036, f-019b,
f-024 and f-023.

**Verdict:** the review that matters is the one that stands outside the department. The directors catch worker
mistakes. @review catches what the directors wrote themselves, and that is where the critical findings came from.

## What clint had to do himself

- **Pre-factory:** everything. He drove each session.
- **Flat:** he answered the orchestrator, approved launches and decided the room deploys.
- **Directors:** he answers questions in batches. The directors raised more than 40 by 02:40 on 09-29, and
  `ANSWERS-2026-09-30.md` holds twenty decisions. He reads the morning reports. He is the only one who may restart a
  busy room. The 09-30 relaunch after the re-sign was his call, and the rebuild cost $57.
- **In both factory eras, the bottleneck is his decisions and not the factory's hands.** Flattening does not change
  that. Fewer questions per item does.

## The options

**Keep as is.** This costs about $600 a day at the 09-29 to 09-30 pace. It gives the most throughput, and the costs
that can be fixed stay unfixed.

**Flatten fully.** The orchestrator dispatches saNN workers directly, and one merger owns claude/main.

- The $677 director line goes away.
- The orchestrator absorbs the briefing and the review: 643 messages to directors become briefs and reviews in one
  context. That context is the expensive one, and it is the one clint talks to.
- Every worker reads the code cold. That was the reason the directors were introduced.
- Throughput falls back toward one lane, about 3 code commits an hour.
- The money saved is the money not spent on work that does not get done. The cost per commit does not change.

**Hybrid 1: @review stays, the other directors go.** This keeps the gate that catches critical issues. It still puts
all the briefing in the orchestrator, so most of the flatten costs remain.

**Hybrid 2: keep the shape, change the model and the ceiling.** I recommend this one.

- Move @ui, @runtime and @fabric to Sonnet 5.5. This saves about $140 over two days. Try it on @fabric first, and
  compare landed items per dollar after one day.
- Let atrium enforce a context ceiling of 150k on directors, including mid-turn. This saves about $145. The lean
  cycle design (`docs/rnd/lean-context-cycle-design.md`) is the mechanism.
- Keep @review on Opus. It cost $95 for two days, and it found the only critical issues.
- Make @rnd on demand: launch it for a design and exit it after. @rnd paid $3.50 to $3.70 for each of three cold
  wakes on 09-30, for $125 over the two days.
- Do the hub orchestration below. It saves about $40, and the orchestrator stays free for clint.

Together these save about $300 to $350 of the $1,203 (the estimates overlap), at the same throughput. Each change
can be undone with a setting or a relaunch.

## Hub orchestration: should code do the orchestrator's mechanical work?

**Answer: partial.** (b) and (c) yes, now. (a) in two steps.

**The saving, over the same two days:**

- (a) There were 33 hub deploys on 09-29 and 09-30. Each one takes a relay turn to read the verdict and one or two
  turns to build, run and verify. At $0.12 to $0.30 a turn, that is about $15. This is my estimate: the transcripts
  do not tag a deploy turn.
- (b) The orchestrators received 235 acknowledgement relays. At $0.07 each, that is about $17. Some "LANDED" and
  "review OK" relays took more than two replies, so (b) is worth $15 to $25 in total.
- (c) Notices cost $23.30. Most of it is already saved: since `86d79282` (09-30 10:15), the orchestrator's notices
  are held on its card and not typed. What remains is the liveness pings, about $3 to $5.
- **Total:** about $35 to $45. That is 3% of all spend, and about a quarter of the orchestrator's spend. The bigger
  gain is that the orchestrator wakes for clint, and not for news.

**(a) The hub deploys itself.** This is not the hub process swapping its own binary. It is a timer script,
`scripts/live/auto-hub-deploy.ps1`, which can be run by hand with `-WhatIf`. It calls today's
`deploy-hub-only.ps1`. It deploys tip T of claude/main only when all of these are true:

1. Every code commit between the installed build's commit and T has a verdict that covers it. The installed binary
   is also what the room starts on its next restart, and an unplanned restart, such as a Windows Update reboot,
   counts. A ROOM-SIDE commit with no ROOM DEPLOY OK therefore blocks even a hub deploy.
   - **A code commit** touches at least one path outside `docs/`, `changelog/` and top-level `*.md`. Scripts, tests
     and the board's files all count. A merge counts only when it carries its own change: a conflict resolution,
     which `git show --remerge-diff` shows as non-empty. A merge with no change of its own is skipped.
   - **Covers means the same patch, not the same SHA.** Landings rebase, and item 49 landed under three sets of
     SHAs. A commit is covered when its `git patch-id --stable` equals the patch-id of a commit inside a verdict's
     range. A patch that changed on the way, a rebase that resolved a conflict, gets a new patch-id, and that counts
     as no verdict. That is the safe answer, because the reviewed code is not the code that landed.
2. The verdict is recorded so that a machine can read it. @review adds a trailer to its review commit:
   `Atrium-Verdict: <hub-ok|room-ok|hold> <base>..<tip>`, or `<sha>` for a single commit.
   - **A range** covers the commits in `git rev-list --first-parent <base>..<tip>`, which matches how @review
     reviews: one verdict over a branch tip. A merge in that walk is covered only for its own change, its
     remerge-diff. Commits reached through a merge's second parent need verdicts of their own. Without
     `--first-parent`, a verdict would cover every claude/main commit merged into the branch, none of which @review
     read: on r-card-model, `108ced12~1..86240e3a` is 70 commits, and the review covered 4.
   - **The newest trailer wins** for each patch, in claude/main's order, so a HOLD that is later followed by an OK
     ends as OK, and an OK that is later followed by a HOLD ends as HOLD.
   - **A conditional OK counts as OK.** The condition is @review's to enforce, with a HOLD if it is not met.
   - Today the verdict is prose in an untracked file, so step one of (a) depends on this. @review starts writing
     trailers once these rules are settled.
3. The package-scoped gate is green on T: the Go tests of the packages touched since the last deploy, and @ui's
   headless sections when the board changed. One verdict per commit cannot catch two commits that are each fine
   alone and break together. A gate on the tip can.
4. At least 15 minutes have passed since the last hub deploy, and no deploy hold or freeze is set. 8 of the 32
   deploy gaps on 09-29 and 09-30 were shorter than 15 minutes.
5. The hosts the hub answers to come from the stored hosts setting (`00b9dc0d`), not from the environment. The
   19:09 breakage happened because a deploy from a session without `ATRIUM_HOSTS` brought the hub up without it.
   `fa5688ef` reads the User environment as a stopgap. The script refuses to run while a share is configured and the
   stored hosts setting is empty.
6. The restart gate runs as it does today: wait for an idle board, then a countdown with a pause button. After the
   deploy, if the hub is not healthy in 25 seconds, or the room does not reattach, the script reverts to the saved
   binary (`Save-Revert` already keeps it). It then tells the orchestrator. A failure is the one case that wakes it.

**Step one:** the board shows "hub deploy ready: T, N commits, all verdicts in, gate green", with a deploy button
for clint or the orchestrator. When the deploy is not ready, the line names what is missing: each commit with no
verdict by its short SHA and subject, each commit under a HOLD, and a red gate. **Step two,** after a week with no
bad one-click deploy: a switch in the gear turns
on the timer. It is off by default.

Room deploys do not change. The hold, then deploy when the room is idle.

**(b) FYI reports stay on the board.** A report or a say carries a kind: `fyi` or `needs`. An `fyi` message to a card
tagged `atrium:hold-notices` is held on that card like a notice (the `holdNotice` path). It also shows on the board
and in the phone's growler as a feed line. It does not start a turn. A `needs` message is typed as today. The
default is `needs`, so a director that forgets the kind still reaches the orchestrator. The orchestrator reads the
held lines in one `atrium_task notices` call when it next wakes. The append to `director-reports.md` becomes a view
of the held lines, not a second channel.

**(c) Liveness is board state.** The STUCK mark (`stuckNow`) already exists on the card. What is left: the
orchestrator's card shows a count of its held notices, and its growler entry shows the oldest. A notice wakes the
orchestrator only when a card it launched has been stuck for longer than the escalation's second step.

**The risks:**

- **A surprise restart overlay for clint.** The countdown and the restarting cover appear without anyone having
  asked for them. The gate waits for an idle board, and the pause button holds the restart with no timeout. The
  15-minute floor keeps restarts from piling up. At night it is not a risk: with no board open, the answer is
  `go`.
- **A bad build ships with no model looking.** The health check, the room reattach, and the automatic revert catch a
  build that does not start. They do not catch a build that starts and is wrong. That case relies on the per-commit
  verdicts and the gate on the tip, the same as today. Today the orchestrator adds one more read.
- **A cross-commit interaction is missed.** The package-scoped gate on the tip is the only defence. Today the
  orchestrator does not run anything across commits either, so this is no worse than now, but it is not better.
- **A forged verdict.** Any session that can commit to claude/main could write the trailer. Today the orchestrator
  trusts a prose line just as far. Every commit has the same git author, so the script cannot
  check who wrote it. It can check that the trailer is on a commit that touches only review files
  (`docs/backlog/*/*-review-*.md`), and so it refuses a verdict that was folded into a code commit.
- **What clint loses in visibility.** Today every landing passes through a model that can mention it to him. After
  (b), FYI news sits on the board unread unless he looks. The morning report and a daily digest card take that over.
  He sees less, on purpose. If that feels wrong after a day, turn (b) off: it is one setting.

**Not in scope:** room deploys, merges, and anything that decides *what* to build. Those stay with models and with
clint.
