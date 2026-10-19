# Slim orchestrator

On 2026-10-19 an orchestrator kept merging branches and running tests inline. That work eats its context and its
attention, and a merge it does itself is one nobody else reviewed. So the operator asked for an orchestrator that
cannot do work, only dispatch it. The `claude-slim` runner is that orchestrator.

## The runner row

`claude-slim`, labelled "claude slim (orchestrator)", is a copy of the `claude` runner row with
`--disallowedTools Bash,PowerShell,Edit,Write,NotebookEdit,Agent` added to both `args` and `resume_args`. The resume
side matters: a resumed session that lost the flag would be a full orchestrator again. The row was created on
sg4-control, and it is data in the room's `harness` table, not code.

## What it keeps

Read, Grep and Glob, and the atrium tools: launching workers, peers, tasks, the backlog, reports and status. It can
look at anything and can ask anyone to change it. It cannot change anything itself, and it cannot start a subagent with
`Agent`, because a subagent would carry the denied tools back in.

## The rule

Every merge goes to a throwaway minion. The orchestrator launches one, and the minion merges the branch, runs
`scripts/test-touched.sh`, pushes when green, and is culled. The orchestrator hears back through `atrium_done` or
`atrium_blocked`, never by doing the merge. See `docs/runtime/item-minion.md` for minions.

## The cost

With no Bash it has no `git log` or `git diff` of its own, and with no Write or Edit it cannot touch memory. So a
summary of a diff comes from the minion that read it, and a memory edit is a request to a minion too. That is slower
by one hop, and it is the price of the guarantee.

## Starting one

In the board's launch dialog pick the runner `claude slim` and set the cwd to the atrium checkout.
