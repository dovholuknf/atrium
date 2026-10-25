# Superlean workers

A lean worker (`docs/runtime/lean-workers-design.md`) starts without the operator's setup: no user CLAUDE.md, no
memory, no user skills or agents, two MCP servers. It still gets every built-in tool Claude Code ships that lean does
not name in `--disallowedTools`, mercurius, and a ten-line worker prompt. A superlean worker cuts those too. It is for a
worker whose brief is a bounded change: edit files, run a command, commit, report.

Per-item minions already start this way by hand, through launch `args`: `--tools Bash,ToolSearch`, `--system-prompt
"<line>"`, sonnet, effort low, lean. Superlean makes that one named option, recorded on the card, so a reopen, a resume
and the keep-alive fork start it the same way again.

Backlog item `r-superlean-launch`. Status: built in phases P1 to P3, measured in P4 ("Numbers" and "Decisions" below).

## The flag set

A superlean launch is a lean launch with three changes. Everything lean does and this list does not name stays
exactly as lean does it, built by the same `leanArgs` call.

| | lean | superlean |
|---|---|---|
| built-in tools | all but the 22 in `leanDisallowed` | `--tools Read,Edit,Write,Grep,Glob,Bash,PowerShell,ToolSearch` |
| `--disallowedTools` | `leanDisallowedFor(kit)` | unchanged. Redundant under `--tools`, free on the wire |
| MCP servers | atrium-control, mercurius, plus `mcp` | atrium-control, plus `mcp` |
| appended prompt | `leanSystemPrompt`, ten lines | `superleanSystemPrompt`, one paragraph, below |
| setting sources, `--settings`, hooks, status line | lean's | lean's, unchanged |
| `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1` | yes | yes |

The allow list grows only by what the launch asks for: `Agent` with `lean_agents`, `Skill` with `lean_skills`. Both keep
working as they do for lean, through the same plugin directory and the same agent allowlist hook.

**ToolSearch stays on the list.** `--tools` removes ToolSearch unless it is named, and without it every MCP tool
loads its full schema instead of its name. Measured for lean, that is +3.4k tokens. MCP tools stay callable under
`--tools`, which is what keeps `atrium_done` working.

**A caller's own `--tools` in `args` wins.** A minion that passes `--tools Bash,ToolSearch` is asking for less than
superlean gives, so superlean adds no `--tools` of its own when `args` already carries one, and the `launched` event
says so. Two `--tools` flags on one command line would leave the answer to Claude Code's parser.

**The prompt.** `superleanSystemPrompt`, appended, never a replacement:

> You are a worker launched through atrium. BRIEF.md is your whole task. End with atrium_done (a sha or an artifact)
> when you finish, or atrium_blocked when something stops you: that is the one message your launcher gets. Ask with
> atrium_say. Commit on your own branch, never on claude/main or main, with a one-line subject and no trailer. Never
> restart atrium or deploy unless the brief says to. Run tests in one foreground call, scoped to what you touched.

## What each cut breaks

**Reporting, `atrium_done`, `atrium_blocked`, `atrium_say`.** Nothing. atrium-control is always kept, and its tools
are deferred behind ToolSearch, so they cost their names on the wire. A launcher gets the same report it gets from a
lean worker.

**No MCP at all, for a throwaway.** Not offered. Dropping atrium-control saves only the deferred names of its tools,
which the lean measurement put in the tens to low hundreds of tokens, and it costs the report. A worker that cannot
report is a silent stop to whoever launched it, and atrium would have to refuse the combination with `origin:agent`.
P4 measures what atrium-control costs a superlean worker. If it is over 1k tokens, a no-MCP mode for operator-launched
cards is filed then, refused for any launch with a launcher.

**Review.** mercurius is gone, so a review worker launched superlean cannot open a review session. A review worker
launches lean, as today, or superlean with `mcp: ["mercurius"]`. With the `lean_worker_gateway` setting on, the
gateway server is not added either: superlean keeps no wide or narrow mercurius unless `mcp` names it.

**Hooks.** Nothing. `--tools` changes the tool list, not the hooks. The permission gate keys on the worktree's
`.mcp.json` marker or `atrium join`, as for lean, and the reporters, the Stop hook, the no-poll hook and the
lean-agents hook come from the same `leanSettings`.

**Tools a worker loses on top of lean.** `TodoWrite`, `WebFetch`, `WebSearch`, `PowerShell`, and any built-in
Claude Code adds later. That last one is the
point: a new tool never arrives by default. A worker that needs any of these launches lean. On a Windows room the
worker runs git-bash through `Bash`, as minions do today.

**Claude Code's own system prompt.** Kept. `--system-prompt` would save ~2.1k but drops Claude Code's guidance on its
tools and on destructive commands, and a superlean worker still has `Bash` in a real repository. A launcher that wants
the replacement passes it in `args`, as minions do now, and it is on that launcher.

**Interview sessions.** Refused. An interview is a human talking to the session, it carries its own system prompt
(`interviewSystemPrompt`), and the one-paragraph worker prompt would replace it with the wrong rules.

**Runners other than claude.** Refused, as lean is.

## The tag

