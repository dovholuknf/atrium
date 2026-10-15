# Small-model orchestration, and where to cycle the orchestrator

Research item. No code changed. Window: 2026-10-08 11:00Z to 2026-10-15 11:10Z (7 days) of the orchestrator's and the
minion's own Claude Code transcripts. Numbers only, no transcript text is quoted. Examples are short paraphrases.

## Findings

1. **The orchestrator's cost is the context it re-reads, not the work it does.** 7,493 Opus 5.5 requests cost about
   $360 at list price. Cache reads are 98.5 percent of the tokens (924M of 938M) and 51 percent of the dollars. The
   median request sits in a 125k context and adds about 0.7k fresh tokens. Any class that moves to a cheaper model
   inside the same session saves nothing, because the cache is per model. A class saves money only when it runs in
   its own short context (minion, items) or does not run a model at all.
2. **The minion is already the "small model".** It runs Sonnet 5.5 (3,259 requests, about $64 for the week). So
   "could Sonnet do the errands" is answered by the transcripts, and the open question is Haiku and local models.
3. **Moving the orchestrator's mechanical classes down is worth about $70 to $115 a week of $360.** Deploy/git errands
   (14.7 percent of cost) and idle/notice turns (8.9 percent) are the clear candidates. Review (15.7 percent) is the
   one that matters and the one most likely to go wrong, so it goes second and behind a bar.
4. **Do not distill yet.** The evidence of what a wrong call costs is in the transcripts (a key-injection hole in a
   review, a broken script marked done), and a fine-tuned small model has the least headroom exactly there. The terms
   also do not clearly allow it (see Part B).
5. **Cycling the orchestrator at 300k would not be cheaper. It is about 22 percent dearer on the input side than 200k.**
   The restart is cheap (S is 35k, and 48k once the notes are read) and context is expensive, so the optimum is
   lower than 200k, not higher. The cost curve is flat between 100k and 150k. **Recommendation: cycle at 150k.**
6. **The brief's cost multipliers are wrong for this model.** Opus 5.5 cache writes here are the 1 hour kind (2x base,
   not 1.25x) and cache reads are 0.05x (not 0.1x). With the billed multipliers the model predicts the real input side
   within 4 percent. With the brief's multipliers it is 45 percent high. There is no price step above 200k context.

### Part A, orchestrator by job (Opus 5.5, 7 days)

List price is Opus 5.5: $4 input, $8 cache write (1 hour), $0.20 cache read, $20 output per million tokens. Share is
the share of dollars. "Requests" is the share of requests.

| class | requests | input | cache write | cache read | output | req share | cost | share |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| clint-led work (investigate, edit, read for clint) | 1,996 | 4k | 4.05M | 245.51M | 786k | 26.6% | $97 | 27.0% |
| review a diff | 1,299 | 3k | 1.67M | 174.54M | 410k | 17.3% | $56 | 15.7% |
| deploy or git errand | 1,132 | 2k | 2.06M | 139.89M | 416k | 15.1% | $53 | 14.7% |
| route or relay a say | 571 | 1k | 1.34M | 75.75M | 336k | 7.6% | $33 | 9.1% |
| idle or notice handling | 778 | 2k | 868k | 103.58M | 209k | 10.4% | $32 | 8.9% |
| answer clint (no tool, text reply) | 637 | 1k | 1.05M | 82.92M | 278k | 8.5% | $30 | 8.5% |
| read notes after a restart | 472 | 944 | 2.02M | 27.48M | 124k | 6.3% | $24 | 6.7% |
| write a brief (launch a worker) | 269 | 538 | 311k | 37.71M | 408k | 3.6% | $18 | 5.1% |
| other | 339 | 678 | 613k | 36.88M | 193k | 4.5% | $16 | 4.5% |
| **total** | **7,493** | **15k** | **13.98M** | **924.27M** | **3.16M** | 100% | **$360** | 100% |

