# Is moving atrium-control tools to the `atrium` CLI worth it?

Status: research, 2026-10-23. Backlog item `r-mcp-to-cli-worth-it`, a follow-on to `docs/rnd/mcp-vs-cli-eval.md`,
whose token numbers stand. Evaluation only. No product code changed. Scratch work is under `D:\tmp\mcp-eval2\`.

## Verdict

**Do part, and not the part the first eval proposed.** Keep agents on MCP. Trim the tool descriptions instead, and give
humans the few CLI verbs they would use.

The number that decides it: **atrium-control schemas cost 2.4% of a real day's tokens on this room, and moving every
movable tool to the CLI nets at most 0.9%.** Halving the description text of the tools as they are saves about 1.2%,
with no new code path, no shell failure surcharge and nothing for an agent to learn.

| | per day, raw tokens | share | per day, input-equivalent | share |
|---|---|---|---|---|
| everything on this room | 195M | | 35.3M | |
| atrium-control schemas, loaded and carried | 6.1M | 3.1% | 0.86M | 2.4% |
| the part a CLI move removes (all but the kept four) | 4.0M | 2.0% | 0.53M | 1.5% |
| a director-only move, after its cheat sheet and shell failures | 2.3M | 1.2% | 0.32M | 0.9% |
| halving every atrium-control description instead | 3.1M | 1.6% | 0.43M | 1.2% |

Input-equivalent weighs each token by its price against fresh input: cache writes 2 (every write this day was a
1-hour write), cache reads 0.1, output 5. Raw counts every token the model read or wrote.

## 1. Tokens over a real day

### Method

- Every Claude Code transcript under `C:\Users\claude\.claude\projects\` with a request at or after
  2026-10-22T00:00Z, less the first eval's `claude -p` runs and this card. 94 sessions, 3,806 requests, from
  2026-10-22T02:17Z to 2026-10-23T12:41Z, 34.4 hours. Per-day figures scale by 24/34.4.
- This room only (sg4-control). Other rooms keep their transcripts on their own machines. The store's
  `session_usage` for the same two days sums to 260M tokens, against 280M here, which checks the denominator.
- A schema load is a `tool_reference` to an atrium tool in a ToolSearch result. Its size is the first eval's
  measured load where there is one (`say` 1,840, `launch` 4,244, `ci` 1,160, `backlog` 977, `done` 855, `report`
  841, `peers` 494) and the schema's JSON length / 3.2 otherwise (`launch_worker` about 2,545). Once loaded, a
  schema is in every later request of that context. That is the **carry**: schema tokens times the requests after
  the load, read from the cache. A context cycle (a `/clear` handoff) starts a new transcript and drops it. There
  were no compactions in the window.
- Role: **worker** when the first message names `BRIEF.md`. **Director** for the orchestrator
  (`D:\git\github\dovholuknf\atrium`), `qa` and `rnd`. **Other** for human sessions, PR reviews and the rest.

Scripts: `analyze.js` (per session), `agg.js` (per role and card), output in `sessions.json`.

### Per role, over the 34.4 hours

| role | cards | sessions (context cycles) | requests | tokens | schema loads | loaded | carried | movable |
|---|---|---|---|---|---|---|---|---|
| director | 3 | 30 | 1,656 | 137.2M | 69 | 104k | 5.97M | 4.94M |
| worker | 49 | 51 | 1,617 | 92.9M | 95 | 74k | 2.30M | 0.57M |
| other | 11 | 13 | 533 | 49.9M | 8 | 8k | 0.34M | 0.18M |
| all | 63 | 94 | 3,806 | 279.9M | 172 | 186k | 8.61M | 5.68M |

"Movable" is loaded plus carried for every tool but `atrium_done`, `atrium_blocked`, `atrium_say` and `atrium_ready`.

What the table says:

- **The carry is 46 times the load.** A schema costs little to load and a lot to keep. A director loads about 3.5k
  tokens of schemas in each cycle and then re-reads them on every request, about 199k tokens a cycle.
- **Context cycles multiply it.** The `qa` director cycled 22 times in 34 hours, the orchestrator 5 times, and each
  cycle loaded `backlog` and `launch_worker` again (3.5k). Cycles keep each context short, so the carry per cycle
  stays bounded, but the load repeats.
- **87% of the movable cost is in the three director cards.** Workers mostly load `atrium_done` (50 times) and
  `atrium_blocked` (19), which stay. A worker's movable cost is 0.2% of the day.
- **Workers load what they never call.** 38 of 95 worker loads were never called, 27k tokens: `blocked` loaded next
  to `done` in case it is needed, and probe loads of `get`, `task`, `record` and `resources`.

### Per tool, over the 34.4 hours

| tool | loads | loaded | carried | calls | kept on MCP |
|---|---|---|---|---|---|
| `atrium_launch_worker` | 17 | 43k | 2.23M | 58 | |
| `atrium_say` | 21 | 39k | 2.07M | 31 | yes |
| `atrium_backlog` | 18 | 18k | 1.00M | 65 | |
| `atrium_done` | 50 | 43k | 0.75M | 65 | yes |
| `atrium_launch` | 2 | 8k | 0.69M | 12 | |
| `atrium_peers` | 7 | 3k | 0.32M | 9 | |
| `atrium_blocked` | 19 | 5k | 0.21M | 11 | yes |
| `atrium_task` | 5 | 6k | 0.19M | 4 | |
| the other 17 | 33 | 21k | 1.15M | 24 | |

`atrium_say` is the second largest cost and stays on MCP whatever is decided. Its description is 5.2k characters.

### What a CLI move saves

The CLI is not free either. Its verbs have to be found, by a `--help` read that is carried like a schema, or by a
cheat sheet in the prompt that every request carries.

- **Every movable tool, cheat sheet in the prompt:** 5.68M less a 300-token director sheet over 1,656 requests
  (0.50M) and a 100-token worker sheet over 1,617 (0.16M). 5.0M raw in the window, 3.5M a day, 1.8% raw.
- **Directors only, which is where the cost is:** 4.94M less 0.50M is 4.44M. Then the shell failures in section 2:
  shell calls of the CLI fail 8.5 points more often than MCP calls, and a failed director call costs a request at
  the director's mean context of 83k. 160 movable director calls give 14 failures and 1.16M. Net 3.28M in the
  window, **2.3M a day, 1.2% raw, 0.9% input-equivalent.**
- **`--help` instead of a cheat sheet:** the help texts are 43% of the schemas they replace (first eval: 3,857 against
  9,062), so 57% of the movable cost, 1.2% raw, before shell failures.
- **Halving the descriptions instead:** half of all schema cost, kept tools included, 3.1M a day, 1.6% raw, 1.2%
  input-equivalent. `say` alone is a quarter of that.

## 2. Does an agent do the job as well through the shell?

### Trials on a scratch card

The top five by volume are `say`, `backlog`, `ci`, `launch` and `report`. `ci` has no CLI verb to try. `report` was
tried as what workers actually call, `atrium_done` against `atrium finish`. Each task three times in three modes:

- **mcp**: `Read,Edit,Bash,ToolSearch` and the atrium-control MCP server, the 36-tool list replayed by a stub that
  records each call (`stub2.js`). `backlog` used the live hub with only `atrium_backlog` allowed.
- **bash**: `Read,Edit,Bash`, no MCP, and the system prompt line "Atrium's own commands are the atrium CLI, `atrium`
  on PATH. `atrium --help` lists its verbs." No verb named.
- **pwsh**: the same with `PowerShell` in place of `Bash`.

On PATH was a shim `atrium.exe` (`D:\tmp\mcp-eval2\shim\`, built to `build.claude/evalshim/`) that records its argv
and stdin, answers `tell`, `finish` and `launch` as the real binary would, and passes `--help`, `backlog`, `peers` and
`task` to the real one. No session was told anything and nothing was launched. The message and the brief carry
double and single quotes, backticks, `$`, `&`, `<>`, `|`, `%`, Windows backslash paths and line breaks
(`msg.txt`, `brief.txt`). `claude-opus-5-5`, `claude -p`, `trial.js` and `score.js`.

| task | mode | turns | tokens, mean | errors | text arrived exact |
|---|---|---|---|---|---|
| say | mcp | 3.0 | 19,413 | 0 | 3/3 |
| say | bash | 4.0 | 21,119 | 0 | 3/3 |
| say | pwsh | 4.3 | 32,214 | 0 | 3/3 |
| backlog show | mcp | 3.0 | 21,010 | 0 | |
| backlog show | bash | 3.0 | 14,806 | 0 | |
| backlog show | pwsh | 3.0 | 20,456 | 0 | |
| launch | mcp | 3.0 | 30,428 | 0 | 3/3 |
| launch | bash | 3.3 | 20,980 | 0 | 3/3 |
| launch | pwsh | 4.3 | 34,976 | 0 | 3/3 |
| done / finish | mcp | 3.0 | 17,528 | 0 | 3/3 |
| done / finish | bash | 4.0 | 21,446 | 0 | 3/3 |
| done / finish | pwsh | 4.0 | 28,809 | 0 | 3/3 |

"Tokens" is the sum over the run's requests plus output, so it counts each re-read.

- **No errors and no retries in 36 runs, and every text arrived byte for byte.** Through Bash the model put the text
  in a quoted heredoc, into a variable or on stdin. Through PowerShell it used a single-quoted here-string. One
  PowerShell run checked `$PSNativeCommandArgumentPassing` first. On this model, quoting is not where it breaks.
- **Discovery costs a turn.** With no verb named, every CLI run read `atrium --help` and then the verb's help. It
  found the right verb each time: `tell` for say, `finish --status done --sha` for done, `launch --prompt` for launch
  (one run read `new --help` first). A cheat sheet would remove that turn.
- **Bash is cheaper than MCP for big schemas and dearer for small ones.** `launch` through Bash cost 31% less than
  MCP, because the MCP run loaded `atrium_launch` and `atrium_launch_worker` (6.8k) where Bash read one help text.
  `say` and `done` cost 9% and 22% more through Bash, for the help reads.
- **PowerShell costs most in three tasks of four**, 15% to 66% over MCP: its 3,479-token tool description and a
  turn more. It was 3% under MCP for `backlog show` only.

### What the real transcripts say

Every atrium-control call and every shell call of the CLI in all transcripts on this machine, 2026-09-04 to now
(`errs.js`, `errs.json`):

| failure | MCP calls (16,669) | CLI calls (925) |
|---|---|---|
| any error | 8.4% | 13.1% |
| atrium refused it (unknown card, wrong state) | 6.7% | 4.1% |
| a queued say delivered by blocking the call | 1.6% | 0.4% |
| the shell guard refused the command | | 6.9% |
| binary not on PATH | | 1.5% |
| argv misparsed | | 0.1% |
| Claude Code's auto-mode classifier or the human refused it | 0.1% | |

The shell adds failures the MCP path does not have: **8.5 points of extra failed calls**.

- **The shell guard.** 64 refusals of the 925: `Use tee instead of > or >>`, `Do not chain multiple commands with
  ';'`, a `git push` in the same line. Agents wrap a CLI call in redirections and chains, and the guard is right to
  refuse them, but each is a failed turn. An MCP call has no command line to refuse.
- **The binary.** 14 calls hit `atrium: command not found` or the PowerShell equivalent. The room names the binary
  by full path because it is not on every session's PATH.
- **Multi-line text.** One real failure: a PowerShell call passed a whole markdown handoff as a positional to
  `atrium ready`, which read it as an unknown command. The trials show the model can quote, the record shows it does
  not always.

### The other failure modes asked about

- **Output size.** The CLI prints text and the MCP tools answer JSON. `atrium peers` is 71% smaller than
  `atrium_peers` (first eval). `backlog show` is the same size both ways. This is a fix to the MCP output as much as
  a reason to move.
- **Discoverability without a schema.** Measured above: one help read per verb, about 2k tokens a task, or a cheat
  sheet. A deferred MCP tool needs a ToolSearch, which is the same one turn.
- **Permission prompts and hooks.** Both paths go through the same gate. Since 2026-10-15 global auto mode approved
  5,299 atrium MCP calls and 345 shell calls of the CLI. Without auto mode an MCP tool is one rule per tool, and a
  shell call needs a `Bash(...)` prefix rule that the binary's full path and the wrappers agents add make brittle.
  The guard's shell rules apply only to the shell.
- **The say-interrupt.** A queued say reaches a busy session by blocking its next tool call with the message
  (`perm-decided` with `by: message`). It blocks a shell call the same way as an MCP call: 96 MCP and 4 shell
  blocks since 2026-10-15. No difference, except that a CLI command that runs long holds the message as any long
  tool call does.

**Answer: as well on the happy path, worse in practice.** The model drives the CLI correctly when it reads the help,
but a shell call can fail in ways an MCP call cannot, and on the record those failures hit 8.5% of CLI calls, about
one in twelve.

## 3. Humans

What clint does, from the board's `board_usage` (3,709 clicks, 2026-10-12 to 2026-10-23) and the PowerShell history
on this machine (`ConsoleHost_history.txt`, 9,332 lines, which PSReadLine writes only for interactive shells, so it
should hold no agent calls):

- **The board is the surface.** Top controls: a card's terminal (`view:terms`, 124), the toast log (98 and 68),
  the stack view (62), restart and terminate (34 each), filters, `+ new agent` (19), the items view (19).
- **The CLI is for running atrium itself.** Atrium verbs typed by hand: `daemon`, `room`, `run`, `stop`, `version`,
  `hook`, and `backlog list --dept ideas` three times. No `tell`, `peers`, `launch` or `task`.

| verb | on the CLI today | would clint run it | better than the board? |
|---|---|---|---|
| backlog list / show | yes | yes, he has | yes: a terminal and grep, no clicking through items |
| backlog file / status / priority | hub machine only | yes, from any room | yes, once it works from every room |
| peers `--fleet` | yes | maybe | no: the board is the same list, live |
| task | yes | rarely | no: the card is one click |
| tell / say | yes | no | no: he types into the card's terminal |
| launch / launch_worker | `launch`, this room, prepared directory | rarely | no: `+ new agent` is the dialog he uses |
| close, exit, cull | `close`, `exit`, `archive-workers` | rarely | no: terminate and clean up are on the card |
| ci | no | maybe | yes: a CI log is text, better in a terminal |
| usage | yes | yes | yes: numbers to pipe and compare |
| resources | yes | rarely | the file is hand-kept, he edits it |
| ready, finish, ask, done, blocked | yes | no | agent verbs |

The human case is real and small: `backlog` from any room, and `ci` logs. Neither needs agents to move.

## 4. Cost to build and keep

### Three front ends today

| surface | where | lines (non-test) | serves |
|---|---|---|---|
| hub MCP | `internal/link/*mcp*.go`, `ctl*.go`, `control_*.go` | 9,097 | room-launched cards |
| stdio MCP, `atrium control` | `internal/cli/control*.go`, `ready.go` | 3,052 | the user-scope server |
| CLI verbs | `internal/cli/peer.go`, `finish.go`, `help.go`, `cardverbs.go`, `backlog.go`, `close.go`, `launch.go`, `resources.go` | 2,209 | humans and shells |

Seven operations have all three: say (`atrium_say` twice, `atrium tell`), peers, task, exit, launch, report
(`atrium_report` twice, `atrium finish`) and, in part, ready. Each surface resolves a peer by its own code
(`resolvePeer` in `internal/cli/control_peers.go`, `c.resolvePeer` in `internal/link/control_mcp.go`) and reaches
the room by its own route: the hub's say through `c.ask` and `sayAcross`, the stdio say through `POST /v1/say`, `atrium tell`
through the agent listener's `/tell`. The good news is underneath: `atrium finish` (`/finish`) and the report door
(`POST /v1/tasks/{id}/report`) both end in `daemon.finish`, so the end of a worker is written once.

Moving a verb to the CLI means a fourth path for cards on other rooms, because the CLI talks to its own room and
the MCP tools talk to the hub. `backlog file`, `backlog status` and `reports add` already show the cost: they work
only on the hub's machine.

### Drift found

1. **`atrium tell` cannot say `fyi`.** The daemon's `/tell` takes `kind` (`internal/daemon/peers.go`), and
   `atrium_say` exposes it, but `tell` has no `--kind` flag, so every CLI say is a `needs`. It has no `--force`
   either.
2. **`atrium peers` and `atrium_peers` answer different questions.** The tool takes `all` and `rooms`, the CLI takes
   `--fleet` and `--since`. The first eval counted 38 sessions against 30 for the same moment.
3. **The hub does not serve `atrium_ready`**, only the stdio server does (first eval, still true).
4. **`backlog` writes only from the hub's machine** on the CLI, from every room through MCP.
5. **The first eval was wrong about `atrium finish`.** It said `finish` "reaches no launcher". `finish` posts to
   `/finish`, which calls the same `daemon.finish` as `atrium_report` and queues the report to the launcher. Its own
   help says so. `atrium finish --status done --sha <sha>` is a working stand-in for `atrium_done` today, which is
   why the trials could use it.
6. **The stdio server and the hub serve different sets.** The stdio server has `ready`, `clear_context`, `open`,
   `git_clone`, `own`, `port`, `proc_start`, `overlay`, `subscribe`. The hub has `alias`, `record`, `files`, `put`,
   `get`, `export`, `cull`, `deploy`, `flow` and the rest. A card gets one or the other depending on how it started.

Every tool that moves has to be kept in step across these, or retired from MCP, which removes the choice from
agents that use it today (16,669 MCP calls against 925 CLI calls on record).

## 5. Verdict per tool

"Trim" means shorten the description and keep the tool: the schema cost is the description, and the carry multiplies
it by every request.

| tool | verdict | why |
|---|---|---|
| `atrium_done`, `atrium_blocked` | stay MCP, trim | how a worker ends. `finish` works too, but MCP never fails on a shell rule |
| `atrium_say` | stay MCP, trim hard | 2.1M carried in 34h, the largest kept cost. 5.2k characters of description |
| `atrium_ready` | stay MCP, add to the hub | the no-shell handoff, missing from the hub |
| `atrium_launch_worker` | stay MCP, trim hard | the largest cost of all (2.2M carried) and a director's daily tool |
| `atrium_launch` | stay MCP, trim hard | 4.2k a load. Bash beat MCP by 31% on this task only because of the schema |
| `atrium_backlog` | stay MCP, trim. CLI: file and status from any room | 65 calls, and clint reads the backlog from a terminal |
| `atrium_ci` | stay MCP. CLI: add `atrium ci` with logs to a file | the one verb a human wants that has no CLI |
| `atrium_peers` | stay MCP, answer text not JSON | output size, not schema, is its cost (4.7k a call) |
| `atrium_report` | drop | done, blocked and say cover it (first eval) |
| `atrium_git_push` | drop | `git push hub <branch>` (first eval) |
| `atrium_clear_context` | drop | ready covers it (first eval) |
| `atrium_task`, `exit`, `close`, `cull`, `alias`, `status` | stay MCP | small, director-only, CLI exists for humans |
| `atrium_get`, `put`, `files`, `export`, `publish`, `record`, `git_url`, `resources`, `reports` | stay MCP, stop probe loads | loaded and never called by workers |
| the rest (`item`, `deploy`, `deps`, `flow`, `transport`, `worktrees`, `model`, `git_collect`, `git_sync`, `wake_after_restart`, `restart_atrium`) | stay MCP | rare, director-only, cheap while deferred |

## Recommendation

1. **Do not move agents to the CLI.** The best case nets 0.9% of the day's input-equivalent tokens, and the shell
   brings a failure rate 8.5 points higher on the record, a fourth code path per verb for other rooms, and drift to
   keep in step.
2. **Trim the descriptions**, starting with `atrium_say`, `atrium_launch_worker`, `atrium_launch` and
   `atrium_backlog`, which carry 69% of the schema cost. Halving all of them saves 1.2%, more than the move, and
   every card gets it.
3. **Stop speculative loads.** Tell workers to load `atrium_blocked` only when they are blocked. 38 of 95 worker loads
   were never called.
4. **Add `atrium_ready` to the hub's worker set.**
5. **For humans:** make `atrium backlog file`, `status` and `priority` work from every room, add `atrium ci` with logs
   to a file, and give `atrium tell` a `--kind`. These are CLI parity fixes, not a migration.
6. **Retire the duplicate stdio tools over time.** The stdio server's say, peers, task, launch, exit and report repeat
   the hub's with their own resolver and route, and a card started by the room never sees them.

## Files

Under `D:\tmp\mcp-eval2\`: `files.json` (the transcripts read), `analyze.js` and `sessions.json` (per session),
`agg.js` (per role, card and the shares), `db.js` (read-only queries of the room's store), `errs.js` and `errs.json`
(failure classes), `stub2.js` (the recording MCP stub), `shim\` (the recording CLI shim), `trial.js`, `msg.txt`,
`brief.txt`, `runs\` (each trial's result and recorded calls), `score.js` and `scores.json`.
