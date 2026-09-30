# r-005: a lean launch that can still start named subagents

You are a worker for @runtime (the director of internal/daemon and internal/store), who launched you. You're on the
sg3 room, in a worktree on branch claude/r-005 off claude/main (hub-main 4815d47). Read CLAUDE.md at the repo root
first, then docs/backlog/runtime/r-005.md (the item) and docs/runtime/lean-workers-design.md (how lean works and why).

## The problem
A lean launch removes the `Agent` tool (leanDisallowed, internal/daemon/lean.go:72) and loads none of the operator's
agent pack. So a lean review-manager cannot start the reviewers it exists to run. @review therefore launches
review-managers and walkers with the FULL setup, which carries every skill, memory and CLAUDE.md the operator has.
That's what lean exists to avoid.

## The fix (approved by clint)
- A new launch field, `lean_agents: ["codebase-steward", "go-security-reviewer", ...]`. It goes on atrium_launch in
  every place a launch field is declared: the Go LaunchRequest, the /v1 launch JSON, the MCP tool schema and
  description in both control MCPs (the hub's and the stdio `atrium control`, see item 60 and
  link.LaunchOptionsDropped / LaunchDroppedWarning), and the room-is-older warning, so an older room says it
  dropped the field.
- When lean_agents is non-empty: keep the `Agent` tool (take it out of --disallowedTools for that launch only), and
  make ONLY the named agents' definition files available to the session. Find how Claude Code loads agents (the
  operator's ~/.claude/agents/*.md, and how lean's settings and config dir isolation hides them today) and pick the
  narrowest mechanism that exposes just those files. Everything else lean drops stays dropped: user CLAUDE.md,
  memory, skills, other agents and output style. The status line stays, as r-001 made it.
- The item also mentions the review-panel SKILL. Skills are dropped by lean. If the mechanism you use for agents
  cannot carry one named skill cleanly, say so in your report and don't do it. That becomes a follow-up item.
- A named agent that doesn't exist refuses the launch with a clear error naming it. The launch must not start
  without it.
- The card keeps the list, like model, effort, args and lean, so a restart, a resume and the keep-alive fork (item 73,
  runnerArgsWith / keepFlags) come back the same. If that needs a column, add a migration at the END of
  internal/store/schema.go's slice and read its header first. Tell @runtime the migration name before you commit it,
  because other branches may be adding migrations too.
- Codex has no agents: a codex launch with lean_agents is refused, the way a runner that can't take a model refuses.

## Tests
Targeted only: `go test ./internal/daemon/ -run 'Lean|Launch|Keepalive|Restart'`, `./internal/store/ -run <yours>`,
and whichever package holds the MCP tools, with -run. Clear ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG first. Cover:
- the argv of a lean launch with and without lean_agents (Agent is disallowed only without it)
- only the named files are exposed
- an unknown agent is refused
- a restart keeps the list
- codex is refused

## Rules
- Never edit internal/api/web/ (the board is @ui's). If the launch dialog needs the field, tell @runtime and it goes
  to @ui. Never edit CLAUDE.md, CHANGELOG.md or docs/test-plan.md.
- Write changelog/runtime/2026-09-29-r-005.md (1 to 5 lines), docs/changes/r-005.md holding only "## Test plan" plus
  "## @LETTER@. <title>" with steps and **Expected:**, and update docs/backlog/runtime/r-005.md to status BUILT. Use
  `git add -f` for docs/backlog.
- Commit on claude/r-005 only. One-line subject, no body, no trailer. Never push, pull, fetch, merge, restart or
  deploy. Never touch the live room or hub, and never write to the real ~/.claude on sg3 in tests (use temp dirs).
- Ask @runtime with atrium_say if a choice is unclear, especially the agent-exposure mechanism. Done: atrium_report
  status done with the sha.