Output includes thinking tokens. 92 of the requests (1.2 percent) ran on Sonnet 5.5 and are priced here as Opus, an
overstatement of a few dollars. Clint-led work is the largest class because the biggest turns are clint's, and it is
mostly Bash, PowerShell, Grep and Read (about 42 percent shell, 29 percent Grep and Read, 5 percent backlog).

### Part A, minion by job (Sonnet 5.5, 7 days)

List price is Sonnet 5.5: $2 input, $4 cache write (1 hour), $0.10 cache read, $10 output.

| class | requests | input | cache write | cache read | output | req share | cost | share |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| deploy or git errand | 1,078 | 2k | 1.56M | 113.70M | 313k | 33.1% | $21 | 32.4% |
| review a diff | 979 | 2k | 1.05M | 113.51M | 308k | 30.0% | $19 | 29.1% |
| idle or notice handling | 523 | 1k | 402k | 55.96M | 144k | 16.0% | $9 | 13.5% |
| route or relay a say | 334 | 668 | 641k | 36.19M | 168k | 10.2% | $8 | 12.3% |
| write a brief (launch a worker) | 184 | 370 | 181k | 20.52M | 185k | 5.6% | $5 | 7.2% |
| read notes after a restart | 103 | 206 | 255k | 4.42M | 20k | 3.2% | $2 | 2.6% |
| clint-led work | 26 | 52 | 168k | 2.22M | 11k | 0.8% | $1 | 1.6% |
| other | 24 | 48 | 27k | 1.77M | 15k | 0.7% | $0.4 | 0.7% |
| answer clint | 8 | 16 | 80k | 674k | 2k | 0.2% | $0.4 | 0.6% |
| **total** | **3,259** | **7k** | **4.37M** | **348.95M** | **1.17M** | 100% | **$64** | 100% |

The minion's "review" is the minion reading the worker's diff before it merges, then reporting up. The orchestrator
reviews again. That double read is visible in both tables.

### Per-class medians (what a weaker model would be handed)

| class (orchestrator) | median context | median fresh input | median output | share of requests above 100k |
| --- | --- | --- | --- | --- |
| review | 136k | 723 | 236 | 71% |
| deploy or git | 122k | 733 | 286 | 63% |
| idle or notice | 131k | 611 | 214 | 71% |
| say | 136k | 982 | 519 | 73% |
| brief | 143k | 711 | 1,365 | 78% |
| clint (text) | 134k | 716 | 350 | 70% |
| restart | 46k | 1,467 | 195 | 11% |

### Part B, could a smaller model do each class

Weekly cost of the same token counts at each model's list price. The token counts would change with another model (more
or fewer tool loops, different thinking), so read these as the price of the context, not a forecast. Haiku 5.5 has a
price step at 100k prompt tokens ($0.10 in, $0.01 read, $0.50 out below, $0.50, $0.05, $2.50 above) and most of these
requests are above it, so it is applied per request.

| class | Opus now | Sonnet 5.5 | Haiku 5.5 | verdict |
| --- | --- | --- | --- | --- |
| deploy or git errand | $53 | $26 | $8 | **Move.** Haiku for scripted steps (the deploy scripts), Sonnet otherwise. Run it in the minion's short context. |
| idle or notice handling | $32 | $16 | $6 | **Move, or do not wake a model.** Haiku router in front. Most of these are "done: ok" with nothing to decide. |
| route or relay a say | $33 | $16 | $5 | Haiku for pure relays of a worker result. Sonnet where the say carries a decision. Opus never needed. |
| write a brief | $18 | $9 | $3 | Sonnet after the review bar exists. Briefs set what review checks, and three of eight reviews went back on a brief gap (baseline report). |
| review a diff | $56 | $28 | $10 | **Sonnet second, behind the bar.** Not Haiku. See the examples. |
| answer clint, clint-led work | $127 | $64 | $20 | **Keep Opus.** Intent is ambiguous, and every call here is the consequential one. |
| read notes after a restart | $24 | $12 | $1 | Not a model choice. S is 35k before any note is read, so trim the prefix (tool definitions, rules). |
| other | $16 | $8 | $2 | Leave. |
| **total** | **$360** | **$180** | **$55** | |

