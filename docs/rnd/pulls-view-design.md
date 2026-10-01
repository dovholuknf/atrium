# The pulls view: pull request review that atrium runs itself (rnd-new-pulls-view)

clint, 2026-10-01: "pull request review flow sucks at this time. this software factory should prolly have a
dedicated pulls type of view or scm or something and atrium should facilitate pr's. right now it's janky and clunky
and not working and not token efficient nor fast."

The backlog item is `docs/backlog/rnd/rnd-new-pulls-view.md`. This document answers its seven questions, measured
against openziti/tlsuv PR 378 at `ad5ddf4`, the review that prompted it.

It builds on `docs/rnd/review-tab-design.md` and does not replace it. That design's stage 1 (the walk drawer,
`js/walk.js`) is built. Its stages 2 and 3 (the `pr_review` table, the `prs` index, the head check, the doors) are not,
and they are most of what this design needs. What it adds is the part that design left to @review: the review itself,
run by atrium from a stored recipe instead of by a chain of agents passing messages.

## 0. The recommendation, in one paragraph

Finish review-tab stages 2 and 3 as the **pulls** view, and add a stored **review recipe** that the daemon runs on
every PR that reaches it, with no adoption step and no director in the loop. The daemon fetches the diff and a
checkout at the head ONCE into the run folder. One **prime** call reads that bundle, and every reviewer, verifier and
critic is a one-shot `claude -p` that FORKS the prime session, the same mechanism keep-alive already uses, so all of
them read one cached copy of the diff instead of each paying to read it. Reviewers return JSON. A merge call dedupes
it, and the daemon writes the finding files and `walk.txt` itself. The only live session is the walker, launched
when clint opens the walk. Target: open to walk-ready in **under 10 minutes** with no human wait, for **under $2**.
PR 378 took 1h49m and about $7.

## 1. What PR 378 cost, measured

From the ten transcripts, deduplicated by message id (`D:/tmp/pr378/tokens.ps1`), and the board's task rows
(`D:/tmp/pr378/timeline.ps1`). Times are local EDT.

### 1.1 Time

| time | event |
| --- | --- |
| 10:30:56 | gwt opens card `pr-tlsuv-378`. It announces itself to @review |
| 10:31:20 | @review holds it under the pause and asks clint. The question never reaches clint |
| 11:12:00 | clint tells the card to go, after 41 minutes idle |
| 11:12:37 | the review-panel skill dispatches three Agent-tool subagents. clint: "why are we not doing this using atrium" |
| 11:13:04 | the three are stopped (about 150k cache write wasted, one on Opus) and relaunched as atrium cards |
| 11:15:47 to 11:16:23 | fit and functional finish, about 3 minutes each. C systems lost its brief to a shared `BRIEF.md` |
| 11:17:00 to 11:22:09 | C systems reruns, 5 minutes |
| 11:22:42 | a flat verdict: 6 med, 12 low, 4 nit. No run folder, no walk, results in `C:/temp` against rule 10 |
| 12:10:43 | @review adopts the card and sends the brief, 48 minutes after the verdict |
| 12:12:41 to 12:16:28 | wave 2: two verifiers, a coverage critic, a consumer-impact pass, 3 to 4 minutes each |
| 12:17:24 to 12:19:30 | 17 finding files written one at a time, a Mercurius round started, `walk.txt` with 17 open |

**Open to walk-ready: 1h49m. Model work: about 19 minutes. Waiting on a director or on clint: about 89 minutes.**
Every helper then stayed alive about an hour after it reported, and each reported twice (a say, then
`atrium_report`), which cost the parent four extra turns.

### 1.2 Tokens

| session | calls | cache write | cache read | output | about $ |
| --- | --- | --- | --- | --- | --- |
| parent | 64 | 141,629 | 4,505,036 | 47,122 | 1.94 |
| C systems | 15 | 72,554 | 643,807 | 12,998 | 0.55 |
| fit | 12 | 55,992 | 534,101 | 12,764 | 0.46 |
| functional | 15 | 67,563 | 771,653 | 15,127 | 0.58 |
| C systems rerun | 36 | 84,535 | 2,110,217 | 23,792 | 1.00 |
| verify A | 19 | 82,341 | 1,083,774 | 14,253 | 0.68 |
| verify B | 15 | 59,266 | 496,098 | 11,933 | 0.45 |
| coverage critic | 19 | 49,431 | 704,195 | 12,337 | 0.46 |
| consumer impact | 15 | 30,389 | 363,982 | 10,308 | 0.30 |
| three stopped subagents | 8 | 149,822 | 142,027 | 270 | about 0.60 |
| **total** | **218** | **794k** | **11.35M** | **161k** | **about 7.00** |

