# Rules an agent can check cheaply: guard blocks for what is checkable, and `atrium rules ask` for the rest

Status: design, 2026-10-14. Nothing here is built. Written by @rnd for backlog `r-rules-ask`. For clint's review before
anything is built, and the build is a trial first.

## The problem

clint, 2026-10-14 16:23, in the backlog item: "what i really want is some quick thing you can call quickly to get
answers that doesn't require slurping up your context. let's try it, see how it goes."

The item's own plan:

- "Checkable rules go into internal/guard rules as hard blocks (first one: a reply that hands clint a hub deploy command
  is blocked by a Stop hook)."
- "Everything else: one call, e.g. `atrium rules ask "<question>"` and an MCP tool, answered by a small fast model over
  the orchestrator memory dir, AGENTS.md files and guard rules. Returns one line: the rule, its why, the file it came
  from. Cost and latency measured."
- "Trial: the orchestrator uses it for a few days. Measure context saved and wrong answers."

Two different things are folded together, and the design keeps them apart. A rule that a program can CHECK should be
enforced, not asked about. A rule that only a reader can apply (a judgement written in a memory file) is what the ask is
for.

## What exists today

- **The guard is a PreToolUse deny hook with rules as data.** `internal/guard/rules.json` (27 rules, embedded) names a
  check from the table in `internal/guard/checks.go` and carries its tools, its params and the sentence the model reads
  (`Rule` and `Params` in `internal/guard/guard.go`). `Evaluate` runs the rules in order and the first match answers.
  Load is strict, and a broken built-in set denies every Bash, PowerShell, Write, Edit and Agent call, so a test loads it
  (`internal/guard/AGENTS.md`). It is off unless `ATRIUM_GUARD` says on. It runs as `atrium hook --event pre-tool-use`
  (`internal/cli/hook_guard.go`).
- **The guard sees a tool call and nothing else.** `Input` is `tool_name`, `cwd` and the tool's `command`, `file_path`,
  `content`, `new_string` and `subagent_type` (`guard.go:124`). It has no field for what the model said. No rule can read a
  reply today.
- **The Stop hook is where a reply is visible.** `atrium turn --event end` is Claude Code's Stop hook (`internal/cli/turn.go`).
  Its payload struct reads `last_assistant_message` (`LastAssistantMessage`, `turn.go:80`), `stop_hook_active` (true when
  the turn is only still running because a Stop hook blocked the last one, and "blocking again would never terminate"),
  `transcript_path` and `background_tasks`. It answers `{"decision":"block","reason":"..."}` to make the model keep
  working, and `turnAnswer` validates that answer, so a truncated read or an error page never becomes a block. Today the
  only thing it reads out of the message is the Open Questions block, and the comment says "Only the questions leave
  this process, never the message they came from" (`turn.go:222`). The Stop hook is optional and is never installed by
  "install all" (`internal/claudeconf/hooks.go`, the comment above `WantedHooks`), because it is the one hook that can
  change what a session does.
- **A model call is a subprocess of `claude`, today.** The one place atrium calls a model is keep-alive, which runs
  `claude -p ... --model <m> --no-session-persistence --setting-sources local --settings <file> --max-turns 1
  --output-format json` (`internal/daemon/keepalive.go`, `forkArgs`, line 910). Atrium holds no API key and makes no
  Messages API call anywhere (`AGENTS.md`, "atrium may hold the NAME of a command that has a credential, and never
  somebody else's credential", and `internal/runnersetup/claude.go` only reads `ANTHROPIC_API_KEY` from the environment
  a runner was launched with).
- **The control MCP is where an agent's tools live.** The hub's MCP adds tools such as `atrium_backlog`, `atrium_reports`
  and `atrium_ci` (`internal/link/backlog_mcp.go`, `ci_door.go`), and `atrium control` is the stdio server a session
  registers (`internal/cli/control.go`). Each tool is classed by `addTool(s, class, ...)`.
