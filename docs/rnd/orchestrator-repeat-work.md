# What the orchestrator and the minion repeat, and the tools that would stop it

Item `rd-orchestrator-repeat-work`. clint, 2026-10-15: scrub all the work the orchestrator
(`orchestrator-sg4-control@sg4-control`) and the minion (`minion-orchestrator-errand-runner-and-wo-2@claude-sg4`) do,
and list the tools to build so the same steps stop being done by hand. This is R&D. Nothing here is built, and no code
changed.

## The answer

- Tonight the orchestrator and the long minion spent **177.3M tokens** between them (2.84M fresh, 174.4M cache read) in
  1,625 model turns, for 36 landing lines in `MINION-LOG.md`. That is 4.9M tokens and 79k fresh tokens per landing for
  coordination alone, before any worker wrote a line.
- Each turn re-reads a context of about 100k tokens (orchestrator 102k, minion 114k), so **the cost is the number of
  turns, not the size of any one step.** A step that needs no judgement still costs one full turn per command.
- Rows 1 to 7 of the table below are 1,388 tool calls, the same seven jobs done by hand: reading a branch, making a
  worktree on a room, staging and landing a branch, relaying a status say, queue bookkeeping, writing the ledger, and
  rebuilding context after a `/clear`.
- Build, in this order: **(1) `atrium review <ref>`, (2) `atrium rooms worktree add|rm`, (3) `atrium item stage` and
  `land`.** They are small, they do not depend on each other, and together they are phases 2, 6, 8 and 9 of the script
  minion already designed in `docs/rnd/script-minion.md`. So they are not throwaway: the shepherd later chains them.
  Estimated saving for the three is about 45M tokens a night, a quarter of what the pair spends.

## Method, and which numbers are counts

- **Counts** (read from files, not guessed): turns, tool calls, fresh and cache-read tokens, says and who sent them,
  sessions, landings. Source: the per-turn `usage` in the Claude transcripts of both cards, for the window
  2026-10-15 21:00Z to 2026-10-16 04:11Z (17:00 to 00:11 EDT, 7.2 hours), the 380 says the hub holds for room
  `claude-sg4` in that window, `MINION-LOG.md`, `HANDOFF.md`, and the five handoff files written in the window.
- **Transcripts, not the usage export.** `atrium_export usage` has one row per session. The transcripts carry usage
  per turn and name the tool call each turn made, which is what a table of repeated actions needs. Both are read from
  the same API usage, so the totals agree to within what a still-running card adds.
- **Estimates** are marked `est`. Every saving figure is an estimate: it is the counted cost of the replaced calls
  times a stated share removed. The share is a judgement and is written next to it.
- **Attribution.** A turn that made several calls is split evenly between their categories. A turn with no tool call is
  "no tool": a reply, a wake for a say, or a final answer. Categories were assigned by what the command text matches,
  so a command that fits two (a `git log` inside a review) lands in the first match. Read the table as the right order
  of magnitude, not to the last 100k.
- **Cache read is not weighted.** It bills far below fresh input, so a table of fresh tokens ranks differently. Both
  columns are given. The tool ranking uses the sum because the brief asked for tokens, and says where fresh differs.
- **Not measured.** The orchestrator's own reading of a diff is not in a tool call, it is in the turns after one, and
  those are inside "no tool". Time spent waiting is free and is not counted. Whether a script would have made the same
  call as the orchestrator did is not testable from here.

## Totals for the window

| card | sessions | turns | fresh tokens | cache read | all | cache read per turn |
| --- | --- | --- | --- | --- | --- | --- |
| orchestrator | 12 (a `/clear` each) | 864 | 1.74M | 88.03M | 89.77M | 102k |
| minion | 5 | 761 | 1.10M | 86.38M | 87.48M | 114k |
| both | 17 | 1,625 | 2.84M | 174.41M | 177.25M | 107k |