Priced at Sonnet 5.5 rates from `internal/daemon/usage.go` (input $2, 1h cache write $4, cache read $0.20, output $10
per million). @review's own turns and the Mercurius round are not counted, so the real figure is higher.

What the table says:

- **Cost is turns times context.** A session's boot is cheap (first call about 1.2k written, 10k read). What costs is
  that every helper read the 60 KB diff and the sources itself, then carried them through 12 to 36 turns.
- **The parent is 28% of it,** and almost none of that is reviewing. It is relaying, waiting and re-reading reports.
- **Nothing was shared.** Eight sessions paid to read the same diff eight times.

## 2. What went wrong, and which part of the design removes it

| On 378 | Cause | Removed by |
| --- | --- | --- |
| 41 minutes before any work | a card had to be adopted by a director under a pause | the recipe runs on intake, no adoption (section 5) |
| 48 more minutes before the walk | the run folder and brief came from @review typing them | the run folder is made by the daemon (5.2) |
| a flat verdict, not a walk | review-panel ran without the brief | the walk is the recipe's only output shape (5.5) |
| C systems reran | three helpers shared one `BRIEF.md` | no helper has a working directory to share (5.3) |
| three subagents stopped and relaunched | the skill used the Agent tool | reviewers are daemon calls, not anyone's subagents |
| helpers alive an hour after reporting | a session ends when someone exits it | a one-shot call exits when it answers |
| two reports per helper, four parent turns | say plus `atrium_report` | the answer is the process's stdout |
| child cards with the parent's title, a false "ready" | helpers were cards | a reviewer is a step on the PR row, not a card |
| the Fit block printed twice | a model merged prose reports | reviewers return JSON, the daemon renders (5.4) |
| eight reads of one diff | one session per reviewer | one prime, forked per reviewer (5.3) |
| Mercurius lost: "the session is gone" | the card ended its turn waiting on the round, and nothing woke it | the second opinion is a bounded daemon step, and its failure never holds the walk (5.5) |
| four more helpers alive after wave 2 | the same as wave 1 | the same: no helper is a session |
| item 1 of 17 was a LOW on `engine.c:227` | verify downgraded every med, leaving one band sorted by file name | the walk order is a rank the merge step assigns, with diff order breaking ties (5.6) |
| "Item 1 of 17, file findings/01-low-engine.c-L227.txt" | the walker narrated its bookkeeping | the item shows rule 33's header and nothing else (5.6) |
| "Suggested fix:" on an unproven item | rule 43 was a sentence in a brief | a finding carries `proven`, and the renderer refuses the label without it (5.6) |

## 3. Options

| | What it is | Time to walk-ready | Cost | Verdict |
| --- | --- | --- | --- | --- |
| A | Keep sessions, fix the plumbing: no pause on clint's PRs, a run folder per helper, exit on report | about 25 min | about $5 to $6 | fixes the waiting, keeps the token shape |
| B | Atrium runs a stored recipe over a shared bundle with one-shot forks, plus the pulls view | under 10 min | under $2 | **recommended** |
| C | Hand reviewing to an outside service (Copilot review, a GitHub App) | minutes | their price | rejected: not clint's walk, and an outside service would hold a credential atrium cannot |

A is cheaper to build, and every part of it is also in B. It is not enough on its own because it leaves the cost
where it was: the C systems rerun alone cost more than B's whole run.

## 4. The view

`pulls` in the nav, the `prs` index of review-tab section 2.7 with one change: a row shows the RUN as well as the
walk, since atrium now does the run.

