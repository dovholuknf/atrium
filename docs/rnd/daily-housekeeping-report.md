# The daily housekeeping report

Status: design draft, no code. Item `r-daily-housekeeping-report`. Background: `docs/rnd/card-lifecycle-design.md`
Interview Q7 and Q8, and section 8 (the sweep).

## 1. Problem

clint, 2026-10-07: "it'd be nice to have a daily 'hey you need to clean some shit' sort of alert that is not mushed
into all the other alerts. a more special report sort of thing with all the recommendations. mismatched rooms, rooms
offline, cards that are idle doing nothing, usage warnings, all that sort of thing"

Today every one of these things is true somewhere and visible nowhere together. An idle card shows as a quiet row. A
room on an old build shows a badge in the rooms tab. A leftover shows in the sweep, which nobody opens unless they
know to. A stash on the hub is not shown at all. Growlers are the wrong carrier. A growler is something waiting on a
human NOW, it is raised per card, it nags, and a pile of cleanup growlers would bury the permission and question
growlers that matter.

So this is a different kind of thing: ONE report a day, on its own surface, whose every line is a recommendation with
the one button that does it, where a button exists. It is a suggestion. Nothing in it is durable state, nothing in it
acts by itself, and nothing in it may stop a room.

## 2. The rule for every line

What does clint do with it? A line with no answer is cut. Each line below has a button, or a named place to go, or it
is not in the report. A line that only informs ("14 cards running") is not housekeeping and is not here.

## 3. The content table

"Exists" means the data is already produced by code in the tree today. "Missing" is what the report has to add.

### Cards (computed on the room, joined by the hub)

| Line | Data source | Exists or missing | Button |
| --- | --- | --- | --- |
| Idle card, 3+ days, with age and disk | Room store: task `last_activity_at` and status (`store/store.go`). Disk: `card_resources` bytes, summed by `diskOf` and `cardDisk` in `api/resources.go` | `last_activity_at` exists. A last TURN time may not, see D6. Disk is measured lazily, so it can be absent | Archive: the card's normal close flow, with its own confirm |
| PR card whose PR merged or closed | The card's PR link. The forge ask is the hub's only: `serveForge` and `forgePR` in `link/forgeroute.go`, one `forge.HubAsk` per PR | Forge route and peek cache exist. Missing: a capped loop over PR cards | Close: the card's close flow |
| Unfinished inventory and sweep leftovers | `card_resources` rows not freed on a done or dead card: `sweepReport.Leftovers` from `Sweep()` in `api/sweep.go`, grouped by owner, with `DiskBytes`. `GET /v1/sweep` keeps the last one | Exists | Sweep close for that owner: `POST /v1/sweep/close` |
| Worktree nothing owns | `sweepReport.NotOwned` | Exists, list only | None. The sweep offers nothing for it on purpose, so the line says "look" and links to the sweep view |
| Overlay or tunneler still running on an idle or finished card | Inventory rows (Q8 of the lifecycle design) | Follows Q8, rides on the inventory line | Same as the inventory line |
| Elevated command to undo | Inventory `elevated` row with its undo command (Q9) | Follows Q9, not built | Shows the undo command to run by hand. Atrium never elevates, so no button runs it |

### Hub (computed on the hub)

