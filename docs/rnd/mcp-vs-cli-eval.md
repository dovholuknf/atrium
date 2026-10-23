# atrium-control: which tools stay MCP and which become `atrium` CLI commands

Status: research, 2026-10-23. Its conclusion: keep four tools on MCP (`atrium_done`, `atrium_blocked`, `atrium_say`,
`atrium_ready`), move the rest to `atrium <verb>`, and expect the saving per session from the schemas a director loads,
not from the first request.

Backlog item `r-mcp-vs-cli-eval`. Evaluation and measurement only. No product code changed. Every scratch file is under
`D:\tmp\mcp-eval\`.

## Headline numbers

First request of a superlean card with `--system-prompt`, the seven superlean tools, in an empty directory:

| atrium-control | first request | vs no MCP |
|---|---|---|
| (1) full, 36 tools, what a director or the orchestrator gets | 6,632 | +1,237 |
| the worker class, 20 tools, what a room-launched worker gets today | 6,346 | +951 |
| (2) only the kept four | 6,081 | +686 |
| (3) no MCP | 5,395 | |

The shell tool: `Bash` costs 1,429 tokens and `PowerShell` costs 3,479, 2.4 times as much. Both together cost 4,908.

Because ToolSearch defers MCP tools, the first request barely moves: cutting 36 tools to 4 saves 551 tokens. The cost
the cut does remove is per session. A director loads each schema the first time it calls a tool, and the five tools
it calls most cost 9,062 tokens to load against 3,857 for the matching `--help` texts, or about 150 for a five-line
cheat sheet in the prompt.

## Method

As `docs/runtime/superlean-design.md` measures, with the rows it asks for here:

- Claude Code 2.1.309, model `claude-opus-5-5` (what `claude -p` picked, `modelUsage` in the result), 2026-10-23.
- `claude -p "Reply with the single word ok." --output-format json`, run from `D:\tmp\mcp-eval\empty`, a directory
  with no files, so no `AGENTS.md` or `CLAUDE.md` loads. The superlean doc's rows carried a 7.5k `AGENTS.md`, so these
  sit about that much lower.
- Flags as `leanArgs` builds them for superlean: `--setting-sources project,local`, `--strict-mcp-config --mcp-config
  <file>`, `--disallowedTools=<the 22 of leanDisallowed>`, `--tools=<list>`, and `superleanSystemPrompt` passed as
  `--system-prompt` (the minion shape, 161 tokens in `/context`). `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1`, and every
  `ATRIUM_*` variable taken out of the child's environment.
- First request is `input_tokens + cache_creation_input_tokens + cache_read_input_tokens` of the first assistant message
  in the session's transcript. The `--output-format json` total is the same number when the run takes one turn. Some
  runs with deferred tools spent a turn on ToolSearch to load `atrium_done` (the prompt tells them to end with it), so
  the transcript is the source.
- Repeated rows gave the same number to the token (`worker20` twice, `full36` twice, `kept4` twice, `none` four times,
  `Read,Edit,Bash` and `Read,Edit,PowerShell` twice each).
- The MCP server in rows (1), worker and (2) is a stub (`D:\tmp\mcp-eval\stub.js`) that replays a `tools/list` captured
  from the live hub's `/_hub/mcp`: with no `X-Atrium-Agent` for the full class (36 tools), with this card's name for
  the worker class (20 tools). The kept four are `atrium_done`, `atrium_blocked`, `atrium_say` from the worker class and
  `atrium_ready` from the stdio `atrium control` server, since the hub serves no `atrium_ready`. The live hub gave the
  same 6,632 as the stub for the full class, which checks the stub.
- Per-call costs were measured as the difference between consecutive requests in one session: the model's tool call
  and its result. Calls with side effects (`say`, `launch`, `report`) were not run live, and are sized from transcripts.

## Measured rows

### MCP

`/context` columns in thousands. "MCP tools (deferred)" is what `/context` counts for the schemas. On the wire a
deferred tool sends its name only.

| row | tools | first request | system tools | MCP tools (deferred) |
|---|---|---|---|---|
| full, 36 | superlean seven | 6,632 | 5.1 | 27 |
| worker class, 20 | superlean seven | 6,346 | 5.1 | 11.5 |
| kept four | superlean seven | 6,081 | 5.1 | 2.6 |
| no MCP | superlean seven | 5,395 | 5.1 | 0 |
| full, 36, eager | the seven less ToolSearch | 32,582 | | |
| worker class, 20, eager | the seven less ToolSearch | 17,085 | | |
| kept four, eager | the seven less ToolSearch | 8,183 | | |
| no MCP | the seven less ToolSearch | 5,395 | | |

What the rows say:

- ToolSearch plus the four kept names cost 686 tokens. Claude Code leaves ToolSearch off the wire when nothing is
  deferred, so "no MCP" is the same 5,395 with or without it on the list.
- Each deferred name costs about 17 tokens past that: 36 names cost 551 more than 4.
- Without ToolSearch the schemas load whole: 32.6k for the full set. Keep ToolSearch on the list whatever the tool
  count.
- Deferral is not free for a tool every worker calls. Loading `atrium_done` through ToolSearch cost 818 to 892 tokens
  in four runs. With the kept four eager instead, `Read,Edit,Bash` measured 6,394 against 4,292 deferred, or about
  5,180 once `atrium_done` is loaded. Deferred still wins for a worker that calls only `atrium_done`. One that also
  calls `atrium_say` (1,840 to load) about breaks even.

### The tool-set floor, no MCP

| tools | first request | price of the shell | system tools in `/context` |
|---|---|---|---|
| `Read,Edit` | 2,177 | | |
| `Read,Edit,Bash` | 3,606 | Bash 1,429 | 2.8k |
| `Read,Edit,PowerShell` | 5,656 | PowerShell 3,479 | 4.8k |
| `Read,Edit,Bash,PowerShell` | 7,085 | both 4,908 | |
| superlean seven (`Read,Edit,Write,Grep,Glob,Bash,ToolSearch`) | 5,395 | | 5.1k |
| superlean seven plus `PowerShell` | 8,874 | PowerShell 3,479 again | |

With the kept four deferred, `Read,Edit,Bash,ToolSearch` is 4,292 and `Read,Edit,PowerShell,ToolSearch` is 6,342.

The prices add: Bash plus PowerShell is Bash's price plus PowerShell's. PowerShell's description carries its own
syntax primer, the list of Unix commands it lacks, and the here-string rules, and that is the 2,050 tokens it costs
over Bash. On a Windows room, git-bash through `Bash` is the cheap shell, which is what superlean already does.

### Per call, CLI against MCP

Top five by volume, plus `atrium_peers` because it was safe to run live. "Load" is paid once a session: the MCP
schema through ToolSearch, or the CLI's `--help` read through Bash. Measured unless marked.

| tool | calls | MCP load | MCP call | CLI today | CLI `--help` | CLI call |
|---|---|---|---|---|---|---|
| `atrium_say` | 7,096 | 1,840 | about 200, est. | `atrium tell` | 539 | about 85, est. |
| `atrium_backlog` | 2,491 | 977 | 832 | `atrium backlog show` | 246 | 756 (Bash), 761 (PowerShell) |
| `atrium_ci` | 2,155 | 1,160 | about 470 median, 1,170 mean, est. | none | | |
| `atrium_launch` | 1,360 | 4,244 | about 910, est. | `atrium launch`, this room only | 1,742 | about 165, est. |
| `atrium_report` | 1,055 | 841 | about 270, est. | `atrium finish`, `atrium ask`, not the same thing | 658, 672 | |
| `atrium_peers` | 756 | 494 | 4,735 | `atrium peers` | | 1,376 |

The estimates are (median characters of the call's input + its result) / 3.5, from the transcripts below. Read them
as sizes, not measurements. What they and the measured rows show:

- **For a call, the content is the content.** A message, a brief or an item body costs the same as a JSON field or a
  shell argument. Backlog `get` is 832 tokens by MCP and 756 by Bash, 9% apart. The difference is framing and the
  result's format.
- **Output format is the real per-call lever.** `atrium_peers` answers in JSON, 4,735 tokens. `atrium peers` prints a
  table, 1,376, 71% less, while listing more sessions (38 against 30, because the two read different scopes). A CLI
  can also write a long CI log to a file for the agent to grep, where an MCP result lands whole in context.
- **The load is the session-level lever.** A director that calls each of the five once pays 9,062 tokens of schemas.
  The CLI's `--help` for the same verbs is 3,857 (no `ci` verb exists to read), and a prompt that names the five
  verbs with their arguments costs about 150 and no call at all.
- **A CLI call carries the binary's path.** The room names the binary in full because `atrium` is not on PATH in every
  room (`newContextLimitPrompt`), and it was not on PATH for this card. About 12 tokens a call, and a worker that
  guesses `atrium` and gets `command not found` pays a failed turn. Moving tools to the CLI wants the room to put the
  binary on the session's PATH first.

## Every tool

`calls` is tool calls found in this machine's transcripts (`C:\Users\claude\.claude\projects`, 2,433 sessions,
2026-09-04 to 2026-10-23, 19,763 atrium-control calls, sessions in brackets). Callers are by class (`ctlclass.go`):
**W** is in the worker set and served to every card, **D** is full class only, served to directors, the orchestrator
and human sessions. "by W" is calls from sessions whose first message named `BRIEF.md`, so it is a worker launch and
includes directors launched with a brief. Treat it as a lower bound for workers. Tools marked **stdio** are served only
by `atrium control`, the user-scope server, and not by the hub that room-launched cards talk to.

| tool | what it does | class | calls (sessions) | by W | CLI today | verdict |
|---|---|---|---|---|---|---|
| `atrium_done` | end the work: finished, with a sha or artifact | W | 816 (530) | 816 | `atrium finish` sets the card done, but reaches no launcher | **stay MCP**, floor |
| `atrium_blocked` | end the work: stopped, with a reason | W | 127 (73) | 127 | `atrium ask` sets the card waiting, but reaches no launcher | **stay MCP**, floor |
| `atrium_say` | message another session | W | 7,096 (804) | 1,036 | `atrium tell` | **stay MCP**, floor |
| `atrium_ready` | say the handoff is written, at the context limit | stdio | 0 | 0 | `atrium ready`, 361 calls | **stay MCP**, floor, and add it to the hub's worker set |
| `atrium_report` | structured report to the launcher | W | 1,055 (477) | 595 | none | drop: done, blocked and say cover its four kinds |
| `atrium_peers` | list the sessions | W | 756 (418) | 130 | `atrium peers`, 48 calls | CLI |
| `atrium_task` | one card's status and events | W | 390 (118) | 12 | `atrium task` | CLI |
| `atrium_status` | is atrium up, store health | W | 49 (29) | 10 | none | CLI |
| `atrium_alias` | read or set a card's alias | W | 110 (38) | 0 | none | CLI |
| `atrium_record` | record a test run against an item | W | 0 | 0 | none | CLI, or drop |
| `atrium_publish` | publish a document on the hub | W | 27 (14) | 7 | none | CLI |
| `atrium_put` | leave a file in the hub's drop | W | 10 (4) | 0 | none | CLI |
| `atrium_get` | fetch a file from the drop | W | 56 (6) | 24 | none | CLI |
| `atrium_files` | list the drop | W | 0 | 0 | none | CLI |
| `atrium_export` | pull every room's numbers into the drop | W | 13 (7) | 2 | none | CLI |
| `atrium_git_push` | push the branch to the hub | W | 74 (31) | 55 | `git push hub <branch>` | drop: a card with a shell has git |
| `atrium_git_url` | URL to fetch code outside the cwd | W | 66 (39) | 39 | none | CLI |
| `atrium_ci` | read a repository's CI: runs, jobs, logs | W | 2,155 (122) | 1,380 | none | CLI, with logs to a file |
| `atrium_resources` | the machine inventory | W | 40 (36) | 22 | `atrium resources` | CLI |
| `atrium_close` | close a card and free its worktree | W | 405 (55) | 202 | `atrium close` | CLI |
| `atrium_reports` | director reports on the hub | W | 70 (47) | 18 | `atrium reports`, add and read only on the hub's machine | CLI |
| `atrium_backlog` | the hub's backlog | D | 2,491 (229) | 190 | `atrium backlog`, file and status only on the hub's machine | CLI |
| `atrium_launch` | start an agent on its own card | D | 1,360 (199) | 235 | `atrium launch`, this room only, prepared directory | CLI, brief passed as a file |
| `atrium_launch_worker` | start a worker for parallel work | D | 981 (119) | 48 | `atrium new` in part | CLI |
| `atrium_exit` | ask a session to leave | D | 772 (179) | 117 | `atrium exit` | CLI |
| `atrium_git_collect` | hub fetches a room's branches | D | 351 (62) | 48 | none | CLI |
| `atrium_git_sync` | bring a room's clone up to claude/main | D | 175 (45) | 36 | none | CLI |
| `atrium_cull` | reclaim a merged worker | D | 129 (24) | 0 | `atrium archive-workers` in part | CLI |
| `restart_atrium` | scheduled room restart | D, stdio | 98 (8) | 0 | `atrium stop`, no restart | CLI, still scheduled and detached |
| `atrium_item` | items the caller owns | D | 43 (8) | 0 | none | CLI |
| `atrium_deploy` | ask for or run a room deploy | D | 28 (7) | 0 | none | CLI |
| `atrium_wake_after_restart` | queue a prompt for after a restart | D | 19 (14) | 0 | none | CLI |
| `atrium_model` | switch a live session's model | D | 1 (1) | 0 | none | CLI |
| `atrium_deps` | work items that wait on others | D | 0 | 0 | none | CLI, or drop |
| `atrium_flow` | the merge flow: roles, stages, checks | D | 0 | 0 | none | CLI |
| `atrium_transport` | turn a room's ziti or zrok off or on | D | 0 | 0 | none | CLI |
| `atrium_worktrees` | audit a repository's worktrees | D | 0 | 0 | `git worktree list` in part | CLI, or drop |
| `atrium_review` | open a review, review class only | review | not counted | | none | CLI |
| `atrium_clear_context` | handoff, clear, reload | stdio | 0 | 0 | `atrium new-context` | drop: ready covers it |
| `atrium_subscribe` | hear a hub event | stdio | 0 | 0 | `atrium subscribe` | CLI |
| `atrium_open` | open a link as a card | stdio | 0 | 0 | `atrium open`, 13 calls | CLI |
| `atrium_git_clone` | a clone to work in | stdio | 0 | 0 | none | CLI |
| `atrium_proc_start` | a long-running process the room owns | stdio | 0 | 0 | none | CLI |
| `atrium_port` | free TCP ports | stdio | 0 | 0 | none | CLI |
| `atrium_own` | record something the card started | stdio | 0 | 0 | none | CLI |
| `atrium_overlay` | a throwaway ziti overlay | stdio | 0 | 0 | none | CLI |

Bash and PowerShell calls of the CLI in the same transcripts: `ready` 361, `backlog` 77, `finish` 65, `tell` 55,
`launch` 51, `ask` 49, `peers` 48, `exit` 15, `open` 13, `task` 9, `close` 7, `answer` 4, `new` 1. Agents already reach
for the CLI where it exists and the prompt names it. `ready` is the clear case: no session called the MCP tool.

## Why the four stay

- **`atrium_done` and `atrium_blocked`.** They are how a worker ends. Each records the end against the card, checks a
  sha exists, and delivers the one message the launcher gets. A worker that cannot call them is a silent stop.
  `atrium finish` and `atrium ask` are older verbs that set the card's status and reach no launcher, so they are not a
  stand-in today.
- **`atrium_say`.** The question path. A worker asks its launcher, and the receiving side's typing, queueing and
  `held` rules live behind it. It is also the most called tool, nearly three times the next, and the floor tool a card
  of any class uses.
- **`atrium_ready`.** The context-limit handshake. The room's limit prompt names it, and it must work on a card that
  has no shell (`runnerHasNoShell`), where no CLI can.
- **They stay MCP because a card without a working shell must still end, ask and hand off.** A shell can be missing,
  denied by the gate, or broken on a Windows room. MCP is the one path that does not depend on it.

**Gap found: the hub does not serve `atrium_ready`.** It is only on the stdio `atrium control` server. A
room-launched card talks to the hub (`C:\Users\claude\.atrium\mcp.json`), so it has no `atrium_ready` tool, and the
limit prompt's "or call the atrium_ready tool" points at nothing. All 361 `ready` calls on record went through
`atrium ready` on the shell. A card launched without a shell has no way to hand off at all. Adding `atrium_ready` to
`workerTools` closes it, at about 17 tokens on the first request.

## Why `atrium_report` drops rather than moves

Its four kinds are already covered: `done` by `atrium_done`, `blocked` by `atrium_blocked`, `question` by `atrium_say`
with `needs`, `progress` by `atrium_say` with `fyi`. It is 1,055 calls in 477 sessions. A worker that reports
`done` and then calls `atrium_done` sends the same message twice, and dropping the tool removes a choice a worker
gets wrong.

## What moving to the CLI costs

The token case for the CLI is real and smaller than it looks, and most of the work is outside the tokens:

- **The first request saves 551 tokens a director and 265 a worker.** Deferral already hid the schemas. Do not move
  tools for the first request.
- **A director saves about 5k tokens a session** in schema loads (9,062 down to 3,857 by `--help`, or to about 150 with
  a prompt cheat sheet). Each loaded schema is also read again on every later turn from the cache, so the saving
  compounds over a long director session.
- **Output can shrink a lot**, as `peers` shows (71%), when the CLI prints text and not JSON.
- **The CLI has to reach the hub from any room.** `atrium backlog file`, `backlog status` and `reports add` work today
  only on the hub's machine. The MCP tools work from every room because the hub serves them. Each verb that moves needs
  the CLI to call the hub over the room link, the way `atrium_task` already resolves `name@room`.
- **The binary has to be on PATH**, or every prompt names its full path.
- **Class gating goes away.** The worker class hides director tools from a worker's list. A CLI verb is always there.
  `ctlclass.go` already says the class is not a boundary and every handler keeps its refusals, so nothing is lost in
  safety. A worker will see `atrium launch --help` if it looks, and the handler refuses it as it does today.
- **The shell matters.** Through `Bash` a call costs what the measurement shows. A card given `PowerShell` pays 3,479
  tokens for the tool before its first call, and multi-line message text needs a here-string, a known source of failed
  calls. Keep `Bash` as the shell for a CLI-driven card on Windows, as superlean does.

## Recommendation

1. Keep `atrium_done`, `atrium_blocked`, `atrium_say` and `atrium_ready` on MCP, deferred behind ToolSearch. Add
   `atrium_ready` to the hub's worker set.
2. Drop `atrium_report`, `atrium_git_push` and `atrium_clear_context`.
3. Move the rest to `atrium <verb>`, the high-volume director verbs first (`backlog`, `launch`, `launch-worker`, `ci`,
   `exit`, `peers`, `task`), each with a terse text output by default and `--json` when asked. Put the binary on the
   session's PATH before the first one moves, and give the director prompt a cheat-sheet line per verb.
4. Leave the first request alone as a goal. The kept four cost 686 tokens over none, and that is the price of a card
   that can always end, ask and hand off.

## Files

All under `D:\tmp\mcp-eval\`: `full.json`, `worker.json`, `stdio.json` (the captured tool lists), `kept.json`,
`stub.js` (the replaying MCP server), `cfg\` (the MCP configs), `run.js` (the rows), `anchor.js` and `anchor2.js` (the
per-call and load runs), `count.js` and `counts.json` (calls by tool), `sizes.js` and `sizes.json` (call sizes),
`res-r1.json`, `res-r2.json`, `res-r3.json` and `ctx-*.txt` (the results).