```
┌─ pulls ───────────────────────────────────────────────────────────────── open ▾  all repos ▾  [ + paste a PR ] ┐
│                                                                                                                │
│  openziti/tlsuv #378   tls engine: session resumption on reconnect      ready to walk       ○ no walker [walk] │
│  ad5ddf4 ✓  ekoby      6 med · 7 low · 4 nit · 1 leak                   ▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱▱  0 of 17   8m ago  │
│                        2nd: codex gpt-5.5 · 2 disputed, settled · $1.62 · 9m 40s                               │
│  ────────────────────────────────────────────────────────────────────────────────────────────────────────────  │
│  openziti/ziti #4410   router: drop stale terminators on rejoin          reviewing 4 of 9    ◐ verify          │
│  71c0e2a ✓  plorenz    ...                                                $0.71 so far       2m ago            │
│  ────────────────────────────────────────────────────────────────────────────────────────────────────────────  │
│  openziti/zrok #1277   share creation rollback compensation             walking 5 of 14     ● pr-zrok-1277     │
│  4f332b8 ⚠ moved       6 med · 6 low · 1 nit · 5 leaks                  ▰▰▰▰▰▱▱▱▱▱▱▱▱▱  5 of 14   12m ago       │
│  ────────────────────────────────────────────────────────────────────────────────────────────────────────────  │
│  openziti/tlsuv #377   ...                                              failed: fetch       [retry] [log]      │
└────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

**One row** shows repo, number, title, author, the head it was reviewed at with the moved mark (review-tab 2.6), the
state of our review, finding counts by severity and leaks, walk progress, the second opinion's summary, and what the
run cost and how long it took. The states are:

| state | meaning |
| --- | --- |
| `queued` | known, not started. Only a source-found PR without clint's review requested waits here (section 6) |
| `fetching` | diff and checkout at the head |
| `reviewing N of M` | the recipe's steps, with the current step named on the right |
| `ready to walk` | finding files and `walk.txt` written, nothing walked |
| `walking N of M` | some walked. The walker card, when there is one, is named |
| `walked` | every finding done or skipped, none deferred (review-tab 1.6) |
| `aborted` | clint stopped the run. The run folder is deleted (rule 44), and the row stays until archived |
| `failed: <step>` | a step failed or the budget stopped it. `retry` reruns from that step, `log` opens `run.log` |

The `stage` of the row is a fact about the run, observed by the daemon from the run folder and the runner, never
typed. It follows "status is a column, activity is a badge": `walking` and `walked` come from `walk.txt`, and
`reviewing` is the runner's live state, gone on a restart and recomputed from the folder.

**Opening a row** opens the walk drawer that stage 1 built, over `/v1/prs/{id}` (review-tab 3.4), with the PR URL in
the header and each item in clint's walk shape. A row with no walker opens the drawer over the files alone, read and
mark only, and `walk` launches the walker (section 7). A failed row opens `run.log`.

PR rows never appear in the agent list or the stack. That is the "not cards mixed into the agent list" clint asked
for, and it comes free: a run is not a card. Only a walker is a card, and it carries the `pr:<org>/<repo>#<n>` tag so
the stack can hide it behind its row.

## 5. The pipeline

```
  door (gwt, paste, source)         the daemon, room side                                   clint
 ─────────────────────────┐  ┌──────────────────────────────────────────────────────┐  ┌──────────────┐
  POST /v1/prs {url}      ├─>│ 1 fetch   gh pr view + gh pr diff + git fetch head   │  │ pulls row    │
                          │  │ 2 prime   claude -p  reads bundle.md, session kept   │  │ ready to walk│
                          │  │ 3 panel   N forks of prime, one per reviewer, JSON   ├─>│ [walk]       │
                          │  │ 4 verify  forks of prime over high and med findings  │  │   │          │
                          │  │ 5 merge   one fork: dedupe, sort, number, JSON       │  │   v          │
                          │  │ 6 second  the chosen runner over the merged list     │  │ walker card  │
                          │  │ 7 settle  one fork per dispute                       │  │ cwd=run dir  │
                          │  │ 8 write   daemon renders findings/*.txt + walk.txt   │  │              │
 ─────────────────────────┘  └──────────────────────────────────────────────────────┘  └──────────────┘
                                  every step logs to run.log and records its cost
```

### 5.1 The recipe

A stored row, shaped like a harness or a source because it is the same idea: a named, editable thing the daemon runs.