- **The corpus is large and mostly prose.** The `AGENTS.md` files under `internal` and the root total 85,793 bytes across
  eleven files (about 21,000 tokens by the rough four bytes a token). `internal/guard/rules.json` is 9,531 bytes. The
  orchestrator's memory directory is a folder of Claude Code's per project memory under `~/.claude/projects/<project>/`,
  and its size is not measured here, since it lives with the orchestrator's session. Step 1 of the trial measures it.
- **Name collision.** The board already calls the permission gate's standing allow and deny entries "rules"
  (`/v1/rules` in `internal/api/api.go`, the `perm_rule` table). `atrium rules ask` would be about the written rules a
  person keeps, and the help text has to say which.

### Measured here, for the cost and latency the item asks for

One run each on sg4 (Windows), 2026-10-14, `claude -p "reply with the single word ok" --model haiku --max-turns 1
--no-session-persistence --output-format json`:

| invocation | wall clock | API time | tokens in (cache write + read) | cost |
| --- | --- | --- | --- | --- |
| `--setting-sources local` | 4.8 s | 1.15 s | 25,123 | $0.0018 |
| plus `--tools "" --system-prompt "Answer in one word." --disable-slash-commands` | 4.5 s | 0.65 s | 4,802 | $0.0010 |

So a `claude -p` call has a floor of about 4.5 seconds, of which most is the CLI starting (about 3.8 s), and the model
itself answers in about a second. A one off is not a distribution, and the trial measures the real one. It does say two
things already: the process start is the cost, not the model, and the fixed prompt can be cut from 25,000 tokens to
under 5,000 with flags.

## Options

### A. Guard hard blocks only, no ask

Everything checkable becomes a rule, and the rest stays where it is. Zero cost, deterministic, and it does not touch the
"slurping up context" problem for the judgement rules, which is most of what a memory directory holds.

### B. Local retrieval, no model

`atrium rules ask` splits the corpus into chunks (a heading and what is under it, a memory file's paragraph, a guard
rule), ranks them against the question with a plain keyword scorer (BM25), and prints the top three with their file and
line. Milliseconds, free, no credential and no failure mode. Costs the reader about 300 tokens instead of 20,000, but the
answer is passages the asker still has to read, and a keyword scorer misses a question phrased in other words than the
rule.

### C. Retrieval, then a small model that answers in one line (recommended)

B narrows the corpus to the top ten or so chunks (about 2,000 tokens), and a small model (Haiku, through `claude -p`
with the lean flags above) turns them into the one line the item describes: the rule, its why, the file it came from.
Retrieval keeps the prompt small and the cost flat as the memory grows, and the model fixes B's weakness, a question
phrased unlike the rule. The model's answer must carry a verbatim quote from a named chunk, and the CLI checks that the
quote really is in that file, so an invented rule is caught before it is returned (see "The answer").

### D. The model reads the whole corpus every call

Simplest to build, and wrong at this size: 20,000 tokens of AGENTS files plus the memory directory on every call, so the
call costs what it was meant to save, and prompt caching only helps a call made inside the cache's lifetime.

### E. A direct Messages API call instead of `claude -p`

About one second a call against 4.5. It needs an API key in the caller's environment, which atrium never stores, and a
new HTTP client in the one binary. A reasonable later engine, behind a flag, if the trial says latency is the problem.
It is not the first build, because the first build should not add a credential path.

## Recommendation

**Two parts, in this order.**

1. **Guard blocks for what is checkable, including a reply check on the Stop hook** (A, built first because it is small
   and deterministic). The first rule is the one in the backlog item, and the same mechanism takes later ones.
2. **`atrium rules ask` as retrieval then Haiku (C), an MCP tool over the same code, and a trial with the measures
   below.** The engine is `claude -p` first (E is a flag later), and it falls back to printing the retrieved chunks
   (B) when `claude` is not on the machine or the call fails, so the command is never an error that blocks an agent.