For scale, the overnight report (`REPORT-2026-10-15-token-efficiency.md`) found the same two cards at 34.1M of 85.1M
tokens, 40 percent of everything, over 8.3 hours and 10 landings, 3.4M per landing. Tonight the pair spent 177M in 7.2
hours, about 4.9M per landing. The two figures come from different methods (usage export there, transcripts here), so
treat the gap as direction, not as a measured regression. The minion alone read 86.4M tokens over 761 turns.

## 1. Repeated actions

Orch is the orchestrator, min the minion. Tokens are `fresh k / cache read M`, split by the turn's calls as above.

| # | action | who | calls | tokens | example |
| --- | --- | --- | --- | --- | --- |
| 1 | Review a branch: fetch, range, merge-tree, diff read | orch | 143 | 169k / 13.8M | see below |
| | | min | 70 | 60k / 6.5M | |
| 2 | Worktree on a room, and ssh to probe or run | orch | 62 | 87k / 5.3M | see below |
| | | min | 108 | 108k / 10.2M | |
| 3 | Stage, verify and land a branch | min | 144 | 151k / 15.3M | see below |
| | | orch | 23 | 55k / 2.5M | |
| 4 | Relay a status say (`atrium_say`) | orch | 113 | 313k / 12.4M | see below |
| | | min | 117 | 136k / 10.2M | |
| 5 | Queue bookkeeping: backlog, collect, sync, launch | orch | 64 | 108k / 4.9M | see below |
| | | min | 330 | 255k / 17.9M | |
| 6 | Ledger and handoff notes | orch | 59 | 121k / 4.1M | see below |
| | | min | 30 | 44k / 1.4M | |
| 7 | Rebuild context after `/clear` | orch | 71 | 156k / 4.0M | see below |
| | | min | 54 | 78k / 4.1M | |
| 8 | Deploy | orch | 11 | 14k / 1.5M | `atrium-deploy.ps1 -Rooms hub` |
| 9 | End-of-turn `atrium ready` | both | 15 | 23k / 1.3M | a command per turn, 11 + 4 |
| 10 | Wake turns with no tool call | orch | 195 | 191k / 22.3M | a reply to an arriving say |
| | | min | 110 | 152k / 12.4M | |

Row 5 is backlog (orch 42, min 177), `git_collect` 52, `launch_worker` 55, `git_sync` 29, `atrium_close` 17 and the
orchestrator's `atrium_launch` 22. Row 6 leaves out 11 handoff files written and 11 read in the temp folder, which sit
in the plain `Write` and `Read` counts. Row 7 is the first-turn sequence plus `ToolSearch` (32 calls), `CronCreate`
(10), and the `git log -1`, `Get-Date` tip checks (29 orch, 45 min). Row 10 overlaps the others, because a woken turn
that makes no call is the reply that closes a unit of work. Rows 1 to 9 do not overlap each other.

### 1.1 Review a branch

Per branch the orchestrator runs `git fetch hub`, then `git log --oneline claude/main..<sha>`, then `git merge-tree
--write-tree claude/main <sha>`, then `git diff <sha> <sha> --stat`, then `git diff` of the Go and board files, each its
own turn. 39 merge-trees, 74 diff or show calls, 12 range reads and 18 fetches by the orchestrator. The minion does the
same range and diff read first (59 and 11), because it reviews before it reports. Every branch is read twice, once by
each. Example from the data: for `claude/land-bug-mail-icon` the orchestrator ran `git log --oneline
claude/main..claude/land-bug-mail-icon`, then `git merge-tree`, then a diff, then read four before and after PNGs.

What is not repeated is the judgement. Seven branches were sent back (section 1.9) and four of those were semantic (a
hole in a gate, landed measured by ancestry only, a stale board strip, a certificate ordering). That reading stays with
a person.

### 1.2 Worktree on a room