The card carries `atrium:superlean` AND `atrium:lean`. Superlean implies lean, so every place that already reads
`atrium:lean` keeps working without a change: the board's "with my full setup" resume entry, the keep-alive fork's
memory environment, the lean-agents and lean-skills tags.

- `leanOptions` returns a fourth value, `superlean`, true for `superlean: true` on the request or the tag on the card.
- `lean: false` takes both tags off, through `withoutLeanTags`, which learns the new tag.
- `superlean: false` on a superlean card takes only `atrium:superlean` off, so the card comes back lean.
- `forkCardArgs` goes through `leanOptions` and `leanArgs` already, and `--tools` is in `forkCarried`, so the fork's
  command line matches the card's and its cache key with it. A test pins that.
- The `launched` event records `superlean` beside `lean` and `mcp`.

## Where it is offered

| surface | today for lean | superlean |
|---|---|---|
| `/v1/launch` on the room | `lean`, `mcp`, `lean_agents`, `lean_skills` | `superlean` |
| `atrium_launch` on the hub | `lean` (default on), `mcp`, the kit | `superlean`, implies lean, refused with `lean: false` |
| `atrium_launch` on the stdio control server | the kit only | `superlean`, passed as the kit is |
| `atrium launch` | nothing | `--lean` and `--superlean` |
| the board's launch dialog | nothing | one select: full setup (default), lean, superlean. Claude runner only |

Not a lean option, and listed here because this is where the launch fields are: `/v1/launch` also takes an optional
`launch_key`, which the hub's and the stdio `atrium_launch` make on the first attempt and send again on the retry for the
same directory. The room returns the card already filed under it, marked `launch_replay`, and starts nothing. See
`docs/changes/f-002.md`.

`atrium launch` and the dialog have no lean option today, so both get lean and superlean together. Offering superlean
where lean cannot be chosen would make the smaller option easier to reach than the larger one.

`atrium_launch` keeps lean as its default. Superlean is opted into. Whether per-item minions and their workers move to
it is decided on the P4 numbers.

A room older than the field ignores `superlean` and starts the worker lean. The hub reads the card it gets back and,
when `atrium:superlean` is missing, says so in the launch answer, the way it already does for `lean_agents`.

## Measurement plan

Measured as lean was, so the rows compare: Claude Code version and model recorded, `claude -p` in one worker
worktree, first request = `input_tokens + cache_creation_input_tokens + cache_read_input_tokens` from the transcript,
and `/context` in print mode for the breakdown.

Rows, all in the same worktree, same day:

1. lean (the default today)
2. superlean
3. superlean with `mcp: ["mercurius"]`
4. superlean with `--system-prompt "<the paragraph>"` in `args`, the minion shape
5. superlean with atrium-control taken out by hand, to price the no-MCP mode above

`/context` columns: system prompt, system tools, system tools (deferred), MCP tools (deferred), total.

**Idle CPU.** `bug-idle-minion-spins-a-core` suspects this flag set. Start one lean and one superlean session
interactively in the same room, let both sit idle for ten minutes after their first turn, then read each process's
CPU seconds twice, sixty seconds apart (`Get-Process -Id <pid>` `.CPU`). A superlean session that burns more than the
lean one is a finding for that bug, with the flag that does it found by removing one at a time.

The numbers go in a table under a new "Numbers" section, as in the lean doc.

## Build, in phases

Each phase lands on `claude/main` on its own, with its own changelog fragment and `docs/changes` page.

**P1, the room.** `LaunchRequest.Superlean`, `leanOptions` returning it, the allow list and the MCP and prompt changes
in `leanArgs`, `superleanSystemPrompt`, the tag, `withoutLeanTags`, the `launched` event field, and the refusals
(non-claude, interview, `superlean` with `lean: false`). Tests: the flag set for a superlean launch, with and without
`lean_agents` and `lean_skills`. A caller's `--tools` left alone. Tag round trip through a reopen and through
`lean: false` and `superlean: false`. `forkCardArgs` for a superlean card carrying `--tools`. Room restart to deploy.

**P2, the callers.** The hub's `atrium_launch` schema and request body, the missing-tag warning, the stdio
`atrium_launch`, and `atrium launch --lean --superlean`. Hub restart for the hub part.

**P3, the dialog.** The setup select, shown only for a claude runner, sending `lean` or `superlean`. Headless board
unit for the select and the request it sends.

**P4, the numbers.** The measurement above, folded into this doc, and the two decisions it settles: the no-MCP mode,
and whether minions launch superlean by default. Done, below.

## Numbers

Claude Code 2.1.302, model `claude-opus-5-5` (what `claude -p` picked, `modelUsage` in the result). All rows are
`claude -p "Reply with the single word ok."` in the `r-superlean-launch-p4` worktree on 2026-10-16, one model call each,
so "first request" is `input_tokens + cache_creation_input_tokens + cache_read_input_tokens` of that call, read from the
`--output-format json` result, which is the same number the transcript holds. The breakdown is `claude -p "/context"`
with the same flags, in thousands of tokens.

