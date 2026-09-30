# Lean workers

A worker started by `atrium_launch` inherits none of its launcher's conversation. Until now it still booted with the
operator's whole setup: the global `~/.claude/CLAUDE.md`, auto-memory, every user and claude.ai skill, every custom
agent type, the operator's prompt-time hook reminders and every MCP server in the runner's config. A worker reads
its `BRIEF.md` and the repo, and needs little else. A lean launch starts it with only that, plus two MCP servers:
atrium-control to report and mercurius for reviews. `atrium_launch` now starts every claude worker lean unless the
caller passes `lean: false`.

The code is `internal/daemon/lean.go` on the room and `leanLaunch` in `internal/link/control_mcp.go` on the hub.

## Numbers

Claude Code 2.1.283, model `claude-opus-5-5`. "First request" is the whole input of the first model call:
`input_tokens + cache_creation_input_tokens + cache_read_input_tokens`, read from the session's transcript.

| worker | today | lean | cut |
|---|---|---|---|
| sa85 (this brief), interactive, in a worktree | 40,325 | | |
| `claude -p`, in a worktree | 35,970 | 11,029 | -69% |
| `claude -p`, gated and reporting, scratch dir, without mercurius | | 10,505 | |
| `claude -p`, scratch dir, with mercurius (the default) | | 10,541 | |
| `claude -p`, in the main checkout (repo CLAUDE.md loads) | 45,358 | 20,498 | -55% |

The worktree and main-checkout rows were measured before mercurius became a default. Keeping it costs 36 tokens on
the wire, because its six tools are deferred and send only their names. The interactive number sits ~4k above the `-p` one because the interactive tool set and the prompt-time hooks are
larger. The cut is about the same size in both modes: ~25k tokens a start.

`/context` in print mode is a local command that costs no model call. It gives the breakdown, in thousands of tokens.
Deferred tools send only their names, so their rows count little on the wire.

| category | today | lean | what moved it |
|---|---|---|---|
| system prompt | 2.1 | 1.5 | auto-memory instructions gone, worker rules appended |
| system tools | 7.4 | 8.1 | Agent, Skill and 20 others disallowed. Dropping the user source adds ~5.9k here, see below |
| custom agents | 2.3 | 0 | user source dropped |
| memory files | 4.5 | 0 | global CLAUDE.md (1.0) with the user source, MEMORY.md (3.5) with auto-memory off |
| skills | 7.8 | 0 | user and claude.ai-synced skills with the user source, built-ins with `Skill` disallowed |
| MCP tools (deferred) | 5.2 | 5.2 | unchanged: atrium-control and mercurius are both kept |
| system tools (deferred) | 13.1 | 3.0 | disallowed tools |
| total loaded | 24.1 | 9.7 | |

## Every lever, measured

Each row is one flag over today's worker, as `/context` totals or as the API number where `/context` cannot say.

| lever | cuts | worker loses | used |
|---|---|---|---|
| `--strict-mcp-config --mcp-config` with atrium-control and mercurius | every other server in the config | servers not named in `mcp` | yes. Dropping mercurius too saves 36 tokens. clint asked to keep it |
| `--setting-sources project,local` | skills 5.8k, agents 2.3k, global CLAUDE.md 1.0k. System tools grow 5.9k, net -3.3k | user hooks, permissions and env, restored by `--settings` below | yes |
| `--settings <filtered user settings>` | the operator's SessionStart and UserPromptSubmit hooks, status line, output style, attribution text | tab title and state scripts, the filler-guard reminder | yes |
| `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1` | 4.3k: MEMORY.md and its instructions | the operator's memory index | yes |
| `--disallowedTools` (22 tools) | Agent 1.1k, Skill and built-in skills 2.0k, deferred names 10k | subagents, workflows, crons, plan mode, AskUserQuestion, worktree tools | yes |
| `--append-system-prompt` | adds ~0.2k | nothing. It carries the worker rules | yes |
| `--system-prompt` (replace) | 2.1k | Claude Code's own tool and safety guidance | no |
| `--tools Bash,...` (allow list) | removes ToolSearch, so MCP tools load in full (+3.4k) | deferred loading | no |
| `--disable-slash-commands` | skills, and `/context` with them | every slash command | no, the user source and `Skill` cover it |
| `--agents '{}'` | nothing | | no |
| `--bare`, `--safe-mode` | everything | every hook, so the gate and the reporters. `--bare` also refuses OAuth | no |
| `--exclude-dynamic-system-prompt-sections` | nothing, it moves text to the first message | | no |