Minion, already on Sonnet: $64 a week, $19 on Haiku. The minion's errands are the best Haiku trial because they are
mostly scripted (deploy scripts, merges, test runs) and a wrong step fails loudly.

**What goes wrong with a weaker model, from the transcripts.** These are judgement calls that mattered, all from the
last seven days.

- A worker's key-handling change was sent back for a hole: the room trusted the hub's relayed set and a crafted entry
  with a forced command passed through into the authorized keys file. The diff looked complete and its tests passed.
  That is a security review, and it is the case a weaker reviewer waves through.
- A worker marked a script commit done, and the orchestrator found it was broken by a stray token from an edit, so every
  run died on an unbound variable. The fix was a new rule: syntax-check and run the test before any done. A weaker model
  that reads the worker's own report accepts it.
- Missing cards in one room were traced to a regression from an earlier transport-ordering change, so a planned
  worker was not launched. This is root cause by reading code across two merges, not a lookup.
- A worker saw two failing test units and never checked the base. The orchestrator asked for "pre-existing or
  regression" on both commits. Declining to assume a flake is a judgement call, and it recurs.
- Workers stopped after a work-in-progress commit without reporting, or ended a turn waiting on a background command.
  The orchestrator noticed the silence and re-sent the job. A cheap model also needs to notice the absence of a message.
- The Sonnet minion and its workers needed corrections in the window: one say per job and no more, stop asking per job
  for something already refused, and syntax-check and run a script before reporting it done. Those are the failure
  modes to expect from one tier lower still.
- About 82 of the 606 says the orchestrator sent in the window (roughly 1 in 7) contain a send-back or fix request, so
  rework is the common case, not an edge. By the same pattern about 97 say an accept or a land. Counts come from phrase
  matching and are approximate.