The worktree's `AGENTS.md` loads as a 7.5k "memory file" in every row, so these totals sit above the lean doc's
(11,029 for lean in a worktree) by that much. The cuts are what compare.

| row | first request | vs lean | system prompt | system tools | system tools (deferred) | MCP tools (deferred) | total loaded |
|---|---|---|---|---|---|---|---|
| 1. lean | 18,749 | | 2.1 | 7.4 | 2.8 | 16.0 | 17.0 |
| 2. superlean | 14,462 | -4,287 (-23%) | 1.6 | 3.7 | 0 | 16.0 | 12.7 |
| 3. superlean with `mcp: ["mercurius"]` | not measured | | | | | | |
| 4. superlean, `--system-prompt` in `args` (minion shape) | 12,847 | -5,902 (-31%) | 0.16 | 3.7 | 0 | 16.0 | 11.3 |
| 5. superlean, atrium-control taken out | 13,416 | -5,333 (-28%) | 1.6 | 3.7 | 0 | 0 | 12.7 |

How the rows were built. Row 1 is lean as `leanArgs` builds it: `--setting-sources project,local`,
`--strict-mcp-config --mcp-config` with atrium-control only, the 22 tools of `leanDisallowed` in `--disallowedTools`,
`leanSystemPrompt` appended, `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1`. Rows 2, 4 and 5 add `--tools=Read,Edit,Write,Grep,Glob,
Bash,ToolSearch` and swap in `superleanSystemPrompt` (appended in 2 and 5, as `--system-prompt` in 4). Row 5 passes an
empty `mcpServers`. `--tools` is written `--tools=<list>`: as a separate token it is variadic and swallows the prompt that
follows.

What is not the real thing, so the table is read as a comparison and not as a recording of a room launch:

- **No room did the launch.** The flag set was rebuilt by hand from `leanArgs`, `superleanTools` and the two prompts in
  `internal/daemon/lean.go`, because no room on this machine runs a binary with P1. The settings file carries only the
  empty attribution and a permissive allow list. It leaves out the hooks `leanSettings` adds (reporters, Stop hook,
  no-poll hook), which cost nothing on the wire in a one-turn run.
- **Row 3 is not measured.** No MCP config on this machine holds a `mercurius` server, and `leanServers` refuses a name
  no config holds. The lean doc priced it at 36 tokens with its six tools deferred, and nothing here suggests that moved.
- **Idle CPU is not measured.** It needs two interactive sessions left idle ten minutes and their CPU seconds read twice.
  This worker is headless and cannot drive an interactive terminal, and `Get-CimInstance` is denied here, so the pids
  could not be matched to sessions either. `bug-idle-minion-spins-a-core` stays open: someone with a room on the P1
  binary runs the plan in "Measurement plan" and removes one flag at a time if superlean spins more than lean.

What the numbers say.

- Superlean cuts 4.3k tokens a start from lean, 23% here, and more in a checkout with a lighter `AGENTS.md`. Almost all
  of it is `system tools`: 7.4k down to 3.7k, and the deferred tool names (2.8k) gone with `--tools`.
- The one-paragraph prompt saves 0.5k over `leanSystemPrompt`. Replacing Claude Code's own prompt, the minion shape,
  saves another 1.6k (2.1k down to 0.16k) and is on the launcher, as "Claude Code's own system prompt" says.
- atrium-control is 16k in `/context`, but deferred tools send only their names, so on the wire it costs 1,046 tokens
  (row 2 less row 5). That is the whole saving of a no-MCP mode.
- `ToolSearch` kept on the list holds up: the 16k of MCP schemas does not reach the wire.

## Decisions

**(a) The no-MCP mode: do not build it.** The design set the bar at 1k and the measurement is 1,046, over it by 46
tokens, 7% of a superlean start. That is a bar met on noise, and the reasons against stand whole. The mode has to be
refused for any launch with a launcher, which is every card a minion or a worker is, so it would be reachable only for
an operator's throwaway, where 1k tokens a start is not what the operator is waiting on. A worker that cannot call
`atrium_done` is a silent stop. Keep "Not offered" as written under "What each cut breaks". If the 1k matters later, the
cheaper lever is the 7.5k `AGENTS.md` that every row loads, not the MCP server.

**(b) Per-item minions and workers: launch superlean by default, with an escape.** The cut is 4.3k tokens a start and 5.9k
in the minion shape, against a loss that is `TodoWrite`, `WebFetch`, `WebSearch`, `PowerShell` and later built-ins, for a
worker whose brief is a bounded change. Per-item minions already start this way by hand through `args`, so this moves
nothing they do not already do and makes it one recorded option that a reopen and the keep-alive fork repeat. Scope it:

- per-item minions and the workers a card launches from an item: superlean, `superlean: false` to bring one back lean.
- review workers: lean, as today. Superlean drops mercurius, and a reviewer cannot open a review without it.
- `atrium_launch` from an orchestrator for anything else: lean stays the default, superlean opted into, as P2 shipped.
- keep a caller's own `--tools` winning, which P1 does.

This is a recommendation. Making it the default is a one-line change in the item launcher, filed as its own backlog item
after the idle-CPU run, so a spin is found before the default moves.