### Part 1, the reply check

- The guard's `Input` gains `LastAssistantMessage string \`json:"last_assistant_message"\``, and `Evaluate` accepts a
  tool name `reply` for a Stop payload. A new check `reply-pattern` (in `checks.go`, in the table) matches
  `Params.Patterns`, which `Load` already compiles into `Rule.patterns`, against that text, with `Params.Except` for
  strings that make it fine. Rules name `"tools": ["reply"]`. A rule that names `reply` and a tool call check in the same
  set is an error at load, so the two never mix.
- It is evaluated in `turnEnded` in the CLI process, before anything is posted. That keeps the stance in `turn.go:222`:
  the message does not leave the process, because the check runs where the message is.
- **It FAILS OPEN, unlike the PreToolUse guard.** A Stop that blocks keeps a session working, so a guard that fails closed
  here is a session that never stops. A broken rule set, a panic or an unparseable payload answers "nothing to say"
  (`keepGoing`), logs once, and the session ends as before. This is the opposite posture to `internal/guard/AGENTS.md`'s
  first rule, and the doc there needs one paragraph saying why.
- **It blocks at most once per turn.** `stop_hook_active` true means the model is answering the block, so the check
  answers "nothing to say" then, whatever the text holds. That is the loop the comment in `turn.go` is about.
- **It corrects, it cannot prevent.** The Stop hook fires after the reply is written, so clint may already have seen the
  command. The block makes the next turn withdraw it. A rule that must never reach clint at all has no hook to ride, and
  the item's own wording, "blocked by a Stop hook", is the same limit.
- **The first rule**, id `reply-hands-deploy-command`: patterns for the deploy scripts by name
  (`atrium-deploy\.ps1`, `deploy-batch\.ps1`, `deploy-hub-only\.ps1`, `Start-Process pwsh[^\n]*deploy`, from
  `scripts/live/`) inside a code fence or a line that starts with a command, with an except for a reply that is quoting
  a path in prose. Reason: "Do not hand clint a deploy command to run. Run the deploy yourself with the deploy tool, or
  say what is needed and ask." The exact wording and the memory rule it comes from are clint's to confirm (open question
  2), and the rule's `reason` cites the memory file it was written from.
- **Opt in, as every Stop hook is.** The board's hook installer offers it by name and never in "install all". Without
  the Stop hook registered the rule does nothing, and `ATRIUM_GUARD` must be on as for every guard rule.

### Part 2, `atrium rules ask`

```
atrium rules ask "<question>" [--json] [--raw] [--engine claude|api] [--source <path>]...
atrium rules ask --wrong <id> [--note "..."]
atrium rules index          prints what is searched, how many chunks, and how big
```

- **What is searched, by default.** (1) The memory directory of the caller's project, found from the working directory the
  way Claude Code names it. (2) Every `AGENTS.md` from the working directory up to the repository root, and each
  package's own under `internal/`. (3) The guard's built-in rules, `rules.json`. `--source` adds paths, and an
  `[rules_ask]` section in the room's settings can set the memory directory for a card that runs elsewhere. Nothing is
  read from outside these paths.
- **Chunks.** A heading with its body, or a guard rule (`id`, `reason`), or a memory file split at blank lines. Each
  carries its file and line.
- **The answer is one line plus its proof**:

  ```
  Never hand clint a deploy command. Run it yourself: he cannot run it from inside a supervised shell.
    why: stopping the room ends the shell that runs the script (scripts/live/atrium-deploy.ps1 header)
    from: memory/feedback_deploy.md:4   [verified]
  ```

  The prompt asks the model for JSON: `{"answer", "why", "file", "quote"}` or `{"answer": null}` when no chunk holds a
  rule that bears on the question. The CLI then checks that `quote` appears in `file` (whitespace folded). `[verified]` or
  `[unverified: quote not found]` is printed with every answer, and an unverified one is the one to doubt. A `null` prints
  `no rule found. nothing in <n> files bears on this`, which is a real answer, and never a guess.
