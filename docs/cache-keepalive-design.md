# Cache keep-alive for idle sessions

Keeping an idle card's prompt cache warm, so that coming back to it after an hour costs a cache read and not a
cache write of its whole context.

Status: built. The room daemon runs the refresh loop (`internal/daemon/keepalive.go`), keeps the ledger in the
store, and the board has the per-card switch, the settings default and the spend line. The sections below are the
design it was built from.

## Why

Last week's usage report (`dotfiles/claude/usage/REPORT-2026-09-27.md`, section 4) counted 66 cache writes after a
gap of more than an hour. They cost $160 of $1,674, which is 9.6% of the week. 37 of them were on contexts of 200k
or more and cost $137 of that.

Claude Code writes the main conversation into the 1 hour cache. A card that sits for 61 minutes and is then
answered pays to write its whole prefix again. A board holds about 15 idle cards at a time with contexts from 100k
to 430k, so this happens every time somebody comes back to one of them after lunch.

The trade is a cache READ now against a cache WRITE later. On Opus 5.5 a read is 0.05x the input price and a 1 hour
write is 2x. A refresh that is never followed by a real turn is wasted, so most of this design is about when to
stop.

## What the prior art does

Three sources were named. The Reddit thread could not be fetched from here (reddit refuses the fetcher), so the two
repositories carry this section.

### claude-thermos (izeigerman/claude-thermos)

- **The request.** A mitmproxy in front of Claude Code, reached through `ANTHROPIC_BASE_URL`. It groups
  `/v1/messages` traffic into lineages keyed on model, tools and system text, and calls the first tool-bearing one
  the main agent. A warm request is the main lineage's last real request body, deep copied, with `max_tokens: 1`,
  `stream: false` and `output_config` removed. Thinking, tool choice and every `cache_control` marker stay, because
  the message tier of the cache is keyed on them. It replays the captured headers minus `host` and
  `content-length`, straight to the API.
- **The conversation.** Untouched. The one output token is discarded. Nothing reaches the transcript and there is
  no visible turn.
- **The meter.** It replays whatever credential Claude Code sent, so on a subscription it is metered like any
  other request. The README does not discuss it.
- **Scope.** Narrow. It warms only while the main agent is idle AND a subagent runs, every 270 seconds, at most 4
  times per episode. It targets the 5 minute cache.
- **What breaks it.** A prefix that changed since the captured request. A captured bearer that has expired. A
  subagent that outlives the cycle cap.

### clodex (avirtual/clodex)

- **The request.** The same replay, from its own proxy (`wirescope`). `wire/hold.js` replays the session's exact
  last request with `max_tokens: 1`. Its comment says thinking is turned off for the ping, which contradicts
  thermos. Thermos matches the documented cache rules: changing thinking parameters invalidates the message tier.
- **The credential.** Every captured header is replayed except `authorization`, which is re-read from Claude
  Code's credential store at ping time. An OAuth token lives about 8 hours and the CLI refreshes it only on a
  turn, so the captured one returns 401 on exactly the idle seat the feature exists for.
- **The gate.** "Ping IFF warm." A prefix is warm when a response receipt said so and `stamped_at + ttl` has not
  passed. Replaying a cold prefix is a write paid for nothing, so it declines. A ping fires only inside a 300 s
  margin before expiry.
- **The policy.** A hold per session with a deadline (12 hours at most) and a budget of 24 pings. Every organic
  turn re-anchors the window and resets the budget. Two consecutive 401 or 403 answers disarm it. Transport
  errors, 408, 429 and 5xx decline without a strike.
- **Other levers.** It parks a non-urgent message to a cold peer instead of waking it, and it offers the agent a
  `compact` intent. Both avoid the write instead of preventing expiry.
- **Codex.** Messaging and intents only. The wire features are Claude-only.
- **What breaks it.** A dead credential. An app restart, which loses the in-memory last request. A changed prefix.

### What they agree on

Both replay the exact last request through a proxy already in Claude Code's API path, because the system prompt and
the tool schemas are not in the transcript. Both keep the conversation untouched.

## Mechanism

### Options