**Local open-weight models (24 to 48 GB).** Secondary sources only, check the model cards before buying anything.
Qwen3.8-27B (about 24 GB, Apache 2.0, 256k context, tool calling) is named the best single-24-GB pick by
[Best Ollama Model for Coding](https://www.agenticwire.news/article/best-ollama-model-for-coding) and
[Best Local Coding Models Ranked](https://insiderllm.com/guides/best-local-coding-models-2026/). Devstral Small 2
(24B, Apache 2.0) is the usual alternative. Qwen3-Coder-Next is the 48 GB or 64 GB Mac pick, and gpt-oss-20b (about
16 GB) is the cheap tool-calling option. Two facts decide this for us. First, our median request sits in a 125k
context, and a 24 GB card holds a 27B model at Q4 plus a long KV cache only with short contexts, so the local model
would need the minion-style 30k to 60k contexts, not the orchestrator's. Second, a local model has no marginal token
cost but our orchestration quality bar is set by the review class, where the weaker model is exactly the risk above.
Verdict: a local model is a plausible **Haiku-tier replacement for the deploy/git and notice classes inside the minion**
(an `ANTHROPIC_BASE_URL`-style swap or an items runner profile), and no for review, brief and clint.

**Distillation or fine-tuning on our transcripts.**

| class | examples in the 7 days | examples in all transcripts since 2026-09-01 |
| --- | --- | --- |
| review a diff | 1,299 requests | 3,639 |
| deploy or git errand | 1,132 | 4,687 |
| route or relay a say | 571 (606 says) | 1,869 |
| idle or notice | 778 | 3,628 |
| write a brief | 269 | 670 |
| clint text answers | 637 | 3,499 |

The all-time column includes the weeks before the minion existed and before 2026-10-10, when the orchestrator did the
errands itself, so those rows are not the same job. Each example is one request in a 125k context, so an example is
not a short pair. Making a training set means cutting each to the say or tool call plus the few tool results that
justified it, labelling outcome (accepted, sent back, later reverted), and keeping only the accepted ones. That is the
effort: a labelling pass, a redaction pass (the transcripts hold private material), and an eval set held out by date.
Order of magnitude: one to two weeks for a reviewable dataset of the deploy and say classes, with review needing
outcome labels we do not store.

**Terms.** Training on Claude outputs is the blocker, and it needs a lawyer's reading, not mine. The Commercial Terms
section D.4 says a customer may not "access the Services to build a competing product or service, including to train
competing AI models" (fetched from
[anthropic.com/legal/commercial-terms](https://www.anthropic.com/legal/commercial-terms), effective 2025-06-17, so
check for a newer version). The same terms say the customer "owns its Outputs". The Consumer Terms section 3 bars using
the Services "to develop any products or services that compete with our Services, including to develop or train any
artificial intelligence or machine learning algorithms or models"
([anthropic.com/legal/consumer-terms](https://www.anthropic.com/legal/consumer-terms), effective 2025-10-08). A model
trained for our own internal routing is arguably not a competing model, but the clause says "train" and "AI models"
without that carve-out. Which terms apply depends on how clint's Claude Code is billed (subscription versus API), and I
did not check that. Get written confirmation from Anthropic before any dataset is built. Ownership of outputs does not
override the restriction on use.

### Part C, the A/B

**Move first, in this order.**

1. **Deploy/git errands, off the orchestrator.** 14.7 percent of orchestrator cost is the orchestrator running ssh,
   merges, worktrees and deploy scripts in its own 122k context. Send them through the minion (or an item in a fresh
   context) and run the minion's scripted errands on Haiku 5.5. Expected: about $53 a week gone from Opus, plus about
   $6 to $8 on Haiku.
2. **Idle and notice turns, filtered before they wake the orchestrator.** 778 requests, $32. A router in front (a plain
   rule first: a "done: ok" or a task-notification with no failure goes to the log and not to a turn, a Haiku call only
   for what the rule cannot place).
3. **Relay says on Haiku.** Pure relays of a worker result to the minion.
4. **Then review on Sonnet, minion first.** The minion's own review of a worker diff is already on Sonnet. The A/B is
   whether the orchestrator's second look is needed at all for diffs under a size bar, with the Opus review kept for
   anything touching auth, keys, the permission chain or the schema.

**How atrium routes it.** Three options, in order of cost to build. (a) The existing minion with a model flag on its
launch (`atrium_launch` takes a model already, and the orchestrator already launches Sonnet workers), no new
code. (b) Items (`docs/changes/items-stage3`, pools and LEARNINGS.md) so the errand is a fresh context that ends with a
done or blocked, which also removes the relay turns the baseline report counted (28 turns, 20.4M). (c) A router in front
of the orchestrator for the notice class. This one is new code and the only one that avoids waking Opus at all, so it
comes last. A model switch inside the orchestrator's session is not an option: it would drop the 125k cache.

**Measure with `scripts/token-report`.** Run `run.ps1` for the same window length before and after, on the same
workload mix, and compare, per class where the cause column allows it:

- fresh tokens (input plus cache write plus output) per landed item, the baseline's headline (172k to 215k overnight,
  260k to 490k earlier days),
- all tokens per landed item, and the share spent on coordination (orchestrator plus minion, 40 percent in the
  baseline),
- say-started turns and the cache they read (61 percent of turns, 35 percent of cache read in the baseline),
- dollars per landed item at list price, by model, which the report does not print and needs adding to `build.ps1`,
- items sent back per items landed (the cards CSV, `landed` column), and re-opened or reverted commits after merge.

**Pass bar.** For each moved class over at least five working days or 30 landed items: dollars per landed item down at
least 25 percent, and sent-back rate (items returned after the cheaper model accepted them) no worse than the Opus
baseline plus 3 points, and zero accepted-then-reverted changes in the touched-by-security set (auth, keys, permission
chain, schema, deploy scripts). Any one reverted security-set change stops the trial for that class. The baseline
sent-back rate is about 1 in 7 says and three of eight reviews in the overnight report, so five days is thin. Use
paired work (the same queue reviewed by both) for review rather than comparing weeks.

### Part D, where to cycle the orchestrator

Measured from 51 orchestrator cycles that started with a restart and had at least 30 requests (61 transcript files
in the window, 10 were partial or not restarts).

| quantity | median | mean | range or note |
| --- | --- | --- | --- |
| S, context of the first request after a restart | 35.5k | 35.4k | 32.6k to 37.2k |
| S_eff, context when real work starts (first non-notes request) | 48.3k | 48.2k | 37.1k to 65.7k |
| d, tokens added per request | 777 | 1,315 | heavy tail, cycle mean 1,392 |
| requests per cycle | 131 | 133 | 53 to 191 |
| context at the end of a cycle | 206k | 208k | 176k to 295k |

S is mostly the fixed prefix (system prompt, tool and MCP definitions, instructions) before a single note is read. It
is the number to shrink. The 13k between S and S_eff is the handoff and notes.

**Check of the model against billed usage.** Opus 5.5 is billed at $4 input, $8 for a 1 hour cache write, $0.20 for a
cache read. In full-input-token units that is 2x for a write and 0.05x for a read, not the brief's 1.25x and 0.1x.
Almost every cache write here is the 1 hour kind (13.77M of 13.84M). The usage block's own figures per request, over
the 51 cycles:

| per request, input-side units | model, billed multipliers | actual | model, brief's multipliers |
| --- | --- | --- | --- |
| write (2d + 2Sd/(C-S)) | 3,349 | 3,751 | 2,093 |
| read (0.025 (S+C)) | 6,089 | 6,065 | 12,177 |
| total | 9,438 | 9,816 | 14,270 |

The billed-multiplier model is within 4 percent of the actual (reads 0.4 percent, writes 11 percent low, from cache
rewrites the d term does not carry). The brief's multipliers overshoot by 45 percent. Output is separate and flat at
about 2,100 units a request, so it does not move the optimum. The transcript totals also tie to the room's usage export
for the same window and the same sessions: 340.5M against 341.0M cache read, 1.118M against 1.119M output.

**Optimum.** With write multiplier w and read multiplier r, C* = S + sqrt(2 w S d / r). The brief's version is
w = 1.25, r = 0.1, which gives S + 5 sqrt(S d). The billed one (w = 2, r = 0.05) gives S + 8.94 sqrt(S d).

| S used | d used | C*, brief multipliers | C*, billed multipliers |
| --- | --- | --- | --- |
| 35.4k | 777 (median) | 62k | 82k |
| 35.4k | 1,315 (mean) | 70k | 96k |
| 48.2k | 777 | 79k | 103k |
| 48.2k | 1,315 | 88k | 119k |

Cost per request, input side, billed multipliers, S_eff 48.2k and d 1,315, with 2.1k of output on top:

| cycle at | input side | with output | vs 200k |
| --- | --- | --- | --- |
| 100k | 8,786 | 10,896 | -7.5% |
| 119k (optimum) | 8,603 | 10,713 | -9.1% |
| 150k | 8,832 | 10,942 | -7.1% |
| 200k | 9,672 | 11,782 | baseline |
| 300k | 11,840 | 13,950 | +18% |

The curve is flat from 100k to 150k (within 3 percent), so the exact optimum does not matter.

**Price above 200k.** None for Opus 5.5 (and Sonnet 5.5): the 1M window is at the standard rate and a 900k request
costs the same per token as a 9k one (Anthropic's pricing page). Haiku 5.5 is the exception, with a step at 100k. The
transcripts hold 473 orchestrator requests above 200k (max 345k) and nothing about them bills differently. The 300k
answer is no because of the read term alone: reading 300k for every request costs more than the 35k to 48k restart
saves.

**Recommendation: cycle at 150k.** Against 200k that is about 7 percent of the orchestrator's total, near $25 a week at
this week's volume, and 300k would add about 18 percent or $65 a week. The optimum is 119k and would save two points
more but cuts the cycle to about 55 requests. At 150k a cycle is about 78 requests instead of 133, so restarts go from
about 51 to about 86 a week. The model prices only tokens. It does not price what a restart loses (the orchestrator's
working understanding, clint's attention at each handoff, the notes it did not write down), and I could not measure
that from the transcripts. That is why 150k and not 119k. If a restart is as cheap in practice as the 48k suggests,
test 120k for a week and compare landed items.

The first lever is S. Every 10k off the fixed prefix is worth about as much as moving the cycle point by 30k.

## Method

**Data.** The orchestrator's transcripts (`D--git-github-dovholuknf-atrium`) and the minion's
(`D--worktrees-atrium-minion`), files touched since 2026-10-08 11:00Z, requests dated from then to 2026-10-15
11:08Z. Sidechain requests were none. Requests are `assistant` records with a `usage` block, deduplicated by message id
because one response is written as one record per content block (maximum taken per field). A `<synthetic>` model
record (13 of them) was dropped. A session file is one cycle, since `/clear` starts a new file.

**Pricing.** Fetched from the [Anthropic pricing page](https://platform.claude.com/docs/en/about-claude/pricing) on
2026-10-15. Opus 5.5 reads and Sonnet 5.5 reads are 0.05x of base input. 1 hour writes are 2x. Cost uses the 1 hour
field of `cache_creation` for the write rate and the 5 minute rate for the remainder.

**Classifier (rules, in order, first match wins).** A turn is the span after a user text message. Its trigger kind comes
from that message. Restart: starts with the atrium "new context" or "restart wake" line. Notice: a stop-hook feedback
message, a task notification, or an atrium or unexpected-exit notice. Minion or orchestrator: an atrium message from
the other one. Peer: any other atrium message (a worker or reviewer). Clint: anything else.

1. Request calls `atrium_launch` or `atrium_launch_worker`, or writes a BRIEF file: **brief**.
2. Request calls `atrium_say`: **say**.
3. In a restart turn, first six requests, only read-like tools, and no remote or merge command: **restart**.
4. A shell, deploy, close, exit, ci, publish or put tool whose command matches ssh, deploy, restart, merge, push,
   cherry-pick, worktree, branch, checkout, rebase, reset, stop, make, or a `go build`: **deploy**.
5. A restart turn, first twelve requests, only read-like tools: **restart**. A restart turn with no tool: **restart**.
6. No tool: **clint** in a clint turn, else **idle**.
7. A minion, orchestrator or peer turn with a diff, log, show, fetch, status, test, or any read, edit or shell tool:
   **review**. Any diff-like command outside a clint turn: **review**.
8. A notice turn: **idle**. A clint turn: **clint-led work**. Anything else: **other**.

**Spot check of 20, by hand.** Twenty requests drawn at random across the classes from the second rule set, read as
the trigger, the tool and the command. 16 were right. Two were ambiguous (a tool-schema lookup in a review turn, a
say that also edited a brief). Two were wrong: an orchestrator edit in a "minion blocked" turn that matched the
deploy rule on a path word, and a `git show` in a deploy report that matched the deploy rule on a commit name
containing "ssh". An earlier draw of 20 found 4 wrong or ambiguous and led to three rule fixes (the restart check moved
ahead of the deploy match, "watch" and "memtrim" commands dropped from deploy, peer shell commands to review). The
`ssh` word-boundary fix I tried after the second draw did not apply, so a few deploy requests are probably review. I
judge that at under 5 percent of the deploy class. The classes are by request, not by whole turn, so a long clint
turn is spread across several.

**Part D.** A cycle is one transcript file that begins with a restart message and has at least 30 requests. S is the
context (input plus cache write plus cache read) of its first request. S_eff is the context of the first request not
classed as restart. d is the difference in context between consecutive requests, drops of more than 5k and jumps above
80k excluded as compaction or a pasted blob. The cycle mean d is (end context minus S) over (requests minus one).

**Limits.** One week, one orchestrator, one operator. Thinking tokens are folded into output. Token counts are not
re-estimated for another model. Local-model names and sizes are from secondary sources. The terms were read from
fetched pages that may not be the current version. The classifier is rule based and was spot-checked on 20, not on
a labelled set.