- **The prompt tells the model it is a lookup, not an adviser.** It answers from the chunks given, says when they do not
  cover the question, and never invents a rule. Lean flags as measured above: `--tools ""`, a short `--system-prompt`,
  `--disable-slash-commands`, `--setting-sources local`, `--no-session-persistence`, `--max-turns 1`, a fixed small model,
  with `ATRIUM_GUARD` and the permission gate off in that child so it neither asks nor loops.
- **No credential is held.** `claude -p` uses the machine's own login. `--engine api` reads `ANTHROPIC_API_KEY` from the
  caller's environment when it is set and stores nothing, as `runnersetup` does.
- **The MCP tool** `atrium_rules_ask {question}` on the control MCP is a thin caller of the same function and returns the
  same JSON. It is read only, so it is classed with the other read tools. It runs on the machine of the room the card is
  on, because that is where the memory directory is.
- **Every ask is logged** to `rules-ask.log` (JSON lines) in the room's directory: time, question, answer, file, verified,
  engine, wall time, tokens, cost, the ids of the chunks sent. The log is what the trial reads, and it stays on the
  machine.

## The trial

Five working days, the orchestrator (sg4-control) as the only user. The orchestrator's instructions get one line: before
reading a memory file or an AGENTS file whole to find a rule, ask first.

What is measured, all from `rules-ask.log` and one weekly read of it:

| measure | how | what would call it a success |
| --- | --- | --- |
| latency | wall time per ask, p50 and p95 | p95 under 6 s on `claude -p`. Under 2.5 s would justify the API engine |
| cost | tokens and dollars per ask, and the day's total | under a cent an ask, and the day's total smaller than one whole AGENTS read |
| context saved | for each verified ask, the size of the cited file's section the orchestrator would otherwise have read, minus the answer's tokens | net positive over the five days, counted per ask |
| wrong answers | `[unverified]`, plus every `--wrong` the orchestrator files after finding an answer false or stale | under 5 percent of asks, and none that led to an action before it was caught |
| misses | twenty asks chosen at random, each checked by reading the corpus for a rule the answer missed | under 10 percent |
| coverage | share of asks answered `no rule found` | informational. High means the rules are not written down |
| the reply guard | each block, whether it was right, and the extra turn it cost | zero false blocks that cost more than a turn, and the rule caught what it was written for |

The orchestrator files a wrong answer with `atrium rules ask --wrong <id>`, which marks the log line, so the count does
not depend on anyone remembering. At the end of day three there is a short look at the numbers, with a decision to
continue, change the engine, or stop. At the end of day five, the same look decides whether it becomes a standing tool.

The baseline for "context saved" is stated before the trial begins: how much the orchestrator loads from memory and AGENTS
files at the start of a typical session, and how many times it re-reads a file to find one rule. Step 1 below measures
that, because without it the saved figure has nothing to be compared to.

## Build plan

Each step lands alone with its tests, and the trial does not start until step 7.

1. **Measure the corpus.** `atrium rules index` (read only): the files it would search for the caller's directory, the
   chunk count and the token estimate. Run on sg4-control, it gives the memory directory's real size and the baseline
   above. Tests in `internal/rulesask/` with a temp directory.
2. **Chunking and retrieval**, package `internal/rulesask`: `Chunk(sources) []Chunk` (file, line, text), `Rank(chunks,
   question, k) []Chunk` with BM25, stop words and a small synonym-free tokenizer. Table tests: a question whose words are in
   the rule, one that is phrased differently (documenting the keyword limit), an empty corpus.
3. **`atrium rules ask --raw`** prints the top chunks with file and line, no model. This is option B and the fallback. A
   `rules` command in `internal/cli/` (read the name collision above, and put the word "written" in its help).
