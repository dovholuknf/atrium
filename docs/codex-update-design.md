# Keeping codex up to date (backlog-2 item 12)

Design only. Nothing here is built. Written 2026-09-29 by @runtime.

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

No migration. The column and index exist, and `''` is already "not keyed".

### 3. Codex does not update itself inside a launch (fixes C, item 12's third ask)

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

### 4. The auto-update setting (item 12's second ask)

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

1. **Suppress codex's own updater (3) on the seeded row?** Recommendation: yes. It is what made item 12 look like a
   hang, and it is an item 67 style trap for typed input. The cost is that codex never updates without atrium's card
   or the setting.
2. **Auto-update per runner row, or codex only?** Recommendation: per row, off everywhere. Nothing in it is
   codex-specific.
3. **Is "the next background check with nothing running" soon enough for auto-update?** On a room that seldom
   launches codex it may never come. The alternative is a check at boot for rows with auto-update on, which is one
   registry request per such row per boot. Recommendation: add the boot check, only for rows with auto-update on.
4. **Archive the two 2026-09-14/15 claude cards and the stale codex card now,** or let rule 1 do it at the next
   launch? Recommendation: let rule 1 do it, since that exercises it.