The recipe step 1 writes a PowerShell script to `/tmp`, pipes it to the room over ssh, and reads the HEAD line to be
sure `git worktree add` did not silently continue in the wrong directory. Counted: 7 `Set-Content /tmp/sgg.ps1` scripts,
`cat > /tmp/wt13.ps1` style scripts for sg3 and sgg, 35 add or remove calls by the orchestrator and 79 by the minion.
The four `git config --worktree` lines (`remote.hub.url`, `user.name`, `user.email`, `extensions.worktreeConfig`) are
identical every time. So is `git worktree remove --force` plus `git branch -D` at the end. Three rooms are Windows
(claude-sg4, sg3, sgg) and one is macOS (m1mini), so the minion writes the script in two shells.

### 1.3 Stage, verify, land

For each accepted branch the minion runs `git switch -C claude/land-x claude/main`, `git cherry-pick <sha>`, a loop that
strips a leading byte-239 BOM from changed `.md` files (`tail -c +4`), `gofmt -l internal/ cmd/`, `go run
scripts/check-test-globals.go`, `bash scripts/test-touched.sh`, then from the main checkout `git merge --ff-only
claude/land-x`, then `git switch claude/land-ci-green` and `git branch -D claude/land-x`. Counted: 54 cherry-pick or
switch calls, 61 verify calls, 6 BOM checks, 23 `--ff-only` lands. Every command is fixed, with the sha as the only
input. The exit code is the whole answer except when a cherry-pick conflicts or a test is red.

### 1.4 Relay a status say

380 says in the window. Typed by what the text opens with, and by who sent it to whom:

| say | count | avg chars |
| --- | --- | --- |
| worker to minion, "done" | 70 | 159 |
| orchestrator to minion, "accepted" | 50 | 351 |
| minion to orchestrator, "landed" | 58 | 388 |
| minion to orchestrator, "done" relay | 30 | 362 |
| minion to orchestrator, "worker launched" | 18 | 89 |
| minion to orchestrator, "land branch ready" | 10 | 437 |
| minion to orchestrator, "collected" | 7 | 537 |
| these seven, together | 243 | |
| the rest (briefs, corrections, probes, rejections) | 137 | |

So 64 percent of all says are the same chain: a worker says done, the minion relays done, the orchestrator says
accepted, the minion says landed. Four says for one merge, as the overnight report found. Quiet is
already possible and mostly used one way. The orchestrator sent 104 of its 113 says with no `kind`, which is `needs`
(`internal/daemon/fyi.go`: "NEEDS IS THE DEFAULT"), so each is delivered as a turn for the minion. The minion sent 45
fyi,
11 needs and 38 with no kind to the orchestrator, so 49 of its 94 woke the orchestrator and 45 did not. The
"accepted" says are real decisions. "worker launched", "collected" and most "landed" are news a board row already shows.

### 1.5 Queue bookkeeping

The minion made 177 `atrium_backlog` calls: 97 `status`, 55 `get`, 19 `file`, 6 `list`. The 97 status calls are the
`in-progress` then `done` pair for each of about 36 items, plus corrections. Nothing is decided in them: the state
follows from the item landing. The orchestrator made 42 (17 `get`, 16 `file`, 8 `list`, 1 `status`), which is real work:
reading an item to write its brief, filing what a review found. The 52 `git_collect` and 29 `git_sync` calls are steps 1
and 3 of the recipe, fixed commands with a room and a sha.

### 1.6 Ledger and handoff notes

17 `Edit` calls on `HANDOFF.md` and 19 PowerShell array-splice writes to it, 11 handoff files `Write` in the temp folder
(one per `/clear`), 26 PowerShell `Add-Content` lines to `MINION-LOG.md` by the minion. `HANDOFF.md` holds 37 dated
status headings across 10-14 to 10-16. Every one needs the clock, because the orchestrator's own clock has been wrong
(the file says "true clock" after times that were early), so at least 4 `Get-Date` and `date` checks sit beside them. A
landing line in `MINION-LOG.md` repeats what `git log claude/main` and the backlog already say.

### 1.7 Rebuild context after a `/clear`