1. **Type a prompt into the card.** Atrium owns the terminal and can type into it, the way `Say` does. It is a
   visible turn. It adds junk to the transcript every hour, the agent can act on it, and it moves the prefix, so
   each refresh also writes. It cannot run while a dialog is open or a line is part written. Rejected.
2. **A replay proxy** in the room daemon, as thermos and clodex do. Byte exact. The costs are all in what it
   changes about the card:
   - Every model call, the credential on it and the whole conversation pass through the room daemon. A proxy bug
     breaks every card, not only keep-alive.
   - Claude Code turns MCP tool search off when `ANTHROPIC_BASE_URL` names a host that is not Anthropic's, which
     moves every MCP tool schema into the prefix. `ENABLE_TOOL_SEARCH=true` turns it back on, if the proxy forwards
     `tool_reference` blocks.
   - Remote Control is disabled when `ANTHROPIC_BASE_URL` is not `api.anthropic.com`.
   - The daemon has to re-read Claude Code's OAuth token from its credential file for every refresh, and it holds
     conversation bodies in memory.
3. **A forked headless resume.** The room daemon runs, in the card's directory and with the card's environment:

   ```
   claude -p "<refresh prompt>" --resume <card session> --fork-session --no-session-persistence
          --model <card model> --setting-sources local --settings <atrium's block-all-tools file>
          --max-turns 1 --output-format json
   ```

   Claude Code's documentation says a resumed conversation keeps the system prompt it started with, so its
   history sits behind the same prefix. `--fork-session` gives the run a new session id, so the card's transcript
   is never written. `--no-session-persistence` keeps the fork from saving a transcript of its own. The card's
   terminal is never touched. The `--settings` file and `--max-turns 1` are there because of the second probe
   below: without them the fork is a working copy of the agent.

### The probes

All on 2026-09-27, Opus 5.5, subscription login.

| # | original | fork | cache read | cache write | output | what happened |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | headless, 49k | `.` | 48,946 | 232 | 54 | "Your message was just `.`" |
| 2 | interactive card, 110k | `.` | 3,906,621 over many calls | 51,519 | 24,992 | it went on working |
| 3 | headless, 49k | tool-bait, `--disallowedTools '*'` | 2,550 | 63,662 | 2 | a MISS |
| 4 | headless, 49k | tool-bait, block-all hook, `--max-turns 1` | 48,946 | 252 | 110 | tool refused, stopped |
| 5 | interactive card, 152k | refresh prompt, hook, `--max-turns 1` | 145,820 | 6,175 | 60 | "OK", 1 turn, $0.08 |
| 6 | headless, 49k | as 5, plus `--setting-sources local` | 48,946 | 1,163 | 4 | "OK", 1 turn |
| 7 | interactive card, 200k | as 6 | 195,140 | 4,469 | 4 | "OK", 1 turn, $0.07 |

What they show:

- **A fork reads the original's cache, interactive or not.** Probe 1 read 99.5% of the prefix. Probe 2 forked an
  interactive card (this design's own author, mid-task) and its first call hit: over its whole run it wrote 51k
  tokens against a 110k context. Probe 5 read 96% of an interactive card's context. A cache read resets the TTL,
  so each of these refreshed the original's cache.
- **A bare fork is a working agent.** Probe 2 took `.` as "carry on". It ran for dozens of calls, edited this
  document, opened a review session and wrote a report file, all as a copy of the card. That cost $1.69 and it is
  the failure this design must never allow. The fork has the card's whole conversation, its tools, its MCP servers
  and its permissions.
- **Denying tools by name changes the prefix.** Probe 3 used `--disallowedTools '*'`. Claude Code dropped the tool
  definitions from the request and the fork wrote the whole prefix again. Any control that changes which tools
  the model sees is out.
- **A hook that refuses every tool keeps the prefix.** Probe 4 baited the fork to read a file. A `PreToolUse` hook
  matching `*` that exits 2 refused the call, and `--max-turns 1` stopped the run. Hooks are not part of the
  request, so the read was a full hit. Probe 5 is the shape the build uses: an explicit prompt, the hook, one turn.
  The model answered "OK" and nothing else ran.

- **The user's own hooks run in a fork unless they are shut out.** A user hook that logs every session start wrote
  four lines for probe 5 (startup, thinking, idle, ended) into the card's `.claude/agent-log.txt`. Any user hook
  (`SessionStart`, `UserPromptSubmit`, `Stop`, `Notification`) would run on every refresh with whatever side effect
  it has, and nothing on the receipt shows it. Probes 6 and 7 add `--setting-sources local`, which loads neither the
  user's settings nor the project's shared settings. Both still read the whole prefix, and the logging hook wrote
  nothing. So user and project settings, the hooks in them included, are not part of the cached request, and the
  fork runs without them.

The refresh prompt is: "Automated cache refresh from atrium. Reply with the single word OK. Do not use tools. Do
not continue any task." The hook file is written by atrium into its own state directory, never into the card's
project or Claude Code's settings. With `--setting-sources local` the only settings the fork loads are that file
and the card's `.claude/settings.local.json`. The local file can carry hooks too, so **a card whose
`.claude/settings.local.json` has a `hooks` key is skipped** as `skipped: local hooks`. Atrium reads that file and
never writes it. `ATRIUM_PERM_GATE=off` stays in the fork's environment as well, so that if a later Claude Code
release loads atrium's hooks some other way, they still do nothing.

The write in probe 5 (6,175 tokens, 4%) is the card's transcript tail since its last request, which in that probe
included the author's turn in progress. On an idle card the tail is its last reply and the prompt, usually under
2k tokens, or $0.016 on Opus 5.5.

One oddity: `total_cost_usd` in probe 4 said $0.29 where the tokens price at about $0.02. The ledger prices from the
receipt's token counts and atrium's own price table, never from `total_cost_usd`.

### The choice: option 3, with option 2 as the fallback

Option 3 changes nothing about the card. No proxy, no credential handling (Claude Code refreshes its own token),
no request bodies held, tool search and Remote Control untouched. What it costs over option 2 is a process start
per refresh (a few seconds and a few hundred MB while it runs), the transcript tail written (under 2k tokens on an
idle card) and about 100 output tokens. That is under $0.02 a refresh on Opus 5.5.

Its one real risk is probe 2: a fork that is not held to one tool-less turn is a copy of the agent acting on the
card's behalf. So the hook file, `--max-turns 1` and the receipt check below are not options. The receipt check
treats more than one turn or any tool use as a failure and stops keep-alive on the card.

The fallback, if forks stop hitting the cache in some later Claude Code release, is option 2 with
`ENABLE_TOOL_SEARCH=true` and Remote Control lost. That is a decision for clint and it is not built without one.

### What the fork has to match

The prefix is exact, so the fork must reproduce everything that is part of the cache key:

- **Directory.** The card's working directory. The cache is scoped to it.
- **Model.** The model of the card's last main reply, from its transcript (`message.model`), plus the 1M context
  variant when the card's telemetry says the session has a 1M window. A different model is a different cache and
  the fork would write the whole context.
- **Effort.** On Opus 5.5 and Fable 5.1 a change of effort keeps the cache. On other models it does not, and atrium
  does not know a card's current effort. So **only Opus 5.5 and Fable 5.1 cards are refreshed**. Others are
  skipped as `skipped: model`. This covers nearly all of clint's use since the report.
- **Fast mode.** A header in the cache key. A card whose last main reply has `usage.speed: "fast"` is skipped.
- **Environment.** The card's own environment, with `ATRIUM_PERM_GATE=off` so the fork's hooks do not reach atrium
  and do not appear as a second session on the board.
- **Launch args.** The fork does not carry the card's launch args. A card launched lean (tag `atrium:lean`, see
  `docs/lean-workers-design.md`) runs with its own `--disallowedTools`, `--mcp-config`, `--append-system-prompt`
  and setting sources, so its tool list and system prompt are not the fork's, and the prefix differs from the first
  token. **Lean cards are skipped** as `skipped: lean card`. Found 2026-09-28 (backlog-2 item 70): the only two
  real misses in the ledger were the two lean cards. sa55's read 0 of 123,752 and wrote 127,952 at the 1h price,
  $1.02 against a $0.12 budget. The other read 10,259 of 99,886.