Dropping the user source adds ~5.9k to "system tools", and no user setting explains it. Passing the whole user
settings file back with `--settings` does not remove it. The net is still a cut, so it is recorded here, not chased.

## What a lean launch passes

In front of the harness's own arguments, so the positional prompt stays last. The harness's own `--mcp-config`,
`--strict-mcp-config`, `--setting-sources` and `--settings` are taken out first.

- `--setting-sources project,local`
- `--settings <json>`: a copy of `~/.claude/settings.json` with `permissions`, `env`, `model`, `effortLevel` and
  `hooks`, and an empty `attribution`. Every hook stays, except that on SessionStart and UserPromptSubmit, the two
  events whose output lands in the context, only atrium's own reporters stay (`session --event`, `hook --event`,
  `turn --event`). The Stop hook is added when the operator has none, as `withStopHook` does for a full launch.
- `--strict-mcp-config --mcp-config <json>`: the servers from the harness's own `--mcp-config` files, cut to
  `atrium-control` and `mercurius` plus the names in `mcp`. A default the config does not hold is left out. A name
  in `mcp` the config does not hold is refused, with the list it does hold.
- `--disallowedTools <list>`: see `leanDisallowed`.
- `--append-system-prompt <rules>`: report with `atrium_report`, never commit on `claude/main` or `main`, never
  restart atrium or deploy unless the brief says to, one-line commit messages with no trailer, Go builds to
  `build.claude/`, do not edit CLAUDE.md files, ask with `atrium_say`.
- `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1` in the runner's environment.

The card gets the tags `atrium:lean` and `atrium:mcp:<name>`, so a reopen after a restart, a resume and an unshelve
all start it lean again with the same servers. Tags, not a column, the way `origin:agent` is.

A lean launch of a runner that is not claude is refused. `atrium_launch` defaults `lean` on for the claude runner only.

## The repo CLAUDE.md: kept

The project source stays, so a repo's own CLAUDE.md and `.claude/settings.json` still load. For atrium this costs
nothing where workers run: the repo CLAUDE.md is an untracked symlink in the main checkout only, so a worker in
`D:/worktrees/...` never sees it (0 tokens, today and lean). In the main checkout it is 9.7k, which is the whole
difference between the two lean rows above. A digest would duplicate a file that belongs to the repo and drift from
it. The worker rules a launched session needs are in the appended system prompt instead.

## Hooks, checked

A lean `claude -p` worker named `sa85-lean-probe`, run in a scratch directory holding the worktree's `.mcp.json`
gate marker, ran `git --version` and called `atrium_report`. The permission history shows `Bash git --version`
approved through the gate, the card shows `launched`, `submitted` and `exited`, and the debug log shows three
PreToolUse hooks for the Bash call. Gating keys on that `.mcp.json` marker or on `atrium join`, not on settings, so
a lean worker is gated exactly when a full one is.

Not checked end to end: a lean worker started by `atrium_launch` through a room built from this branch. That needs a
room restart.

## What a lean worker loses

- The global CLAUDE.md: the chat reply rules, the `Open Questions:` format, the atrium-session preference.
- The auto-memory index. The brief carries what the worker needs.
- User skills (`/code-review`, `/pr-body`, `/recap`, ...), claude.ai-synced skills, and the Skill tool.
- Custom agent types and the Agent tool, so no subagents. Workflows, crons, remote triggers, plan mode,
  AskUserQuestion and the worktree tools.