| Line | Data source | Exists or missing | Button |
| --- | --- | --- | --- |
| Kept stash | `refs/heads/stash/<card>/<branch>` on the hub's repos, aged by `gitsync/stashexpiry.go` (push log time or commit date, whichever is later). Retention is `git.stash_retention_days`, 30 | Age rule exists in `ExpireStashes`. Missing: a LIST of stashes with age and expiry day | Link to the card if it still exists. Delete-now only if Q4 says yes |
| Room on a mismatched build | `hubstore.Room.Version` against the hub's own build (`link/upgrade.go`, `Offered`, and its rule that `dev` never equals anything) | Version exists. Missing: the comparison as a query. `dev` builds are left out | Open the room page, which already offers the upgrade |
| Room offline | `hubstore.Room.LastSeen`, and the live set in `internal/link`. Liveness is a socket fact and lives only there, so the hub joins the two. A room never connected (`EverConnected` false) is not offline | Both exist, the join is missing | Open the room page |
| Room marked for deletion and still holding things | `Room.State == StateMarked` with `ClearedAt` | Exists | Open the room page |
| Usage warning | Per room: `GET /v1/usage`, `/v1/usage/limits` (`store.LimitReadings`), `/v1/usage/week` (`api/usageweek.go`) | Data exists. Missing: a threshold, see Q5 | Open the usage tab |

Cut, and why:

- Cards that are running or waiting. Not housekeeping, the board already shows them.
- Disk totals by themselves. No answer to "what do I do". The per-line disk rides on the idle and leftover lines.
- A reminder per idle card. The lifecycle design already decided idle cards are not alerts.
- Anything about a backlog item's state. That is the orchestrator's, not clint's cleanup.

## 4. Decisions

Each has a recommended answer. The ones that change what clint sees are also in section 6.

### D1. Where it is computed: the hub

The hub is the only place that sees every room, and the only place that talks to forges. It also owns the room list,
versions, last seen, and the stashes. But the hub does NOT own cards (a room persists its cards and the hub's cache is
a replaceable copy, see `hubstore/cache.go`), and it does not own disk or inventory.

So the report is built in two halves and joined by the hub:

```
 hub, at report time
    |
    |-- own facts: rooms list, versions, last seen, marked rooms, stashes, forge asks for PR cards
    |
    |-- for each CONNECTED room, one read-only ask over the link, with a timeout:
    |        GET /v1/housekeeping   (new, on the room)
    |            idle cards + disk, PR cards (card id and PR link only), leftovers, not-owned, usage readings
    |
    '-- merge into one report JSON, every line tagged with its room
```

The room half is a thin wrapper over what exists: the card list, `cardDisk`, the last `Sweep()` report, and the limit
readings. The room never calls a forge. It hands the hub the PR link and the hub asks.

A room that is offline contributes no half. That is not an error, it is the "room offline" line.

### D2. When: a daily time, and on demand

A daily time on the hub, a hub setting `housekeeping.hour` (default 09:00 hub local time). On demand: a Refresh button
on the report rebuilds now.

If the hub was down at the daily time, the report is built at the next start when today has none. A report is never
missed, only late. A build that fails leaves yesterday's report in place with its date showing.

A build is paced. Rooms are asked one at a time with a 10 second timeout each, and forge asks are capped at 20 per
build and cached (the peek cache already exists), so a build never bursts against a forge.

### D3. Where it is kept: a cache table, plus a dismissals table

A table `housekeeping_report` on the hub: one row per day with `day`, `built_at` and `body` (JSON). Kept 14 days, then
pruned. It is a CACHE: deleting it loses nothing and the next build recreates it. Computing fresh on every open was
rejected because the forge asks and the room asks are slow and the phone opens it often.

A second table `housekeeping_dismissal` with `key`, `fingerprint`, `dismissed_at` and `until`. See D5.

Neither table is read by anything that makes a decision. No room, card, close or sweep consults them. That is the
whole resilience argument, section 5.

### D4. How the sweep fits: read the last one, do not run it

`Sweep()` marks gone rows freed as a side effect, which is a write. The report must not be what causes writes on a
room. So the room half reads the last sweep report and, if there is none, says "no sweep yet" instead of running one.
The room already sweeps at start and on the board's ask. The report's Refresh may ask a room to sweep now (the one
`POST /v1/sweep` the board button already makes), and that is an explicit press. Recommended: the daily build never
sweeps, Refresh does.

### D5. How a line is dismissed so it does not return unchanged