### Reading the receipt

`--output-format json` returns `subtype`, `num_turns`, `permission_denials` and the usage. The receipt is
classified in this order, and the first match wins:

1. **`acted`**: `num_turns` above 1 with `permission_denials` empty. The model took a second turn and nothing
   refused the first one, so a tool may have run. This is the probe 2 failure. The card goes to `stopped: acted`
   and keep-alive is SUSPENDED for the whole room (below).
2. **`refused`**: `permission_denials` not empty. The model tried a tool and atrium's hook refused it (probe 4
   reads `turns=2 denials=1 subtype=error_max_turns`). Nothing ran, but a card whose conversation pulls the model
   into acting on a refresh will do it again, so the card goes to `stopped: acted` and stays there until a person
   turns it back on. The room is not suspended: the safety net did its job.
3. **`failed`**: a non-zero exit, a timeout (120 s), no usage, or `subtype` other than `success`. Not counted as a
   refresh. See "Failure handling".
4. **`warmed`**: `cache_read_input_tokens` at least 90% of the card's context C. There is no write threshold. The
   first version also asked for `cache_creation_input_tokens` under 5% of C, but a fork always writes its own tail
   (the card's last reply and the prompt), and on 2026-09-28 two forks that read 99.99% (54,772 of 54,774, and
   154,894 of 154,896) wrote 6.4% and 5.006% and were stopped as misses.
5. **`miss`**: anything else.

Only `warmed` extends the card's warm window. Every outcome that reached the API (all but a `failed` with no
usage) is priced into the budget, because it was paid for whatever it achieved.

**The budget cannot stop a miss.** The check before a fork prices it as a read of C, which is what a warm fork
costs. A miss writes all of C at the 1h price instead, which is eight budgets by the stop rule's own arithmetic,
and nothing before the request can tell the two apart. What bounds it is that one miss stops the card, and two on
two cards suspend the room. So a card whose prefix the fork cannot rebuild must be skipped before it is forked
(lean cards, above). The card's view counts misses (`missed`) apart from warmed refreshes (`refreshes`), so the
tooltip on a card stopped by a miss says what the spend bought.

### Which session a receipt belongs to

The fork has a session id of its own, and it must never be mistaken for the card's:

- The card is found by its task id, and its Claude session by the card's `resume_id`, the same field telemetry
  matches on. The ledger row is keyed on the task id and records that `resume_id`. The fork's `session_id` from
  the receipt is written to the row for debugging and read by nothing.
- The card's context C, model, speed, TTL and the time of its last real reply come ONLY from the card's own
  transcript (found from `resume_id`). The receipt never updates them.
- The receipt may change exactly three things: the ledger row, the card's keep-alive state, and the card's warm
  window, whose end becomes the time the refresh was sent plus the card's TTL. That last one only on `warmed`.
- The fork cannot reach atrium any other way. `--no-session-persistence` means it leaves no transcript (checked
  after probes 1 to 7: the projects directories hold no fork transcripts, and the originals' transcripts were not
  written). `ATRIUM_PERM_GATE=off` and `--setting-sources local` mean no atrium hook fires. A print-mode run draws
  no status line, so it posts no telemetry, and a telemetry post for an unknown session id is dropped anyway.
- A real turn is recognised from the card's own hooks (`UserPromptSubmit` or a new `Stop` for the card's task id),
  never from anything the fork does.

### What each outcome does to the card

A `miss` means the fork wrote a prefix the card will not use. It turns keep-alive off for that card (state
`stopped: miss`) until its next real turn, because a second try pays the same write. Two misses on different cards
in a row, or one `acted` anywhere, SUSPEND keep-alive for the whole room and raise a toast, because at that point
the mechanism is wrong and not the card. Suspended means no card is refreshed until somebody clears it in the
settings cog. It is separate from the board switch, which only sets the default for new cards and so could not
stop the cards that already exist. The suspension is persisted, so a restart does not clear it.

### Where this sits against "never inject"