- The status line, the output style, and the operator's SessionStart and UserPromptSubmit hooks: session bootstrap,
  tab title and state, the filler-guard reminder, the layout snapshot.
- Every MCP server other than atrium-control and mercurius, unless named in `mcp`.

It keeps the permissions, the env, every PreToolUse, PostToolUse, Stop, SessionEnd, Notification, Subagent and
PreCompact hook, atrium's reporters, the project's CLAUDE.md and settings, atrium-control and mercurius.

## Deploy

HUB-SIDE and ROOM-SIDE. The hub sends `lean` and `mcp` on `/v1/launch`, and the room builds the flags. A new hub with
an old room launches as today, because the room ignores the fields. A new room with an old hub is lean only for a
caller that sends `lean: true`. Both are needed, and the room part needs a room restart.

The stdio control server in `internal/cli/control_peers.go` has its own `atrium_launch` and is not changed, on
purpose. Sessions use the hub's HTTP one, so a launch through the stdio server starts a worker as before. Aligning it
is a follow-up if that server stays.

The `launched` event's `cmd` is the harness command before the lean flags go on, as it was before this change. The
lean flags carry the whole settings copy, so the event records `lean` and `mcp` beside `cmd` instead.

## lean_agents and lean_skills (r-005)

`lean_agents: ["codebase-steward", ...]` and `lean_skills: ["review-panel", ...]` on `atrium_launch` (the hub's and
the stdio control MCP) start the worker lean and keep what they name. `lean_agents` keeps the `Agent` tool (and `Task`)
out of `--disallowedTools`. `lean_skills` does the same for `Skill`. The user source stays cut, so no other agent, skill,
memory or CLAUDE.md loads.

**Mechanism: `--plugin-dir`.** `--agents <json>` was tried first and cannot carry real agents: four of them come to
about 39 KB, a Windows command line takes 32 KB in all, and `--agents` takes a file only with `--print`. So atrium
writes a session-only plugin and passes `--plugin-dir <dir>`. The plugin holds `.claude-plugin/plugin.json`,
`agents/<name>.md` and `skills/<name>/`, copied byte for byte from `~/.claude/agents` and `~/.claude/skills`, following
links. Nothing else of `~/.claude` comes with them.

**Names are namespaced.** Claude Code namespaces every plugin's agents and skills, so the worker starts them as
`atrium:<name>`, for example `atrium:go-security-reviewer` and `atrium:review-panel`. A review manager's prompt must use
those names. There is no way to get the bare name from a plugin, and the bare name is what `--agents` gave, which is the
reason it was tried first.

**Where the directories live.** In atrium's own state, `<state>/lean-plugins/<hash>/`, next to the database, never in
the worktree (which must stay clean for the cull). The hash is of the plugin's contents, so one distinct set of named
files is one directory, shared by every card that names it, written once and never rewritten under a live session. A
changed agent file is a new directory. They do not pile up beyond the number of distinct sets, and every one is safe to
delete when no worker is running: the next launch, restart, resume or keep-alive fork writes it again from the card's
tags.

**Costs and limits.** With `lean_skills`, Claude Code's built-in skills come back with the `Skill` tool, about 2k
tokens. The built-in agent types (Explore, general-purpose, Plan, statusline-setup) come with the `Agent` tool and
cannot be removed.

**Refused, each tested:** a name with no file or directory (the error names it), an unsafe name (a separator,
dot-dot or leading dot), `lean: false` with either field, and a runner that is not claude. The card keeps the lists as
`atrium:agent:<name>` and `atrium:skill:<name>` tags, so a restart, a resume and the keep-alive fork rebuild the same
flags, with no column. A room older than the fields is named in the launch warning.