Every line has a `key` (kind plus subject, for example `idle|<room>|<card>`) and a `fingerprint` (a short hash of the
facts that make it worth saying). For idle, the fingerprint is the age bucket: 3 to 6 days, 7 to 13, 14 and over. For
a mismatched build it is the two versions. For offline it is the last-seen date.

Dismiss stores `(key, fingerprint)` with a default snooze of 7 days. The line stays hidden until the snooze ends OR its
fingerprint changes, whichever is first. So:

- An idle card dismissed at 4 days comes back at 7 days idle (new bucket), not tomorrow.
- A room dismissed while on version A comes back when it is mismatched to a different version.
- A line whose condition ends is simply not in tomorrow's report, and its dismissal row is pruned.

There is also "Not this one, ever" for a specific card or room (a card clint keeps idle on purpose). It stores an empty
`until` for that key. These show in a small "Hidden (3)" list at the foot of the report so they can be undone.

### D6. Idle is a turn, not an activity touch

The ask says 3 days without a TURN. `last_activity_at` moves on more than turns, so the room half reports the time of
the last turn end if the room already records one, and falls back to `last_activity_at` otherwise. Which exists in the
store is the first thing phase 1 checks. If only `last_activity_at` exists, the line says "no activity for N days",
which is honest, and the threshold stays 3.

A card waiting on clint is not idle, it is a growler. A shelved or backlog card is not idle either, it is where clint
put it. The idle line covers running and done-not-closed cards only.

### D7. One line per card or room, not one per fact

A card that is idle, has a leftover, and holds a stash would be three lines. It is ONE line with the facts listed and
one button. Grouping is by subject: a card, a room, a stash, or usage.

### D8. Where it shows up

Not a tab. A section at the TOP of the Terminals tab, which is the primary view, collapsed by default to one strip:

```
 +------------------------------------------------------------------------------+
 | Housekeeping, today 09:00     7 things     [Refresh]                    [v]  |
 +------------------------------------------------------------------------------+

 expanded:
 +------------------------------------------------------------------------------+
 | Housekeeping, today 09:00     7 things     [Refresh]                    [^]  |
 |------------------------------------------------------------------------------|
 | Rooms                                                                        |
 |   sg4 offline since Oct 8 14:02                          [Open room]  [Hide] |
 |   m1mini on build a1b2c3, the hub is on d4e5f6           [Open room]  [Hide] |
 | Cards                                                                        |
 |   fix-login  idle 9 days  1.2 GB   PR #482 merged        [Close]      [Hide] |
 |   spike-q    idle 4 days  310 MB                         [Archive]    [Hide] |
 |   old-task   leftovers: worktree, branch, 2 overlays     [Sweep close][Hide] |
 | Hub                                                                          |
 |   2 stashes, the oldest expires in 3 days                [Show]       [Hide] |
 | Usage                                                                        |
 |   weekly limit at 82 percent, resets Oct 14              [Open usage] [Hide] |
 |                                                                              |
 | Hidden (3)                                                                   |
 +------------------------------------------------------------------------------+
```

With zero lines there is no strip at all, not a strip saying "all clear". The phone gets the same report as a page of
the same lines with the same buttons, reached from the phone's menu, not from a tab. The report always shows its "as
of" time, because it is a day old by design.

### D9. How it reaches clint without being an alert

Opening the board is how it reaches clint. Beyond that, an OPTIONAL single quiet web push per day, "Housekeeping: 7
things", with no sound, no reminder and no escalation. It is sent only if there are lines and only if clint switched
it on. It reuses the push transport but not the growler ladder, so it can never be mistaken for a permission ask. Off
by default.

### D10. Buttons call what exists

Every button is an existing flow, called as its own ask with its own confirm. Archive and Close open the card's close
flow (`closeFlow`, with its stash and leftover questions). Sweep close is `POST /v1/sweep/close`. Open room and Open
usage are navigation. There is NO "clean everything" button: closing is a choice about what to keep, and the lifecycle
design says nothing is removed except by a close that was pressed.