CLAUDE.md lists injecting prompts into running Claude sessions from outside MCP as out of scope, and `peers.go`
describes the one path that types. This design injects nothing into a running session. The card's conversation,
transcript and terminal are untouched, and the card's model sees no new message. It runs a separate, throwaway
session that reads the card's cache. It stays inside the existing line.

It is still a model turn on a copy of the card's conversation, run by atrium without anybody asking. That is why
the copy may not use a tool, may take one turn only, and persists nothing, and why each of those is checked on
the receipt rather than trusted.

## Choosing the TTL

Claude Code's prompt caching page ("Choose the TTL yourself") offers two TTLs and no others: `5m` and `1h`. The
main conversation gets `1h` on a subscription within plan usage and `5m` everywhere else, including a subscription
drawing on usage credits. Subagents, compaction and titles get `5m`. `CLAUDE_CODE_PROMPT_CACHE_TTL` (or the
`promptCacheTtl` setting) chooses the main one, and `CLAUDE_CODE_SUBAGENT_PROMPT_CACHE_TTL` chooses the rest.

Choosing the TTL is not an alternative to keep-alive. The longest TTL is one hour, and the resume data below shows
85% of stretches that pass one hour also pass two. It is part of the design in one place:

- **Pin the main conversation to `1h` on every card with keep-alive on**, by setting
  `CLAUDE_CODE_PROMPT_CACHE_TTL=1h` in its launch environment. Without it, an account that runs past its plan
  limit drops every card to `5m`. On `5m` every pause over 5 minutes is a cold wake, and a keep-alive would need 12
  refreshes an hour. The pin costs 0.75x input more per written token (2x against 1.25x). A card writes about its
  final context size over its life, so the pin pays for itself the first time the card pauses between 5 and 60
  minutes, which almost every card does. The fork inherits the card's environment, so it writes at the same TTL.
- **Leave the subagent bucket alone.** A subagent sends its requests back to back and rarely pauses 5 minutes, so
  `1h` there is the 0.75x premium on every subagent write for nothing.
- **Do not run keep-alive on a 5 minute cache.** One idle hour costs 12 reads on `5m` against 1 read on `1h`. At
  Opus 5.5 prices that is $2.40 against $0.20 per million tokens of context per hour. If a card is on `5m` anyway
  (its last reply wrote `ephemeral_5m` tokens only), it is skipped as `skipped: 5m cache`.

## The arithmetic

### Prices

Per million tokens, from `$UsagePrices` in `dotfiles/claude/usage/UsageCommon.ps1`. W is the 1 hour write, R the
cache read.

| model | W | R | R / W | W/R - 1 |
| --- | --- | --- | --- | --- |
| Opus 5.5 | $8.00 | $0.20 | 1/40 | 39 |
| Fable 5.1 | $20.00 | $0.25 | 1/80 | 79 |
| Opus 5 | $10.00 | $0.50 | 1/20 | 19 |
| Sonnet 5 | $4.00 | $0.20 | 1/20 | 19 |

For a context of C tokens, a wake onto an expired cache costs `C x W`. A wake onto a warm one costs `C x R`. One
refresh costs `C x R` plus about 300 tokens of overhead. On Opus 5.5:

| context | one cold wake | one refresh | 5 refreshes |
| --- | --- | --- | --- |
| 100k | $0.80 | $0.02 | $0.10 |
| 200k | $1.60 | $0.04 | $0.20 |
| 430k | $3.44 | $0.09 | $0.43 |

Refreshes fire about once an hour (5 minutes before expiry), so N refreshes keep a card warm for N hours after the
first hour runs out.

### How often an idle card comes back

From Claude Code's transcripts for the last 21 days (284 files, 92,640 main-thread replies, subagents excluded).
An idle stretch is the gap after a main reply on a context of 50k or more. 597 stretches passed one hour. 347 of
them ended in a resume and 250 were still open when measured. The hazard is the share of cards still idle at the
start of the hour that resume inside it.