4. **The model call and the verified quote.** `Ask(ctx, question, chunks, engine)` runs `claude -p` with the flags above,
   parses the JSON, verifies the quote against the file, and returns the answer or a fallback to step 3's chunks on any
   error or a timeout of eight seconds. The child's command comes through a seam like `internal/guard/env.go`'s `Env` so a
   test answers it with a canned reply. Tests: verified, unverified, null, a model returning prose that is not JSON, `claude`
   missing, a timeout.
5. **The log and `--wrong`.** `rules-ask.log` writer (bounded, rotated at 1 MiB, one old file), the `--wrong` marker, and a
   `--stats` view that prints the table of measures from the log.
6. **The MCP tool.** `atrium_rules_ask` on the control MCP in `internal/link/`, classed read only, with a test that it
   returns the CLI's JSON. Its description tells a model to use it before reading whole rule files.
7. **The reply guard** (Part 1): `Input.LastAssistantMessage`, the `reply` tool and the `reply-pattern` check, wiring in
   `turnEnded`, fail open and at most once, the first rule, and the paragraph in `internal/guard/AGENTS.md`. Tests:
   the deploy reply blocked, the same text in prose passing, `stop_hook_active` letting it go, a broken rule set failing
   open, a payload with no message. `go test ./internal/guard ./internal/cli -run 'Reply|Turn'`.
8. **The trial** (above), with its notes in `docs/changes/rules-ask.md` and a changelog entry.

## Risks

- **A confident wrong answer.** The worst failure of any lookup. The verified quote catches an invented rule, and does not
  catch a true quote that is stale or answers a different question. The `--wrong` count and the random miss audit are the
  guard, and the trial's success line says no wrong answer may lead to an action before it is caught.
- **Stale memory.** A memory file says a thing that stopped being true. Retrieval returns it and verifies it, since it is
  there. The answer prints the file and its line so the asker can see how old it is. Out of scope to fix here.
- **Latency is the process start.** About 4.5 seconds a call is a real pause for an agent mid thought, though an order of
  magnitude less than reading the files. If the trial says it is the problem, the API engine and a warm child are the
  two answers, in that order.
- **The reply guard blocks something fine.** One extra turn, bounded by `stop_hook_active`. The log of blocks is read in the
  trial. A rule that blocks wrongly twice is removed.
- **A Stop hook is the one hook that can change a session.** Fail open, once per turn, optional and named at install. The
  design's whole posture here is that a broken check never traps a session.
- **Another place a credential could creep in.** `--engine api` reads a key from the caller's environment. It must never
  be written to the log, the settings or the store. A test greps the log for the key.
- **The memory directory is private to a person.** The ask runs on the orchestrator's machine against its own files, and the
  log stays there. The MCP tool answers a card on that room only.

## Open questions for clint

1. Is `claude -p` with Haiku the engine to start with (about 4.5 seconds, no key held), or do you want the direct API
   call first (about one second, needs a key in the environment the ask runs in)?
2. Which memory file holds the deploy rule, and is the right pattern "any deploy script by name in a code fence", or
   broader, such as any `pwsh ... deploy` line in a reply to you?
3. `atrium rules` collides in meaning with the permission rules on the board's Rules tab. Is `atrium rules ask` fine, or
   would you rather `atrium ask-rule`?
4. Should the Stop guard be on for every session once it exists, or only the orchestrator's? This design makes it per
   settings file, like every Stop hook, and offers it by name.
5. Who files a wrong answer in the trial, the orchestrator through `--wrong`, or you when you notice one in a reply?

## Decisions (orchestrator, 2026-10-14)

- `claude -p` with Haiku first.
- The deploy rule lives in the memory files `deploy-rules.md` and `run-it-yourself-first.md`. The pattern is any deploy script
  named inside a code fence.
- Keep `atrium rules ask`, with "written" in its help.
- The Stop guard is on for the orchestrator only during the trial.
- The orchestrator files wrong answers.
- Nothing is built yet.