```sql
-- added at the END of the migration slice
CREATE TABLE IF NOT EXISTS pr_recipe (
  name          TEXT PRIMARY KEY,           -- 'default', or one per org or repo
  match         TEXT NOT NULL DEFAULT '',   -- '' for every PR, else a glob over 'org/repo'
  harness       TEXT NOT NULL,              -- the harness row whose binary and model run the forks
  panel         TEXT NOT NULL,              -- JSON: [{"agent":"c-systems-reviewer","when":["*.c","*.h"]}, ...]
  verify_at     TEXT NOT NULL DEFAULT 'med' CHECK (verify_at IN ('high', 'med', 'low', 'none')),
  critics       TEXT NOT NULL DEFAULT '[]', -- JSON: ["coverage", "consumers"]
  second        TEXT NOT NULL DEFAULT '',   -- a harness row name, or '' for none
  walker_brief  TEXT NOT NULL,              -- the walker's brief template, rules 31 to 43 in it
  budget_usd    REAL NOT NULL DEFAULT 3.0,  -- the run stops at this, and says so on the row
  turns_cap     INTEGER NOT NULL DEFAULT 12,-- --max-turns for each fork
  updated_at    TEXT NOT NULL
);
```

- **The panel** names agent definitions (`~/.claude/agents/<name>.md`), the same ones the review-panel skill picks
  from, with the file globs that select each. The daemon reads the agent's body at run time and puts it in the fork's
  prompt, so improving an agent improves the recipe with no edit here.
- **The most specific `match` wins,** the rule standing rules already use. A repo with no recipe gets `default`.
- **The recipe is configuration.** clint edits it in settings, and the `default` row is seeded by a migration with
  the panel review-panel picks today, so stage 1 works with no setup.

### 5.2 The run folder and the bundle

The folder is the one review-tab and `docs/review/review-memory-design.md` already define, made by the daemon:

```
<reviews_root>/github-openziti-tlsuv/pr-378-ad5ddf4/
  pr.json          gh pr view output: title, author, base, head, files
  pr.diff          gh pr diff, once
  src/             the repo at the head, blobless shallow fetch, read only to every step
  bundle.md        what the prime reads: pr.json summary, pr.diff, each changed file in full at the head
  steps/<step>/    one folder per step: prompt.md, out.json, the fork's usage. nothing shared
  findings/        written by the daemon in step 8, then by clint and the walker
  walk.txt         written by the daemon in step 8, then by clint and the walker
  review.json      heads, verdict line, panel, second opinion, cost per step, timings
  run.log          one line per step: start, end, cost, error
```

Every outbound call is a named command run with its own credential, the rule `fetch` and the head check already
follow: `gh pr view`, `gh pr diff`, and `git fetch` of `pull/<n>/head` into `src/`. Each is bounded in time and in
output, read-bounded as sources are, and a failure is the row's `failed: fetch` with stderr's first line.

`bundle.md` is built by the daemon, not a model, so nothing is re-read to build it. For 378 it is about 60 KB of diff
plus about 80 KB of changed files, about 40k tokens.

### 5.3 One prime, forked per reviewer

This is where the tokens are saved, and it uses a mechanism atrium already ships. `docs/review/review-memory-design.md`
decision 1 item 4 names forks "a measured experiment only", because nobody knew whether a fork could carry a named
persona. Here it carries one as prompt text after the shared prefix, not as an agent definition, and P1's acceptance
is the measurement that decision asked for. Keep-alive refreshes a card with
`claude -p --resume <id> --fork-session --no-session-persistence --max-turns 1 --output-format json`
(`internal/daemon/keepalive.go`, `forkArgs`), run by `runForkProcess`, which already resolves the binary, hides the
window, bounds the time and returns stdout. The review runner is a second caller of the same function.

1. **Prime.** One `claude -p` in `src/` with the bundle as its prompt and an instruction to read it and answer `ok`.
   Its session is KEPT, which is the one difference from keep-alive's flags. Cost: about 40k written once.
2. **Each reviewer is a fork of the prime.** `--resume <prime> --fork-session --no-session-persistence`, its prompt
   the agent body plus "return findings as JSON in this schema", read-only tools (`Read`, `Grep`, `Glob`) so it can
   look past the hunk into `src/`, `--max-turns` from the recipe. Every fork's prefix IS the prime's conversation, so
   the cache hit is by construction, not by luck in matching prompts. The diff is read from cache at $0.20 a million
   instead of written at $4.
3. **The forks of one step run at once.** clint's standing rule is to keep the CPU busy, and the wall time of a step
   is its slowest fork.
4. **A fork exits when it answers.** No card, no report, no exit, no title. Its stdout is `steps/<step>/out.json`,
   and the usage block in it is the step's cost, priced by the same code keep-alive prices with.