| idle hour | still idle | resumed in the hour | hazard | still idle at start |
| --- | --- | --- | --- | --- |
| 1 to 2 | 597 | 88 | 14.7% | 100% |
| 2 to 3 | 509 | 29 | 5.7% | 85% |
| 3 to 4 | 480 | 28 | 5.8% | 80% |
| 4 to 5 | 452 | 19 | 4.2% | 76% |
| 5 to 6 | 433 | 13 | 3.0% | 73% |
| 6 to 7 | 420 | 5 | 1.2% | 70% |
| 7 to 8 | 415 | 9 | 2.2% | 70% |
| 8 to 9 | 406 | 14 | 3.4% | 68% |
| 9 to 12 | 392 | 20 | 1.7% a hour | 66% |
| 12 to 24 | 372 | 68 | 1.7% a hour | 62% |
| 24 to 30 | 302 | 5 | 0.3% a hour | 51% |

The median resumed gap is 5.7 hours. Half of all stretches past one hour are still idle a day later.

### When a refresh stops paying

A refresh at the start of idle hour t is paid only if the card is still idle then. It pays off only if the card
resumes inside that hour, when it saves `W - R` (a read instead of a write). So one more refresh is worth it while

```
hazard(t) x (W - R) > R        that is        hazard(t) > R / (W - R)
```

| model | hazard it takes | last hour above it | best N |
| --- | --- | --- | --- |
| Opus 5.5 | 2.6% | hour 5 to 6 (3.0%) | 5 |
| Fable 5.1 | 1.3% | about hour 23 | about 23 |
| Opus 5 | 5.3% | hour 3 to 4 (5.8%) | 3 |
| Sonnet 5 | 5.3% | hour 3 to 4 (5.8%) | 3 |

The hazard falls with time, so stopping at the first hour below the line is the best rule. Summing the expected
saving over all stretches that pass one hour, per million tokens of context:

```
EV(N) = sum over t = 1..N of  still_idle(t) x ( hazard(t) x (W - R) - R )
```

| model | N = 2 | N = 3 | N = 5 | N = 8 | N = 10 | N = 20 | best |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Opus 5.5 | $1.16 | $1.36 | **$1.48** | $1.43 | $1.34 | $0.89 | $1.48 at 5 |
| Fable 5.1 | $3.40 | $4.12 | $4.81 | $5.21 | $5.31 | $5.71 | $5.92 at 23 |
| Opus 5 | $0.93 | **$0.97** | $0.74 | $0.15 | -$0.30 | -$2.37 | $0.97 at 3 |
| Sonnet 5 | $0.37 | **$0.39** | $0.30 | $0.06 | -$0.12 | -$0.95 | $0.39 at 3 |

For a 300k Opus 5.5 card that goes idle, stopping at 5 refreshes is worth $0.44 on average against no keep-alive.

### The stop rule, and whether one half is right

clint's starting rule: stop once the keep-alive spend since the card went idle reaches half of one full cache
rehydration, `f x C x W` with `f = 1/2`. Spend is `N x C x R`, so a budget f allows `N = f x W / R` refreshes.

| model | refreshes at f = 1/2 | EV at f = 1/2 | refreshes at f = 1/8 | EV at f = 1/8 | best EV |
| --- | --- | --- | --- | --- | --- |
| Opus 5.5 | 20 | $0.89 | 5 | $1.48 | $1.48 |
| Fable 5.1 | 40 | about $5.8 | 10 | $5.31 | $5.92 |
| Opus 5 | 10 | -$0.30 | 2 | $0.93 | $0.97 |
| Sonnet 5 | 10 | -$0.12 | 2 | $0.37 | $0.39 |

**One half is too high.** It keeps paying 15 hours past the point where refreshes stop earning on Opus 5.5 and
gives back 40% of the saving. On Opus 5 and Sonnet 5 it loses money outright. The budget that fits the data is
**one eighth** of a rehydration:

- It lands on the best N for Opus 5.5 exactly (5).
- It gets 96% of the best for Opus 5 and Sonnet 5 and 90% for Fable 5.1, with one number for every model.
- It is the right shape. A spend budget scales the refresh count by W/R, which is what the hazard line does as
  well. A fixed refresh count does not.