## 5. Resilience

The report is a suggestion, never durable state, and must never halt a room.

- Nothing reads it back. Not the close flow, not the sweep, not the board's card list. Delete both tables and no room
  behaves differently.
- A build runs only on the hub, on its own goroutine, never on a hot path (not a hook, not a tool call, not an
  announcement). A slow or hung room costs that room's half after 10 seconds and nothing else.
- Every ask to a room is read only. The new room endpoint answers from memory and the store. It walks no disk beyond
  the lazy `dirSize` measure the resources code already does. A room under load answers late or not at all, and the
  report says "room X did not answer" as a line of its own.
- A forge that is not signed in does not block the report. The PR line is skipped and the existing forge alert is the
  place that says so, once.
- A failed build keeps yesterday's report. A failed write of the report is logged and dropped.
- No new card state, no new halt reason, no new column on the cards table. This work adds no writes to a room, so it
  adds nothing to the storage-failure halt. The hub's own tables are written on the hub's own goroutine.

## 6. Open questions for clint

1. Is 09:00 hub local the right hour, and is one report a day plus the Refresh button enough?
2. Do you want the optional daily quiet push on the phone (D9), or is opening the board enough?
3. When you hide a line, is "hidden for 7 days, or until it gets worse" (D5) what you mean, or should it stay hidden
   until you un-hide it?
4. Stashes: is a list with the expiry date enough, or do you want a Delete-now button? It removes a branch from the hub
   for good, so it would ask first.
5. Usage warnings: at what level does a limit become a line? Proposed: 80 percent of any limit window, and any window
   that will run out before it resets.
6. A done card with leftovers, or one idle for 3+ days, is one line (D7). Is a single "Close" the right button, or do
   you want Archive and Sweep close side by side?

## 7. Phased build plan

Each phase is a backlog-sized item and ships on its own. Phase 1 has no UI, phase 2 is the useful one.

```
 phase 1  room half and hub build, no UI
    |
 phase 2  Terminals section, buttons, dismissals
    |
 phase 3  phone page, optional quiet push
    |
 phase 4  PR and stash lines
    |
 phase 5  elevated and overlay lines (waits on Q8 and Q9 of the lifecycle design)
```

1. **r-housekeeping-1-room-half-and-hub-build.** The room endpoint `GET /v1/housekeeping` (idle cards with age and disk,
   the last sweep's leftovers and not-owned, usage readings and the threshold rule). The hub build that asks each
   connected room and adds rooms offline, on mismatched builds, and marked. The `housekeeping_report` table, the daily
   timer, `GET /_hub/housekeeping` and `POST /_hub/housekeeping/refresh`. First task: settle D6, last turn or last
   activity. Tests: a room that does not answer, a room that answers late, a hub restarted after the daily time.
2. **r-housekeeping-2-board-section-and-dismissal.** The collapsed strip and expanded report at the top of the
   Terminals tab, buttons wired to the existing close, sweep close and room page, the `housekeeping_dismissal` table
   with key, fingerprint, snooze and "not this one", and the Hidden list. Test: a dismissed line stays hidden until
   its fingerprint changes.
3. **r-housekeeping-3-phone-page-and-quiet-push.** The same report on the phone, and the optional one-a-day quiet push,
   off by default, on a path that does not touch the growler ladder.
4. **r-housekeeping-4-pr-and-stash-lines.** The PR merged or closed line (a capped, cached forge ask per PR card, made
   by the hub only) and the kept-stash list with expiry dates (a read of `refs/heads/stash/` and the push log, next to
   `ExpireStashes`). Delete-now for a stash only if question 4 says yes.
5. **r-housekeeping-5-elevated-and-overlay-lines.** The elevated-undo and running-overlay lines. Waits on the lifecycle
   items that add those inventory kinds, so it is last and may slip with them.