5. **The budget is checked between steps.** When the run's recorded cost reaches `budget_usd`, the next step does not
   start and the row says `failed: budget`, with `retry` raising the cap once.

Lean worker options (`leanArgs`, the MCP-free launch) apply to every fork, so no MCP server boots per call.

### 5.4 Findings are data until the last step

Every step after prime returns JSON, never prose:

```json
{"findings": [{"sev": "med", "path": "src/tls_engine.c", "line": 412, "code": "if (sess->resumed) {",
  "says": "...", "fix": "...", "test_ask": "Add a test to ...: ..., expect ...", "proven": "no",
  "impact": "a reconnect after resume reuses a freed session", "rank": 3,
  "exposure": {"who": "every client that reconnects", "likely": "on any network drop", "opt_in": "no"},
  "cause": "introduced", "found": "traced", "leak": "", "raised_by": "c-systems-reviewer"}]}
```

`exposure` is rule 26's three answers, rendered as the `Exposure:` line in Evidence. `impact` is one sentence for the
rank, and the two are not the same field.

- **Verify** forks get the findings at or above `verify_at` and answer per finding `holds`, `does not hold, because`,
  or `holds at <sev>`, with what they read or ran.
- **The `consumers` critic** reads rule 27's list from the repo's reviewer file, under `## Known consumers`, and
  greps the local checkouts it names. The fork's `Read` and `Grep` reach those paths, which sit outside `src/`.
- **Merge** is one fork. It gets every reviewer's and verifier's JSON, dedupes findings about the same defect,
  ranks them (section 5.6), and returns the final list. It is the only step that judges across reviewers, so it is
  the only step that needs to.
- **Step 8 is Go, not a model.** The daemon renders each finding into clint's file shape (review-tab 1.2: the label
  line, the deep link computed from the path's SHA-256 and the line, the bullets, Evidence with an `Id:`), names it
  `NN-<sev>-<file>-L<line>.txt` with `NN` the walk position, and writes `walk.txt` with every line `open`. The Fit
  block cannot print twice because no model writes the file.

### 5.5 The second opinion and the settling

Review-tab 1.3 decided that a second model family re-reads the findings and @review settles every dispute before the
walk. Both stay, moved into the recipe:

- **Step 6** runs the recipe's `second` harness row one-shot over the merged list and `pr.diff`, the same verdicts
  as before (agrees, disputes, re-rates, added). A codex or gemini harness row has a non-interactive mode, and the
  runner calls it the way it calls claude. **Mercurius** today is an MCP server with rounds, not a command, so it
  cannot be a step until it has a one-shot entry point. Until then a recipe that names it skips step 6 and the
  walker offers a Mercurius round at the top of the walk, as @review does today.
- **The second opinion never holds the walk.** On 378 the review card ended its turn waiting on a Mercurius round,
  nothing woke it, and at 12:39 the walk opened on "the session is gone". Step 6 is bounded in time like every step.
  A step 6 that fails or times out leaves the row `ready to walk` with `2nd: failed (<why>) [retry]` on it, and a
  retry reruns step 6 and step 7 over the same folder. Nothing waits in a session for an answer that may not come.
- **Step 7** settles each dispute and re-rate with one fork of the prime, which reads the code and keeps, changes or
  drops the finding with one sentence why. It writes the `Settled:` line that @review wrote. One it cannot settle
  is marked "left for clint" and shown first, as decided (review-tab question 12).

This takes the settling away from @review. @review keeps the recipe, the walker brief and the rules, which is where
its judgment compounds, and stops being a step every PR waits on.

### 5.6 The walk order, and what an item shows

clint on item 1 of 378: "and not ordered? this sucks". It was a LOW on `engine.c:227`. Verify had downgraded all six
mediums, so 17 items sat in one band sorted by file name and line. Severity alone cannot order a walk, because one
pass of verify can flatten it, and file name says nothing about how much an item matters or how the diff reads.