So the stop rule is spend-based as clint proposed, with `f = 1/8` as the default. It is a daemon setting, and the
build ships the transcript script that produced the table (`scripts/keepalive-hazard.ps1`) so the number can be
re-derived as the data grows.

Two limits on these numbers. The stretches are all Claude Code sessions on this machine, not only board cards, and
a 250-stretch open tail is right-censored (some of those will still resume). Both push the true hazard slightly up,
which moves the best N up slightly and makes 1/8 a little conservative, not wrong.

On the subscription meter, the report's single-point estimate is $17.10 per 1% of the weekly limit. Five refreshes
of a 300k Opus 5.5 card are $0.30, about 0.02% of the week.

## Policy

A card is refreshed when ALL of these hold:

1. Keep-alive is on for the card (see "Controls").
2. The card runs Claude, on Opus 5.5 or Fable 5.1, not in fast mode, on the `1h` cache, and atrium knows its
   session id and directory.
3. The card is idle: its Stop hook has landed since its last prompt, and no permission dialog is open.
4. Its cache is still warm. Expiry is the time of the last reply that read or wrote the prefix (the card's own last
   main reply, or atrium's last `warmed` refresh) plus one hour. A cold prefix is never refreshed: that is a write.
5. Expiry is less than 5 minutes away.
6. The context is at least 50k tokens. Below that a saved wake is worth under $0.40.
7. The spend since the card went idle is under `f x C x W` (the budget, f = 1/8 by default).

Context C and the card's model come from the card's transcript, which atrium already finds from the session id
(`internal/api/sessions.go`): the last main reply's `message.model`, `timestamp`, `usage.speed`,
`usage.cache_creation` and `input + cache_read + cache_creation` tokens.

### At break-even

When the next refresh would take the spend past the budget, atrium does not send it. Instead it:

- sets the card's keep-alive state to `stopped: break-even`,
- raises a toast through the board's toast path, logged like every other toast: "keep-alive stopped on <card>
  at break-even after 5 refreshes, $0.30",
- shows a status icon on the card, a small snowflake, whose tooltip reads "keep-alive stopped at break-even: 5
  refreshes, $0.30 of a $0.30 budget, cache went cold at 17:42".

A new real turn on the card (its next prompt) clears `stopped` back to on and starts a fresh budget. So does
turning the per-card switch on by hand. A card turned OFF by hand stays off whatever happens.

### Failure handling

- The fork exits non-zero, times out (120 s) or returns no usage: `failed`, no refresh counted. Two in a row on a
  card stops it (`stopped: failing`) until its next real turn.
- A miss: as in "Reading the receipt".
- Daemon restart: nothing is lost that matters. The budget state is in the database, and the transcript still says
  when the cache was last touched. A card that went cold while the daemon was down is simply skipped by rule 4.

## Controls

clint's requirements, which override the first brief:

- **Board switch**, in the board settings cog: "Keep idle sessions' caches warm". **Default ON.** It is the
  default for NEW Claude cards only. Turning it off or on does not change a card that already exists. It is a room
  setting (`cache_keepalive_default` in the room's setting table), like `unexpected_exit_wake`, because the room is
  what launches a card and reads the default at that moment. Unset reads as on.
- **Per-card switch**, in the card's menu. Each new Claude card takes the board setting at launch. After that the
  card's own switch is the only thing that decides. Greyed with a reason when the card cannot be warmed (not
  Claude, a model outside rule 2, no session id).
- Launch environment: a card launched with keep-alive on gets `CLAUDE_CODE_PROMPT_CACHE_TTL=1h` (see "Choosing
  the TTL"). A card turned on later that launched without the pin is still refreshed while its cache is `1h`, and
  skipped when it is `5m`.
- **The ledger.** Every fork is a row: card, the card's `resume_id`, the fork's session id, time, model,
  outcome (`warmed`, `miss`, `refused`, `acted`, `failed`), cache read, cache write, input and
  output tokens, and cost from a price table in the daemon mirroring `$UsagePrices`. The row names the price
  table's version (the date it was copied), so a stale table shows and the raw token counts let a later reader
  reprice it. Each row also carries the inputs of the decision: the context C, the seconds left on the TTL,
  the budget spent before this attempt and the budget cap, so a surprising stop can be explained from the row.
  A skip is not a row. The daemon keeps the last skip reason per card in memory and shows it on the card, so the
  `skipped: <why>` names in this doc are those reasons, and they are gone after a restart. If a fork's row fails to
  save, the daemon holds that card's refreshes in memory until the cache the fork may have warmed would expire, or
  until the card takes a real turn, and the card shows why.
  Persisted, because the point is to see what it cost over a week. Every card with the switch on wears a chip:
  a dotted `◎ watching` while nothing has been refreshed yet, with the last skip reason and the warm-until time in
  its tooltip, `❄ warm` once it has been, and a dashed `❄ cold` once it stopped. The warm tooltip shows the current
  idle stretch (`kept warm 3x, $0.18 of $0.30`). The settings cog shows the week's refresh spend next to the switch, and the
  suspension with its reason and a button to clear it when there is one.
- Nothing from the conversation is in the ledger: no prompt text, no reply text. The fork's reply is discarded.

The card state is one of `on`, `off` (by hand), `stopped: break-even`, `stopped: miss`, `stopped: failing`,
`stopped: acted`. The first three `stopped` states clear themselves on the next real turn. `stopped: acted` (from
`acted` or `refused`) does not: a fork that tried to act is a bug to look at, and the card stays stopped until it
is turned on by hand.

## Runners

- **Claude.** Yes, as above.
- **Codex.** No. Codex talks to OpenAI's Responses API, where caching is automatic, there is no write premium (a
  cold prefix is billed at the plain input price) and the client cannot set or refresh a TTL: OpenAI keeps a cache
  for minutes of inactivity at its own discretion. With no premium write to avoid and no expiry to read, the stop
  rule above has nothing to stand on. If codex gains a client-controlled retention, it is a separate item.
- Anything else: no. The per-card switch is greyed.

## Build order

1. **Interactive check.** Done: probes 5 and 7 read 96% and 98% of a real interactive PTY card's context in one
   tool-less turn. The test plan repeats it by hand on an idle card after the build.
2. Store: the two settings, the per-card state, the ledger table.
3. Daemon: the tick, the fork, the receipt, the budget, the toast event. Tested with a fake runner (a stub `claude`
   that prints a canned JSON receipt) and a fake clock.
4. Board: the cog switch, the card switch, the stopped icon and tooltip, the toast. A headless board test.
5. Test-plan section, CHANGELOG, backlog-2 item.

The daemon half needs a room restart to deploy. The board half is served with the room's board as well, so the
whole change is a room restart, not a hub-only deploy.

## What is not in this design

- Compacting a big card before it idles, which the report ranks as the bigger saving. That is the agent's decision.
- Refreshing a busy card. A busy card keeps itself warm.
- A refresh on a 5 minute cache, or on a model whose effort is part of the cache key.

## Tests the build owes

- Go, with a stub runner and a fake clock: each of the seven rules blocks a refresh on its own. The fork is run in
  the card's directory with `--resume`, `--fork-session`, `--no-session-persistence`, the card's model,
  `--setting-sources local`, the block-all-tools `--settings` file, `--max-turns 1` and `ATRIUM_PERM_GATE=off`. A
  card with hooks in its `.claude/settings.local.json` is skipped. Receipts are classified in the documented order:
  `acted`, `refused`, `failed`, `warmed`, `miss`. The receipt never changes the card's C, model or last real reply.
  The budget stops at f and raises exactly one toast. A real turn clears `stopped` and resets the budget. A hand OFF
  survives a real turn. `stopped: acted` survives a real turn. Two misses on two cards, or one `acted`, suspend the
  room, and a `refused` does not. A new card takes the board default. Changing the board default leaves existing
  cards alone.
- Headless board test: the cog switch, the per-card switch and its greyed state, the stopped icon and its tooltip
  text, the toast in the toast log.
- Test-plan section for the manual run on a real card, which is also build step 1. Its result is written down in
  the test plan: the command, the card's runner and launch mode, the model, the context, `cache_read_input_tokens`,
  `cache_creation_input_tokens`, and pass or fail against the 90% read threshold.
