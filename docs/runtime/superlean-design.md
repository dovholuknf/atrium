# Superlean workers

A lean worker (`docs/runtime/lean-workers-design.md`) starts without the operator's setup: no user CLAUDE.md, no
memory, no user skills or agents, two MCP servers. It still gets every built-in tool Claude Code ships that lean does
not name in `--disallowedTools`, mercurius, and a ten-line worker prompt. A superlean worker cuts those too. It is for a
worker whose brief is a bounded change: edit files, run a command, commit, report.

Per-item minions already start this way by hand, through launch `args`: `--tools Bash,ToolSearch`, `--system-prompt
"<line>"`, sonnet, effort low, lean. Superlean makes that one named option, recorded on the card, so a reopen, a resume
and the keep-alive fork start it the same way again.

Backlog item `r-superlean-launch`. Status: designed, not built.

## The flag set

A superlean launch is a lean launch with three changes. Everything lean does and this list does not name stays
exactly as lean does it, built by the same `leanArgs` call.

| | lean | superlean |
|---|---|---|
| built-in tools | all but the 22 in `leanDisallowed` | `--tools Read,Edit,Write,Grep,Glob,Bash,ToolSearch` |
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
and whether minions launch superlean by default.