**The order is fixed in step 5 and never changes after** (rule 14). It AMENDS rule 6 (severity, then file, then
line) and rule 10 (`NN` is that table's order). @review changes both in the rules doc when P2 lands, so the walker
brief and the renderer never disagree. The order is:

1. Disputes left for clint, as decided (review-tab question 12).
2. Then by severity band.
3. Within a band, by the merge step's `rank`: what breaks for a user of this code, and how likely, worst first. The
   merge fork gives every finding an `impact` line, one sentence, and a rank across the whole list. Leaks rank as
   their impact says, never last by default.
4. Ties, and only ties, in diff order: the order files appear in `pr.diff`, then line. That is the order clint reads
   the PR in on GitHub, so two items of equal weight come in the order their code does.

File name is never a sort key. The file's `NN` is the walk position, so `ls findings/` is the walk.

This is a proposal to test, not a settled answer. P2's acceptance has clint walk 378 in this order and say whether
item 1 is the one clint would have started on. The rank is a JSON field, so a different rule is a change to the merge
prompt and the sort, not to the folder or the view.

**An item shows rule 33's header, the code line with its link, and the bullets. Nothing else.** The 378 walker
opened each item with "Item 1 of 17, file findings/01-low-engine.c-L227.txt". The position is the drawer's progress
bar and rail, and the file name is bookkeeping. The walker brief template says so in one line, and the drawer never
shows a file name outside its menu.

**"Suggested fix:" only when `proven` says so.** Rule 43 allows the label only for a problem proven by a test we
wrote or settled by the code, and on 378 it was a sentence in a brief that the panel ignored. Here it is a field.
`proven` is `code` or `no`. A reviewer sets it and verify must agree: a `code` that verify does not confirm becomes
`no`. There is no `test` value, because every fork is read-only (5.3) and no step writes or runs a test, so a
reviewer claiming one would be claiming something no step did. `test` is added when a step can run one, and the
renderer then accepts it only when that step's folder holds the test and its output.

Step 8 writes `Suggested fix:` only for `code`. For `no`, the `fix` field must be a question (rule 43's "Could we
...?" form, one condition, one consequence, rule 38), and a `fix` that is not one is sent back to the merge fork once
with the rule quoted. If it is still not one, the file is written without a fix bullet and Evidence says why.

**The renderer's checks.** Each is mechanical, and each sends the finding back to the merge fork once with the rule
quoted before acting:

| rule | check | if it still fails |
| --- | --- | --- |
| 5 | every finding with a non-empty `leak` that any reviewer raised is in the final list, whatever merge or settle did | the run fails `merge`, naming the missing leaks. A leak is never dropped by a model |
| 8, 35 | the line is one the PR adds or changes at the head, read from `pr.diff` | the run fails `merge` naming the finding. It is never moved to a line by the renderer |
| 26 | a MED or higher has all three `exposure` answers | the run fails `merge` naming the finding. Its severity is never changed by the renderer |
| 34 | "LLM review says" once | the duplicate lead-in is removed |
| 36 | one fix, no "X, or Y" | the fix bullet is dropped and Evidence says why |
| 40 | every path in a bullet exists in `src/` | the finding is written with the path flagged in Evidence |
| 43 | `Suggested fix:` only for `proven: code` | as above |

**The head at walk start (rule 17).** Until P3 builds the head check, the walker brief keeps rule 17's
`gh pr view <n> --json headRefOid` step at the start of every walk. From P3 the daemon runs it when the drawer
opens, and the brief's step goes.

## 6. Intake: three doors, one endpoint

Every door ends in `POST /v1/prs {"url": "...", "why": "..."}`, which matches the URL against the recogniser table
(`internal/api/recognisers.go`, built), creates the `pr_review` row and its run folder, and starts the recipe. There
is no second intake.

1. **gwt.** Today gwt opens a card that announces itself to @review. Instead it calls `POST /v1/prs`, and no card is
   made. The adoption step that lost 41 minutes on 378 is gone. This is a change to gwt and to
   `docs/review/review-pr-start-design.md`, owned by @review.
2. **Paste.** `+ paste a PR` on the pulls view, and the recogniser's `deliver_to` from review-tab 1.1, which now
   delivers to `/v1/prs` rather than to @review's card.
3. **A source.** `gh search prs --review-requested=@me --json url,title,repository` as a source row
   (`internal/store/sources.go`, built), or any other source that lists PRs. Its items land in the pulls view as
   rows rather than in the inbox.

**A source-found PR is reviewed on arrival when clint's review is requested on it, and on a click otherwise**
(decided, clint, 2026-10-01). The test is per PR, not per source, so a source listing every open PR in a repo works
too. The fetch step's `gh pr view` asks for `reviewRequests` as well, and the daemon compares it with the login
`gh api user --jq .login` answers, read once per daemon start and kept in memory. A match starts the recipe. No
match, or a login check that failed, leaves the row `queued` with a `review` button. A queued row whose review is
requested later starts on the next source tick that sees the request.

**A PR clint asked for is never held by a pause.** A board pause stops directors from picking work. A recipe run is
not a director's work and has no director, so the pause does not apply to doors 1 and 2, nor to a source-found PR
with clint's review requested.

## 7. The walk

Review-tab stage 1 built the drawer, and it stays as it is. Two things change.

- **The walker is launched on demand,** from the recipe's `walker_brief`, when clint presses `walk`. Its cwd is the
  run folder, so the drawer's file endpoints reach it through `internal/safepath` as they do today. It is the only
  live session in a review, and it costs only when clint is walking. This answers review-tab question 10: the board
  launches the walker itself, from a stored brief. It carries rule 30's tags (`atrium:subagent`, `dept:review`,
  `review`, `pr`) and `pr:<org>/<repo>#<n>`. The brief holds rule 29 as amended at ff815049, so a Mercurius round the
  walker offers never holds the walk either.
- **The walk can happen in the pulls view without a walker at all.** done, skip, defer and comment already write
  `walk.txt` and the finding's file from the drawer. The walker is for the conversation ("do we care? do we know?"),
  which is the part clint called useful, and `a` still asks it about the finding on screen.

Posting stays clint's: option A today, B (read back and mark done) with the doors, C (a pending review) only on
clint's ask (review-tab section 5).

## 8. What PR 378 would have cost

An estimate from 378's own numbers, at the same rates. The panel was three reviewers, the second wave four.

| step | calls | cache write | cache read | output | about $ |
| --- | --- | --- | --- | --- | --- |
| prime | 1 | 42k | 10k | 0.1k | 0.17 |
| panel, 3 forks | 3 × 8 turns | 3 × 6k | 3 × 8 × 55k | 3 × 6k | 0.52 |
| verify, 2 forks | 2 × 6 turns | 2 × 4k | 2 × 6 × 55k | 2 × 4k | 0.25 |
| critics, 2 forks | 2 × 6 turns | 2 × 4k | 2 × 6 × 55k | 2 × 4k | 0.25 |
| merge | 1 × 2 turns | 8k | 2 × 60k | 8k | 0.14 |
| settle, 2 disputes | 2 × 4 turns | 2 × 2k | 2 × 4 × 60k | 2 × 2k | 0.15 |
| **total** | **about 59** | **about 88k** | **about 3.3M** | **about 46k** | **about 1.50** |

Plus the second opinion on its own runner, which 378's figure did not count either. Against 378: about 59 calls
instead of 218, a ninth of the cache writes, under a third of the reads and of the output, a fifth of the money.
The output saving is JSON instead of prose reports. The read saving is no parent relaying.

**Time:** fetch about 1 minute, prime under 1, panel 4 (378's slowest first-wave reviewer, minus its rerun), verify
and critics 4 (they run at once), merge, settle and write about 2. **About 10 minutes, with no wait on anyone.**
The target in the acceptance below is the measured run, not this table.

## 9. Built where, staged, with owners

Everything runs in the ROOM daemon: it has `gh`, `git`, `claude`, the filesystem and the reviews root. The hub
proxies the board's `/v1/prs` routes as it proxies `/v1/tasks`. That is @fabric's part, a check with a test and code
only where the proxy names routes rather than passing a prefix. The store
holds the index (`pr_review`, `pr_recipe`), never a finding: the folder stays the source of truth (review-tab 3.1).

Each stage is useful alone. Every acceptance is measured by running the stage against 378 at `ad5ddf4` in a
throwaway room with its own `ATRIUM_LOCATION` and fixtures off.

### P1: the runner. @runtime.

- The `pr_review` table of review-tab 3.1 plus `run_state`, `run_error`, `cost_usd`, `started_at`, `ready_at`, all
  observed. The `pr_recipe` table with a seeded `default`. Both at the end of the migration slice.
- `reviews_root`, the run folder, the three fetch commands, `bundle.md`.
- The runner: prime, forks per step through `runForkProcess`, JSON in `steps/`, the budget check, `run.log`,
  `review.json`, and step 8's renderer.
- `POST /v1/prs`, `GET /v1/prs`, `POST /v1/prs/{id}/retry`, `POST /v1/prs/{id}/abort`, on the human listener.
- The renderer's checks of 5.6, each with a test.
- **Acceptance:** `POST /v1/prs` with 378's URL produces a run folder that the built walk drawer opens, with finding
  files in clint's shape and `walk.txt`, in under 10 minutes, under $2 as recorded in `review.json`, with no card
  made and nobody asked anything. Its findings cover at least the 6 med of 378's run, judged by @review. No
  `Suggested fix:` on a finding whose `proven` is `no`. `review.json` records the forks' cache reads against the
  prime's write, which is the fork measurement review-memory decision 1 asked for. No fork waits on a permission:
  a fork's PreToolUse hook reports for an agent atrium has never heard of, as keep-alive forks do, and the run
  shows it never blocked. An aborted run follows rule 44: the daemon deletes the run folder, `steps/` and
  `review.json` with it, and the row says `aborted`.

### P2: the pulls view. @ui, after P1's API.

- The view of section 4, the row states, the nav count on the alerting path (review-tab 2.7 and question 9).
- The drawer moved to `/v1/prs/{id}` (review-tab stage 2's @ui list), `walk` launching the walker from the brief.
- **Acceptance:** 378's row moves from `fetching` to `ready to walk` live, the walk opens and marks without a
  walker, and `walk` gives a walker that knows rules 31 to 43 and shows no file name or "item N of M". clint walks
  378 in the order of 5.6 and says whether item 1 is the right first item. If not, the rank rule changes before P3.

### P3: the doors and the second opinion. @runtime, @ui, @review.

- gwt calls `POST /v1/prs` (@review owns gwt's PR start). `deliver_to` points at `/v1/prs`. The review-requested
  source row in `scripts/`.
- Step 6 and step 7, and a recipe editor in settings (@ui).
- The head check and moved-head marks of review-tab 2.6 and 3.2.
- The review-requested test of section 6: `reviewRequests` against the `gh` login, on arrival or `queued`.
- **Acceptance:** gwt on 378 shows a row with no card, a dispute settled before the walk, and a moved head marked. A
  source-found PR with clint's review requested starts by itself, and one without waits on `review`.

### Hub. @fabric, beside P2.

- `/v1/prs` and its SSE event reach a room's board through the hub, on the phone too.
- **Acceptance:** the pulls view of a room, opened through the hub, shows 378's row and opens its walk.

### E2E: the test that closes the design. @rnd.

A real PR, not 378 replayed, goes in through a door (gwt, or the review-requested source), runs the recipe, and is
walked in the pulls view on the live board. Recorded in `review.json` and the row: open to walk-ready wall time, cost,
fork cache reads, finding count, and whether a human or director was waited on (it must be none). It passes at under
10 minutes and under $2, against 378's 1h49m and about $7, with clint's read of the walk order taken. A pass is
reported with those numbers. A miss is reported with them too, and the stage that missed is reopened.

### P4: only on clint's ask.

Pending-review posting (review-tab option C), and Mercurius as a step once it has a one-shot entry point.

## 10. What is out

- **Atrium holds no GitHub credential.** It runs `gh` and `git`, named commands holding their own. The same rule as
  overlays and `fetch`.
- **Atrium posts nothing to GitHub** before P4, and P4 is a pending review clint submits.
- **The review-panel skill stays** for targets that are not a PR (a branch, a local diff) and for a person who wants
  a review in chat. The recipe is the skill's steps moved into atrium for PRs, and the two share agent definitions.
- **A recipe run is read-only, so it does not cover testing rules 1 to 4** (real hardware, three builds per repro,
  environment details asked up front, external failures apart). A review asked for with hardware or repros is a
  walker's job or a separate card, and the row says "read-only review" so nobody reads it as having run anything.
- **No reviewer is a card.** A step that needs a conversation is the walker's job, and there is one walker.

## 11. Decided

1. **A source-found PR** is reviewed on arrival when clint's review is requested on it, and on a click otherwise
   (clint, 2026-10-01). Section 6 says how the daemon tells.
2. **Delivery** is @rnd's: each stage goes to its owner in order, and the design is done when the end-to-end test
   passes. A real PR goes from intake through the recipe to a walk in the pulls view, measured against 378's 1h49m
   and about $7 (clint, 2026-10-01).
