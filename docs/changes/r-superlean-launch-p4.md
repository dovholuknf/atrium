## Test plan

## @LETTER@. Superlean numbers

A documentation change, nothing to run in the board.

1. Open `docs/runtime/superlean-design.md`. The status line says P1 to P3 are built and P4 measured.
2. Find "Numbers". It records Claude Code 2.1.302 and `claude-opus-5-5`, a table of rows 1, 2, 4 and 5 with first-request
   tokens and the `/context` breakdown, and says plainly that row 3 and the idle-CPU comparison were not measured, and why.
3. Find "Decisions". It recommends against building the no-MCP mode and for superlean as the default for per-item minions
   and workers, each with the numbers behind it.
4. Optional, to reproduce: from a worktree, run `claude -p --output-format json` with the flag set the "Numbers"
   section describes and add `input_tokens + cache_creation_input_tokens + cache_read_input_tokens`. Expect the lean and
   superlean rows within a few hundred tokens of the table.