12 orchestrator sessions in 7.2 hours, and five of them under 8 minutes each late in the night. They open the
same way: `Read` the handoff file (11 reads), `Read` `HANDOFF.md` (5), `ToolSearch` to load the atrium tool schemas (32
calls in the window), `CronCreate` to re-arm the 05:05 stop check (10), `Get-Date` plus `git log -3 claude/main`,
`atrium_peers`, `atrium_reports`, a probe of `/_hub/work`. The first ten turns of each session cost 17k to 60k fresh
(median 46k) and 370k to 800k cache read. That includes a little real work, so it is an upper bound on the restart
overhead. Five minion sessions open the same way, a `Read` of its handoff, `ToolSearch`, then tip checks.

### 1.8 Deploy

11 orchestrator calls and one by the minion, 1.5M tokens. The cost is small because deploys were held all night, but
the HANDOFF lines show the reasoning repeated at each entry: "Deploy HELD: 4 workers run and a hub restart drops minion
MCP". The deploy readiness chip landed tonight (`41c7151d`) and answers the same question on the board. Nothing more
is worth building here.

### 1.9 Sent back

The orchestrator's says contain 7 that open "Not yet accepted" or "NOT accepted". So **7 branches were sent back**,
against 50 "accepted" says. By the text of the say: 4 semantic (a gate that skips `peerGateOpen`, landed defined by
ancestry only when everything lands by cherry-pick, a board strip missing, an `ensureServer` ordering) and 3
mechanical (one cut from an old `claude/main`, then bucket lint failing on the merged tree twice, hub/rest at 30 files
over a ceiling of 29, and a headless unit in no bucket).
A mechanical send-back costs a say, a worker turn, a second collect, a second stage and a second review, which is
about 1.5M tokens by the per-turn averages (est, 12 to 14 turns at 100k to 115k each across the two cards).

## 2. Tools, ranked

Ranked by estimated tokens saved a night for the size of the work, with the biggest absolute saving noted. Savings are
est: the counted cost of the calls replaced times the share removed. A "night" is the 7.2 hour window measured above.
Savings across tools overlap by under 10 percent, because each replaces calls the others do not make, except where
noted. Sizes: XS under half a day, S about a day, M two to three days, L a week or more.

### Rank 1. `atrium review <ref>`: one call for the evidence packet

- **Kind:** an atrium command, `atrium review <sha|branch> [--base claude/main]`, printing one page. Also readable
  through the existing control MCP if the orchestrator keeps no shell.
- **Replaces:** section 1.1 and part of 1.3. Fetch from the hub if the ref is not local, resolve the base, print the
  commit list with authors, run `git merge-tree --write-tree` and say clean or list the conflicting files, print the
  diffstat grouped by area (Go packages, board files, docs, changelog), flag the mechanical things a reviewer checks by
  eye (a BOM, a missing `docs/changes/<id>.md` or changelog entry, a changelog date not matching, a branch not cut from
  the current `claude/main`, a `BRIEF.md` or PNG in the tree, a file in no test bucket), and list the named symbols the
  item mentions with a `git grep` hit count on `claude/main` (the "is this already built" check from `item-minion.md`).
- **Saves:** orchestrator 143 calls and 13.8M, minion 70 calls and 6.5M, 20.4M in all. A review becomes one call plus
  a read of the packet and the diff the packet points at. Share removed 65 percent, so **about 13M a night (est)**,
  about 0.1M of it fresh. Replaced calls that stay: the diff read of the files that carry the decision.
- **Size:** S. A script first (`scripts/review-ref.sh`, about 150 lines, git only, no hub), then a subcommand when the
  hub mirror check is wanted. It is read-only, so it cannot hurt anything.

### Rank 2. `atrium rooms worktree add|rm <room> <id>`: the worktree, one call, with the HEAD line asserted

