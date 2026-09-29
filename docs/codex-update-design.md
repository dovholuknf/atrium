# Keeping codex up to date (backlog-2 item 12)

Written 2026-09-29 by @runtime. Sections 1 and 2 are built (sa12). Sections 3 and 4 are design only.

**What is decided and what waits.** Sections 1 and 2 fix bugs and are decided: they can be built now. Sections 3
and 4 are written as the recommended answers to the open questions at the end, and are NOT built until clint answers.
Each open question says which part of the body it changes. A "no" removes that part and nothing else depends on it.

Mercurius: round 1 (s_A8w3s9a6GtA0) asked which parts were decided, and that is now marked. Round 2
(s_cTMD6Gs7OLsF) found sections 1 and 2 buildable. Its one remaining question is whether 3 and 4 are in scope, which is
what clint's questions decide. Both advisories are folded into the tests.

## What was asked

Item 12 (raised 2026-09-24): a codex session stopped at start to update itself (`Updating Codex via npm install -g
@openai/codex...`) with nothing else on screen. It asked for three things:

1. A codex update card like claude's, `codex: <old> to <new>`, with the update as its action.
2. A "keep codex up to date" setting, off by default. On, atrium runs the update itself between sessions, never while
   a codex session runs, and records the result on the card.
3. Codex updating itself inside a launch must not look like a hang.

## What is already there

The first ask was built before the item was filed. `runnerupdate.go` (2017744, 2026-09-15) is generic: any harness row
with a `package` gets checked at launch, and migration `0052_harness_package` gave the codex row `@openai/codex`. The
installed version is read from the package's own `package.json` (`runnersetup.InstalledVersion`, shared since
dceb738), and the published one comes from one registry request. Nothing starts codex to ask.

The evidence says it worked, on a copy of the live database taken 2026-09-29:

| When (UTC) | What |
| --- | --- |
| 2026-09-24 12:33:56 | a launch finds codex 0.154.0 installed, 0.156.1 published. Card `01a0d368` "codex: 0.154.0 to 0.156.1" goes to the inbox |
| 2026-09-24 12:38:16 | codex's own check writes `~/.codex/version.json`: latest 0.156.1, nothing dismissed |
| 2026-09-24 12:57:29 | card `01a0d37e` (sa44's codex probe) launches codex |
| 2026-09-24 12:58:33 | `@openai/codex/package.json` is rewritten. Codex updated itself inside that launch, which is the sighting |

So item 12's sighting was codex's own updater, and atrium's card was already sitting in the inbox. What the evidence
shows is wrong is the part after that.

## What is wrong

**A. A satisfied card is never withdrawn.** `01a0d368` still says "codex: 0.154.0 to 0.156.1" five days after 0.156.1
was installed. Nothing retracts an update card once the update has happened some other way. Two older claude cards
(`01a0a201`, `01a0a286`, 2026-09-14 and 15, "claude code: 2.1.270 to 2.1.271" and "... to 2.1.272") are the same
failure. They date from the old source, which put the version in the key, so no later check even matches them.

**B. A card that has left the inbox blocks every later release.** The key is the package, and
`idx_task_intake_key` is unique. `store.Offer` finds the existing card by key. It refreshes it only while it is in
`backlog`, and otherwise returns it untouched. Once somebody starts, finishes or archives the update card, every later
codex release finds that card and offers nothing. That is permanent, and true for claude too.

**C. Codex's own updater runs inside atrium's terminal.** When `check_for_update_on_startup` is on (the default) and a
newer version is known, codex's TUI offers to update at start and runs the install in the foreground in its own
terminal. The binary carries the strings for both ("Updating Codex via `...`", "Update ran successfully! Please restart
Codex", and `version.json` with `dismissed_version`). The exact prompt was not reproduced, since that needs an older
codex installed. A launched card's first typed input can answer it, the same class of failure as item 67. On Windows the
install then races the runner's own `codex.exe`.

**D. A stale answer lingers.** The check runs only at launch. A room that has not launched codex since 2026-09-24
still holds `runner_latest` 0.156.1 while 0.158.0 is out (checked 2026-09-29). That is by design (the header's point
2) and cheap to live with once A is fixed, so it is not changed here.

## The design

### 1. Withdraw a satisfied card (fixes A)

`checkRunnerUpdate` already knows the installed version and the latest. When installed is NOT older than latest, it
archives any `backlog` card whose source is `runner-update` and whose external id is that package, with an event
saying why ("codex 0.156.1 is installed, which is what this card offered"). That includes legacy cards, whose key is
the package plus a version: they are matched by source and by `external_id` starting with the package and `@`.

Only `backlog` cards. A card somebody started is theirs, which is the rule `Offer` already keeps.

A backlog card with no recorded version (the pre-change codex card `01a0d368`, no URL) follows the same two rules and
nothing special. Installed equal to latest: withdrawn. A newer latest: it is REWRITTEN in place by `Offer` to "codex:
<installed> to <latest>" with the new URL, not withdrawn and re-offered. It is still in the inbox and keyed by the
package, so a rewrite is what every later release does to it anyway, and it keeps its place and any prompt edit. Only a
legacy card, whose key holds a version so `Offer` can never find it, is withdrawn and replaced by a fresh one. Every
withdrawal also releases the card's key, since an archived card still holding it would be refreshed unseen.

Archived, not deleted, so the history keeps it and the sweep deletes it on its normal timer.

### 2. A new release after the card has moved on offers a new card (fixes B)

`store.Offer` gains no general behaviour. A general source re-offering an item somebody finished would be the bug
intake was designed against. A new store call, `ReleaseIntakeKey(taskID)`, sets `intake_key = ''` on one card. The
update check calls it when the keyed card is past `backlog` and the version it offered (from the `offered` event) is
below the new latest. Then `Offer` inserts a fresh card. The old card keeps its title, history and source.

The offered version is carried in the card's `url`, which the update card leaves empty today:
`https://www.npmjs.com/package/<package>/v/<latest>`. It is a useful link on the card, and `refreshOffered` already
rewrites `url` while the card is in the inbox, so it always names the version the card last offered. No event is
needed, which matters because `refreshOffered` writes none. The check reads the version back only from a URL of
exactly that shape, which atrium wrote itself. A card without one (every card from before this) counts as older, so
it is released at most once.

The URL is part of the runner-update state contract, not only a link. The code that builds and parses it says so in a
comment, so a later tidy of how cards show links cannot quietly break release detection. The package goes into the
path as npm writes it, `@openai/codex` with its slash, and the parser splits on the last `/v/`.

No migration. The column and index exist, and `''` is already "not keyed".

### 3. Codex does not update itself inside a launch (fixes C, item 12's third ask). Waits on question 1

Codex takes config overrides as `-c key=value`, which the codex row already uses for effort. `check_for_update_on_startup`
is a top-level config key (present in codex-cli 0.156.1's `ConfigToml`). The seeded codex row gets
`-c check_for_update_on_startup=false` at the front of `args` and of `resume_args`. It goes at the front because
`resume` is a subcommand and global options go before it.

Why on the row and not in launch code: atrium learns nothing about a runner beyond what its row says, the rule
`0052` states. A new migration (at the end of the slice) prepends the pair only where the row is still the seed:
`id = 'codex'`, `args = '[]'`, and `resume_args` exactly the seeded `["resume","{resume}"]`. An operator who edited the
row keeps their edit, and the runner setup report says the flag is missing (a new check in the codex adapter, report
only).

Consequence: with the flag, codex never updates or offers to, so atrium's card is the only way anybody hears. That is
why A and B come first.

### 4. The auto-update setting (item 12's second ask). Waits on questions 2 and 3

Per runner row, not per codex: the mechanism is generic and claude has a package too. It is kept in the settings table
as `runner_auto_update`, a JSON map of runner id to true, so no migration is needed. Off for every row by default.
The switch sits on the runner row in the edit-agents screen.

When the check finds a newer version for a row with auto-update on:

- **Only when none of that runner is live.** Live means any card whose runner is that row and whose runner process
  is alive (supervised or joined). If one is, it files the card as today and tries again at the next check.
- **Only from a launch nobody is waiting on** (`blocking` false), or AFTER a blocking launch has started. A person's
  launch is never held for an install. Since the check runs before the spawn, "after" means the next check. So in
  practice auto-update happens at the first background check with nothing running, which is a fixture at boot, a
  queued launch, or a peer's launch.
- **A check at boot, only for rows with auto-update on** (question 3). One registry request per such row per daemon
  start, through the same `checkRunnerUpdate` with `blocking` false, so a room that seldom launches that runner still
  gets updated. Rows with auto-update off are never checked at boot. If clint answers no, this bullet goes, and
  auto-update waits for a background launch as above.
- **The install is `npm install -g <package>`, bounded.** Started with `hideWindow`, three minutes at most, output
  bounded while read (the sources rule), never retried within one check.
- **Recorded on the card.** The update card is filed (or refreshed) first, and the result lands on it as an event
  (`installed 0.158.0` or `npm exited 1: <last lines>`). A success then withdraws it by rule 1 at the next check.
  A failure leaves it in the inbox with the reason.

`npm` is looked up on PATH. When it is missing, auto-update reports that on the card and does nothing else.

## What this does not do

- It does not check on a timer. The header's reasoning stands: the answer is only actionable before a launch.
- It does not run claude's update. Claude Code updates itself natively, and a claude row can opt into auto-update.
- It does not handle a codex that npm did not install. m1mini's codex is `~/.local/bin/codex`. If no `package.json`
  sits where `InstalledVersion` looks, it gets no card at all, today and after this. Not verified there.

## Tests

- Store: `ReleaseIntakeKey` clears one key, and a second `Offer` with that key then inserts.
- The offered-version URL round-trips for `@openai/codex` (a scoped package with a slash) and for an unscoped one,
  and a URL of any other shape reads as no version.
- A backlog update card refreshed from one offered version to a newer one reads back the newer version from its URL.
- Daemon, with a fake registry (the existing `updateRegistry` seam) and a fake installed version:
  - installed equal to latest archives a backlog update card, including a legacy-keyed one
  - a started card at an older offered version is released and a new card is offered
  - a started card at the current version is not
  - auto-update off: no install. On with a live runner: no install. On with none: the install runs (a fake npm on
    PATH) and its result lands on the card
- Migration on a copy of the live database: the seeded codex row gets the flag, and an edited row is untouched.
- Manual (test plan): launch codex on a room where a newer codex is published. No update prompt appears, and the
  card is in the inbox.

## Open questions for clint

Sections 1 and 2 do not wait on any of these.

1. **Suppress codex's own updater on the seeded row?** Decides section 3. Recommendation: yes. It is what made item
   12 look like a hang, and it is an item 67 style trap for typed input. The cost is that codex never updates without
   atrium's card or the setting. A no removes section 3 and its migration.
2. **Auto-update per runner row, or codex only?** Decides section 4's scope. Recommendation: per row, off everywhere,
   since nothing in it is codex-specific. Codex only would key the same setting on the codex row and hide the switch
   on the others. A no to auto-update altogether removes section 4.
3. **Check at boot for rows with auto-update on?** Decides section 4's last bullet. On a room that seldom launches
   codex, "the next background check with nothing running" may never come. Recommendation: yes, one registry
   request per such row per boot. A no removes that bullet.
4. **Archive the two 2026-09-14/15 claude cards and the stale codex card by hand now,** or let section 1 do it at the
   next launch of each runner? Recommendation: let section 1 do it, since that exercises it. Changes no code either
   way.

## clint's answers, 2026-09-29

1. Suppressing codex's own updater "seems like a good idea", not settled. Section 3 is not built yet.
2. **The runner row controls whether that runner auto-updates.** That is the per-row answer, and it is off by default.
3. **Check nightly, whatever the boot.** A registry lookup costs no tokens, so the check runs once a night for every
   row with auto-update on, instead of per boot.
4. Not settled either way. Section 1 has landed (ea67105) and withdraws the stale cards on each runner's next launch.
