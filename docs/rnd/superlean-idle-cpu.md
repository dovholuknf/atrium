# Superlean idle CPU

The idle-CPU step of the P4 measurement plan in `docs/runtime/superlean-design.md`, run for
`r-superlean-default-for-items`. It looks for a spin (`bug-idle-minion-spins-a-core`) before the default moves. The
default is not changed here.

## Result

No spin. Both sessions settled to about 0.3 CPU seconds a minute, and superlean was not higher than lean.

| session | CPU s after first turn | CPU s at minute 10 (A) | CPU s 60 s later (B) | B - A over 60 s |
| --- | --- | --- | --- | --- |
| lean | 2.71 | 5.50 | 5.85 | 0.35 |
| superlean | 2.52 | 5.63 | 5.90 | 0.27 |

A spinning core would add about 60 s of CPU in those 60 s. Both rows are under 1 s.

## Method

Claude Code 2.1.310 on darwin, model claude-sonnet-5-5, 2026-10-26, run on the worker's own room with local processes
only and nothing launched through atrium.

- Two interactive `claude` sessions, started together in the same worktree, each on a pty of 50 by 160 driven by a
  small Python script that answers the cursor query and types one prompt.
- After a 15 s start, the prompt `Reply with the single word ok.` was sent, then 40 s for the turn to finish. Both
  turns completed.
- The sessions then sat idle for 600 s. CPU seconds were read with `ps -o cputime=` on the session pid at the end of
  that idle (A) and again 60 s later (B). The sessions were then terminated.
- Lean flags: `--setting-sources project,local`, `--strict-mcp-config --mcp-config` with an empty server list,
  `--disallowedTools` with the 22 tools of `leanDisallowed`, `--append-system-prompt`,
  `CLAUDE_CODE_DISABLE_AUTO_MEMORY=1`.
- Superlean flags: the lean set plus `--tools=Read,Edit,Write,Grep,Glob,Bash,ToolSearch`.

## What is not the real thing

- No atrium-control MCP server and no `leanSettings` hooks (reporters, Stop hook, no-poll hook) were in either session,
  so a launch through atrium was not reproduced. A spin that comes from the MCP stdio server or a hook is not covered.
  Both sessions lacked them equally, so the lean against superlean comparison holds for the tool list and prompt.
- The system prompt was a short stand-in appended to both, not the exact `leanSystemPrompt` or `superleanSystemPrompt`.
- One run each, so a difference of 0.1 s a minute is noise.
- Row 3 of the plan (superlean with `mcp: ["mercurius"]`) is still unmeasured. No config on this machine holds a
  mercurius server.