- **Kind:** an atrium command on the CLI the recipe already uses (`atrium rooms git sync|collect`).
- **Replaces:** section 1.2 and the ssh probes that check a room. It runs `git worktree add -b claude/<id>` at a
  sha, sets the four worktree config lines, prints and asserts the HEAD line (fail loudly if it is not the sha, which
  the
  recipe names as the silent failure), and on `rm` does `git worktree remove --force` and `git branch -D`. It picks the
  path from the room's layout, so the Windows and macOS room tables in `item-minion.md` stop being something a model
  retypes. One command per room kind, no `/tmp` script.
- **Saves:** orchestrator 62 calls and 5.3M, minion 108 calls and 10.2M, 15.5M in all. Share removed 80 percent,
  because some ssh probes check other things, so **about 12M a night (est)**, 0.2M of it fresh.
- **Size:** S. The commands exist in the recipe word for word. The one new part is the room layout table, which
  `atrium rooms` already knows for the room link.

### Rank 3. `atrium item stage <id> <ref>` and `atrium item land <id>`: stage and land in two calls

- **Kind:** an atrium command, two verbs, matching phases `stage` and `land` and `close` of `docs/rnd/script-minion.md`
  (recommendations 6, 8 and 9).
- **Replaces:** section 1.3, the 97 `status` backlog calls of 1.5, and most of the ledger lines of 1.6. `stage`
  makes the staging worktree off `claude/main`, cherry-picks the range, strips a BOM from changed `.md` files, runs
  `gofmt -l`, `check-test-globals.go` and `scripts/test-touched.sh`, reruns once on the 20ms unit budget miss the recipe
  already names, and prints the packet. `land` refuses unless the item is accepted (an `accept` verb with an id, not
  free
  text), rebases if main moved and re-runs the checks, `git merge --ff-only`, sets the backlog status, appends the
  `MINION-LOG.md` line from the commit list, closes the worker card and removes both worktrees. A conflict or a red test
  exits non-zero with the packet and stops. Nothing lands before `accept`, as `AGENTS.md` and the recipe require.
- **Saves:** minion 144 calls and 15.3M, orchestrator 23 calls and 2.5M, plus about 3M of backlog status calls and 1.4M
  of ledger writes, 22M in all. Share removed 85 percent, so **about 19M a night (est)**, 0.25M of it fresh. This is the
  largest single saving of the three.
- **Size:** M. It is the script minion's stage, hold and land phases without the state row, so no migration and no hub
  deploy. A first version is a script (`scripts/item-land.ps1` and a bash twin, one call each) that the design later
  replaces with the Go command.

### Rank 4. `atrium orient`: one call for the first turn of every session

- **Kind:** an atrium command, printing one page, plus an optional `--stop-at HH:MM` that records the hard stop.
- **Replaces:** section 1.7. It prints the true time (from the hub, not the card), `claude/main` tip and what is ahead
  of `origin/main`, the running workers with room, card, item and age (what `atrium_peers`, `atrium_reports` and
  `/_hub/work` give separately), pending deploys, the head of `HANDOFF.md`, and the time left to the hard stop. The
  stop check becomes a field the hub holds, so 10 `CronCreate` calls and their 4 re-arm lines stop being rewritten
  after each `/clear`. A lean orchestrator profile that preloads the control tool schemas removes the 32 `ToolSearch`
  calls.
- **Saves:** the first-turn sequence of 12 orchestrator and 5 minion sessions. By the counts: `ToolSearch` 1.63M
  (orch) and 0.19M (min), `CronCreate` 0.27M, tip checks 2.1M (orch) and 3.9M (min), handoff reads 0.6M, together
  8.9M, plus about 5 of the first 10 turns of each of 17 sessions that would not be needed. **About 10M a night
  (est)**, 0.3M of it fresh. The fresh share is high here, because a post-clear turn writes the cache.
- **Size:** S. Every field is a read the orchestrator already makes. The hard-stop field is the only state.

### Rank 5. Quiet notices: stop saying what the board already shows

- **Kind:** a hub rule plus a recipe change, no new tool. "Worker launched", "collected" and "landed" become item
  step changes the board shows (item lifecycle derives `building` and `landed` from git evidence,
  `item-lifecycle-design.md`
  section 0), and a say that is only that news defaults to `kind: fyi`. The orchestrator's "accepted" stays a `needs`.
- **Replaces:** the 38 says the minion sent the orchestrator with no `kind`. By their text they are "landed", "done"
  and "worker launched" news, and each was delivered as a turn. The 45 fyi and the 11 needs are not touched.
- **Saves:** 38 wakes at the orchestrator's 102k per turn are 3.9M, plus the sending turn. **About 4M a night (est)**,
  0.1M of it fresh. It is small because the minion already sends most news as fyi. The larger part of the relay chain
  (the worker's done to the minion, the orchestrator's accepted) is a wake that carries a decision, and only rank 6
  removes it.
- **Size:** XS. `kind` and the held-fyi path exist. The work is a recipe line, "launched, collected and landed are fyi",
  and the item step write for `landed`. Do not make the orchestrator's own says fyi by default: a model that misspells
  the kind must still reach the receiver, which is why `NEEDS` is the default today.

### Rank 6. The shepherd: `atrium shepherd run <item>`, the minion as a program

- **Kind:** the Go command designed in `docs/rnd/script-minion.md`, a state row on the hub, a poll instead of a wake.
- **Replaces:** what ranks 1 to 5 do not: the minion's own say traffic (10.2M), its wake turns with no tool (12.4M),
  the 52 `git_collect`, 55 `launch_worker` and 29 `git_sync` calls (11.6M), 177 backlog calls less the status calls,
  and the `accepted` relay from the orchestrator. The orchestrator's accept becomes `atrium item accept <id>`, a verb
  with an id, so no minion has to be woken to hear it.
- **Saves:** what ranks 1 to 5 leave of the minion, by the counts above: says 10.2M, wake turns 12.4M, collect, launch
  and sync 11.6M, backlog calls less the status calls about 3M, close and exit 1M, 38M in all. Share removed 85 percent,
  so **about 32M a night (est)** on top of the first five, about 0.7M of it fresh. It is the largest saving in this
  document and the only one that removes the minion rather than shrinking it.
- **Size:** L. Thirteen recommendations in the design, a migration at the end of the hub slice, a deploy of the hub.
  Rank 6 only because the design is approved and 1 to 3 are its phases. Building 1 to 3 first makes the shepherd a
  chain of tested commands instead of a new program.

### Rank 7. `scripts/pre-done.sh`: the checks a send-back would find, run by the worker

- **Kind:** a script the worker brief tells it to run before `atrium_done`, and that `test-touched.sh` can call.
- **Replaces:** the mechanical share of section 1.9 (3 of 7). Is the branch cut from the current `claude/main`, does the
  merged
  tree pass `scripts/check-test-buckets.js`, are there no BOMs, no `BRIEF.md` or PNG in the diff, does
  `changelog/<dept>/`
  carry a file with today's date and `docs/changes/<id>.md` carry `## Test plan`, is `gofmt -l` empty and
  `check-test-globals.go` clean.
- **Saves:** 3 of 7 send-backs were mechanical, at about 1.5M each (est), so **about 4.5M a night (est)**, 0.1M fresh.
  The overnight report found the same, "three of eight reviews went back for one of these".
- **Size:** S. Most checks already exist as scripts. `atrium review` (rank 1) runs the same list on the reviewer's
  side, so build the list once and call it from both.

### Rank 8. `atrium note "<text>"`: a dated line in the ledger, written by the tool

- **Kind:** an atrium command appending a timestamped line (from the hub clock) to `HANDOFF.md`, with `--landed <sha>`
  filling the `MINION-LOG.md` line from the commit.
- **Replaces:** the 17 `Edit` and 19 PowerShell array-splice writes of section 1.6 (the splice reads the whole file to
  put a line at the top), and the clock checks that go with them. The handoff file written at each `/clear` could be
  assembled by `atrium orient` from the same lines.
- **Saves:** orchestrator 4.1M and minion 1.4M, 5.5M (rank 3 takes the `MINION-LOG.md` half). Share removed 50 percent
  after rank 3, so **about 2.6M a night (est)**.
- **Size:** XS. Worth doing only because it is a day's work for a daily saving, and because it removes the wrong-clock
  lines. Rank 3 absorbs the `MINION-LOG.md` half.

### Considered and not recommended

- **A deploy planner.** 1.5M for the orchestrator all night, and the deploy chip landed tonight. Nothing to add.
- **Auto-accept when all checks pass.** Rejected by the script minion design and by `AGENTS.md`: auto mode trades
  interruption for review, and a green run is evidence, not an acceptance. Seven send-backs, four of them semantic,
  are the proof that review reads something a check cannot.
- **A board button for accept.** It belongs with rank 6, where accept is already a verb. As a button on its own it
  helps clint when he is at the board, not the night, when he is not.
- **A shorter context for the orchestrator.** The orchestrator's average turn re-reads 102k. A lean profile cuts the
  base but the handoff, the queue and the decisions are the job. Ranks 1 to 5 remove turns, which pays whatever the
  context is.

## 3. Summary of the ranking

| rank | tool | replaces (sections) | saves per night (est) | size |
| --- | --- | --- | --- | --- |
| 1 | `atrium review <ref>` | 1.1 | 13M | S |
| 2 | `atrium rooms worktree add\|rm` | 1.2 | 12M | S |
| 3 | `atrium item stage` and `land` | 1.3, 1.5, 1.6 | 19M | M |
| 4 | `atrium orient` | 1.7 | 10M | S |
| 5 | quiet notices | 1.4 | 4M | XS |
| 6 | `atrium shepherd run` | 1.4, 1.5, rest of the minion | 32M | L |
| 7 | `scripts/pre-done.sh` | 1.9 | 4.5M | S |
| 8 | `atrium note` | 1.6 | 2.6M | XS |

Ranks 1 to 5, 7 and 8 together are about 65M a night (est), 37 percent of the 177M the pair spent. With rank 6 on top,
about 97M, 55 percent. Both are estimates built on the shares stated in each entry. The counted facts under them are
the 1,625 turns, the 107k average context, and the 243 of 380 says that are the same four-say chain.

## 4. What to build first

1. **`atrium review <ref>`.** Smallest, read-only, and it removes the one job both cards do twice on every branch. It
   also names the checklist that ranks 3 and 7 reuse, so it fixes the list in one place. Start as a script.
2. **`atrium rooms worktree add|rm`.** Smallest write, and the step where the recipe records its own silent failure. It
   turns the room layout tables in `item-minion.md` into code, which the shepherd needs anyway.
3. **`atrium item stage` and `land`.** The largest saving that does not need a hub deploy or a migration. Ship it as
   one script call per verb first. When the shepherd is built, it chains these and nothing is rewritten.

After those, `atrium orient` is the best next step, since it needs no design and 12 of tonight's `/clear`s paid for
it. Rank 6 follows once 1 to 3 are in and the shepherd can be a chain of commands instead of a new program.

## What could change these numbers

- The window is one evening, with 17 sessions. A quieter night, or one with fewer sent-back branches, shifts the
  shares. Every count above is exact for the window and none is a forecast.
- The orchestrator transcript was still being written when this was counted (864 turns, 861 in an earlier pass), so
  the totals can drift by a few turns.
- The per-turn context depends on how recently a card was cleared. Tonight's orchestrator was cleared 12 times, which
  lowers its average context. A card cleared less often would cost more per turn and save more per removed turn.
- Which says were status-only was read from the first words of each say, so a say that opens with something else and is
  news all the same sits in "the rest", and rank 5 is, if anything, low.
